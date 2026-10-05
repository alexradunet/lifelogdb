package importer

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"lifelog/internal/core"
	"lifelog/internal/text"
)

const (
	correctionIntentDir     = "correction-intents"
	correctionIntentVersion = 1
)

type correctionIntent struct {
	Version                  int      `json:"version"`
	EventKey                 string   `json:"event_key"`
	WorkspaceSource          string   `json:"workspace_source"`
	RootSource               string   `json:"root_source"`
	RootImportKey            string   `json:"root_import_key"`
	Metric                   string   `json:"metric"`
	ActorSource              string   `json:"actor_source"`
	LegacyFingerprint        string   `json:"legacy_fingerprint"`
	PredecessorKind          string   `json:"predecessor_kind"`
	PredecessorEventKey      string   `json:"predecessor_event_key,omitempty"`
	PredecessorLocalID       int64    `json:"predecessor_local_id,omitempty"`
	PredecessorValue         *float64 `json:"predecessor_value,omitempty"`
	PredecessorRetracted     bool     `json:"predecessor_retracted"`
	Value                    *float64 `json:"value,omitempty"`
	Retracted                bool     `json:"retracted"`
	CreatedAt                string   `json:"created_at"`
	DirectorySyncUnsupported bool     `json:"directory_sync_unsupported,omitempty"`
}

type legacyCorrectionRecord struct {
	Source    string   `json:"source"`
	Key       string   `json:"import_key"`
	MetricKey string   `json:"metric_key"`
	Value     *float64 `json:"value,omitempty"`
	Retracted bool     `json:"retracted"`
}

type correctionRoot struct{ source, metric, key string }

func (i correctionIntent) root() correctionRoot {
	return correctionRoot{i.RootSource, i.Metric, i.RootImportKey}
}
func (i correctionIntent) valueOK(v float64, ok bool) bool {
	return (i.Retracted && !ok) || (!i.Retracted && ok && i.Value != nil && *i.Value == v)
}
func valueOK(want *float64, retracted bool, got float64, ok bool) bool {
	return (retracted && !ok) || (!retracted && ok && want != nil && *want == got)
}

type pendingCorrectionError struct{ msg string }

func (e pendingCorrectionError) Error() string { return e.msg }

func pendingCorrection(format string, a ...any) error {
	return &core.Error{Status: 409, Msg: pendingCorrectionError{fmt.Sprintf(format, a...)}.Error()}
}

var (
	correctionAfterReadyHook  func(correctionIntent) error
	correctionAfterCommitHook func(correctionIntent) error
)

// CorrectImported corrects a replayable imported reading through a durable workspace intent. handled is false for
// ordinary rows whose corrections keep the existing core behavior and do not replay.
func (w *Workspace) CorrectImported(ctx context.Context, s *core.Store, actor string, wrong int64, value *float64) (id int64, handled bool, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	var root, leaf core.MeasurementRow
	if err := s.DryRun(ctx, actor, func(t *core.Tx) error {
		var e error
		root, leaf, e = t.MeasurementRootAndLeaf(wrong)
		if e != nil {
			return e
		}
		if !core.IsImport(root.Source) || root.ImportKey == "" {
			return nil
		}
		_, e = t.Correct(wrong, value)
		return e
	}); err != nil {
		return 0, true, err
	}
	if !core.IsImport(root.Source) || root.ImportKey == "" {
		return 0, false, nil
	}
	workspaceSource, err := w.approvedImportSource()
	if err != nil {
		return 0, true, err
	}
	if workspaceSource != root.Source {
		return 0, true, refuse("workspace source %s cannot record a correction for %s", workspaceSource, root.Source)
	}
	if err := w.recoverCorrectionsLocked(ctx, s); err != nil {
		return 0, true, err
	}

	var intent correctionIntent
	err = s.Do(ctx, actor, func(t *core.Tx) error {
		var e error
		root, leaf, e = t.MeasurementRootAndLeaf(wrong)
		if e != nil {
			return e
		}
		if root.Source != workspaceSource || root.ImportKey == "" || !core.IsImport(root.Source) {
			return refuse("workspace source %s cannot record a correction for %s", workspaceSource, root.Source)
		}
		intent, e = w.buildCorrectionIntent(root, leaf, actor, value)
		if e != nil {
			return e
		}
		id, e = t.CorrectKeyed(wrong, value, intent.EventKey)
		if e != nil {
			return e
		}
		postRename, e := w.publishCorrectionIntent(intent)
		if e != nil {
			id = 0
			if postRename {
				return pendingCorrection("correction intent %s is pending recovery after publication: %v", intent.EventKey, e)
			}
			return e
		}
		if correctionAfterReadyHook != nil {
			if e := correctionAfterReadyHook(intent); e != nil {
				id = 0
				return pendingCorrection("correction intent %s is pending recovery after publication: %v", intent.EventKey, e)
			}
		}
		return nil
	})
	if err != nil {
		return 0, true, err
	}
	if correctionAfterCommitHook != nil {
		if e := correctionAfterCommitHook(intent); e != nil {
			return 0, true, pendingCorrection("correction intent %s is pending recovery after commit: %v", intent.EventKey, e)
		}
	}
	return id, true, nil
}

func (w *Workspace) approvedImportSource() (string, error) {
	g, err := w.Gate("rules.md")
	if err != nil {
		return "", err
	}
	if g != "approved" {
		return "", refuse("workspace rules.md is %s; approve it before recording replayable corrections", g)
	}
	r, err := w.Rules()
	if err != nil {
		return "", err
	}
	return r.Source, nil
}

func (w *Workspace) buildCorrectionIntent(root, leaf core.MeasurementRow, actor string, value *float64) (correctionIntent, error) {
	legacy, err := w.legacyState(root.Source, root.Metric, root.ImportKey, root.Value)
	if err != nil {
		return correctionIntent{}, err
	}
	intent := correctionIntent{
		Version:                  correctionIntentVersion,
		EventKey:                 newCorrectionEventKey(root.Source, root.Metric, root.ImportKey),
		WorkspaceSource:          root.Source,
		RootSource:               root.Source,
		RootImportKey:            root.ImportKey,
		Metric:                   root.Metric,
		ActorSource:              actor,
		LegacyFingerprint:        legacy.fingerprint,
		PredecessorLocalID:       leaf.ID,
		PredecessorValue:         leaf.Value,
		PredecessorRetracted:     leaf.Value == nil,
		Value:                    value,
		Retracted:                value == nil,
		CreatedAt:                time.Now().UTC().Format("2006-01-02T15:04:05.000Z"),
		DirectorySyncUnsupported: directorySyncKnownUnsupported(),
	}
	if strings.HasPrefix(leaf.ImportKey, "correction-") {
		prev, ok, err := w.intentByEventKey(leaf.ImportKey)
		if err != nil {
			return correctionIntent{}, err
		}
		if !ok || prev.root() != intent.root() || prev.LegacyFingerprint != legacy.fingerprint {
			return correctionIntent{}, refuse("correction predecessor %s is not an unambiguous workspace event", leaf.ImportKey)
		}
		intent.PredecessorKind = "event"
		intent.PredecessorEventKey = leaf.ImportKey
		return intent, nil
	}
	if hasEvent, err := w.rootHasEvent(root.Source, root.Metric, root.ImportKey); err != nil {
		return correctionIntent{}, err
	} else if hasEvent {
		return correctionIntent{}, refuse("correction history for %s %s has event records but the current predecessor is not an event", root.Metric, root.ImportKey)
	}
	if !valueOK(legacy.value, legacy.retracted, valueOfRow(leaf), leaf.Value != nil) {
		return correctionIntent{}, refuse("legacy correction baseline for %s %s no longer matches the current predecessor", root.Metric, root.ImportKey)
	}
	intent.PredecessorKind = "legacy"
	intent.PredecessorValue = legacy.value
	intent.PredecessorRetracted = legacy.retracted
	return intent, nil
}

func valueOfRow(r core.MeasurementRow) float64 {
	if r.Value == nil {
		return 0
	}
	return *r.Value
}

func newCorrectionEventKey(parts ...string) string {
	var nonce [16]byte
	_, _ = rand.Read(nonce[:])
	h := sha256.New()
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	h.Write([]byte(time.Now().UTC().Format(time.RFC3339Nano)))
	h.Write(nonce[:])
	return "correction-" + hex.EncodeToString(h.Sum(nil))[:32]
}

type legacyState struct {
	fingerprint string
	value       *float64
	retracted   bool
}

func (w *Workspace) legacyState(source, metric, key string, original *float64) (legacyState, error) {
	cs, err := w.Corrections()
	if err != nil {
		return legacyState{}, err
	}
	var records []legacyCorrectionRecord
	state := legacyState{value: cloneFloat(original), retracted: original == nil}
	metricKey := text.TitleKey(metric)
	for _, c := range cs {
		if c.Source != source || c.Key != key || text.TitleKey(c.Metric) != metricKey {
			continue
		}
		r := legacyCorrectionRecord{Source: c.Source, Key: c.Key, MetricKey: metricKey, Value: cloneFloat(c.Value), Retracted: c.Value == nil}
		records = append(records, r)
		state.value, state.retracted = cloneFloat(c.Value), c.Value == nil
	}
	b, _ := json.Marshal(records)
	sum := sha256.Sum256(b)
	state.fingerprint = hex.EncodeToString(sum[:])
	return state, nil
}

func cloneFloat(v *float64) *float64 {
	if v == nil {
		return nil
	}
	x := *v
	return &x
}

func (w *Workspace) publishCorrectionIntent(intent correctionIntent) (postRename bool, err error) {
	dir := filepath.Join(w.Dir, correctionIntentDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return false, err
	}
	data, err := json.MarshalIndent(intent, "", "  ")
	if err != nil {
		return false, err
	}
	data = append(data, '\n')
	tmp, err := os.CreateTemp(dir, ".tmp-*.json")
	if err != nil {
		return false, err
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return false, err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return false, err
	}
	if err := tmp.Close(); err != nil {
		return false, err
	}
	ready := filepath.Join(dir, intent.EventKey+".json")
	if err := os.Rename(tmpName, ready); err != nil {
		return false, err
	}
	cleanup = false
	if err := syncDir(dir); err != nil {
		return true, err
	}
	return false, nil
}

func syncDir(dir string) error {
	if directorySyncKnownUnsupported() {
		return nil
	}
	f, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}

func directorySyncKnownUnsupported() bool {
	// Windows does not support the POSIX directory-fsync operation used here. The intent file itself is still
	// synced before rename, so the supported claim there is process-crash recovery from a visible ready file.
	return runtime.GOOS == "windows"
}

// RecoverCorrections applies ready workspace intents to the trial database. It has no effect on legacy
// corrections.json and does not mutate immutable intent files.
func (w *Workspace) RecoverCorrections(ctx context.Context, s *core.Store) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.recoverCorrectionsLocked(ctx, s)
}

func (w *Workspace) recoverCorrectionsLocked(ctx context.Context, s *core.Store) error {
	intents, err := w.correctionIntents()
	if err != nil {
		return err
	}
	for _, intent := range intents {
		legacy, err := w.legacyState(intent.RootSource, intent.Metric, intent.RootImportKey, nil)
		if err != nil {
			return err
		}
		if legacy.fingerprint != intent.LegacyFingerprint {
			return refuse("correction intent %s conflicts with edits to legacy corrections for %s %s", intent.EventKey, intent.Metric, intent.RootImportKey)
		}
		if err := s.Do(ctx, intent.ActorSource, func(t *core.Tx) error { return recoverOneCorrection(t, intent) }); err != nil {
			return err
		}
	}
	return nil
}

func recoverOneCorrection(t *core.Tx, intent correctionIntent) error {
	if id, err := t.MeasurementByKey(intent.ActorSource, intent.Metric, intent.EventKey); err != nil {
		return err
	} else if id != 0 {
		return verifyEventRow(t, intent, id)
	}
	rootID, err := t.MeasurementByKey(intent.RootSource, intent.Metric, intent.RootImportKey)
	if err != nil {
		return err
	}
	if rootID == 0 {
		return refuse("correction intent %s names a reading the import did not write: %s", intent.EventKey, intent.RootImportKey)
	}
	leafID, value, ok, err := t.CurrentOf(rootID)
	if err != nil {
		return err
	}
	leaf, err := t.MeasurementRow(leafID)
	if err != nil {
		return err
	}
	switch intent.PredecessorKind {
	case "legacy":
		if leaf.ID != intent.PredecessorLocalID || !valueOK(intent.PredecessorValue, intent.PredecessorRetracted, value, ok) {
			return refuse("correction intent %s predecessor no longer matches; owner resolution is needed", intent.EventKey)
		}
	case "event":
		if leaf.ImportKey != intent.PredecessorEventKey {
			return refuse("correction intent %s predecessor is %s, not %s", intent.EventKey, leaf.ImportKey, intent.PredecessorEventKey)
		}
	default:
		return refuse("correction intent %s has unknown predecessor kind %q", intent.EventKey, intent.PredecessorKind)
	}
	_, err = t.CorrectKeyed(leaf.ID, intent.Value, intent.EventKey)
	return err
}

func verifyEventRow(t *core.Tx, intent correctionIntent, id int64) error {
	root, row, err := t.MeasurementRootAndLeaf(id)
	if err != nil {
		return err
	}
	if root.Source != intent.RootSource || root.ImportKey != intent.RootImportKey || text.TitleKey(root.Metric) != text.TitleKey(intent.Metric) {
		return refuse("correction intent %s matches a row with another root", intent.EventKey)
	}
	if row.ID != id && row.ImportKey != intent.EventKey {
		// The event exists but has itself been corrected; replay/recovery can advance from it, but a copied target
		// with another current leaf is checked by the next intent or reported as a conflict there.
		return nil
	}
	if (intent.Retracted && row.Value != nil) || (!intent.Retracted && (row.Value == nil || intent.Value == nil || *row.Value != *intent.Value)) {
		return refuse("correction intent %s matches a row with another value", intent.EventKey)
	}
	return nil
}

func (w *Workspace) correctionIntents() ([]correctionIntent, error) {
	dir := filepath.Join(w.Dir, correctionIntentDir)
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var intents []correctionIntent
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") || strings.HasPrefix(e.Name(), ".tmp-") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		var intent correctionIntent
		if err := json.Unmarshal(b, &intent); err != nil {
			return nil, fmt.Errorf("%s: %w", e.Name(), err)
		}
		intents = append(intents, intent)
	}
	sort.Slice(intents, func(i, j int) bool {
		if intents[i].CreatedAt == intents[j].CreatedAt {
			return intents[i].EventKey < intents[j].EventKey
		}
		return intents[i].CreatedAt < intents[j].CreatedAt
	})
	return intents, nil
}

func (w *Workspace) intentByEventKey(key string) (correctionIntent, bool, error) {
	intents, err := w.correctionIntents()
	if err != nil {
		return correctionIntent{}, false, err
	}
	for _, intent := range intents {
		if intent.EventKey == key {
			return intent, true, nil
		}
	}
	return correctionIntent{}, false, nil
}

func (w *Workspace) rootHasEvent(source, metric, key string) (bool, error) {
	intents, err := w.correctionIntents()
	if err != nil {
		return false, err
	}
	metricKey := text.TitleKey(metric)
	for _, intent := range intents {
		if intent.RootSource == source && intent.RootImportKey == key && text.TitleKey(intent.Metric) == metricKey {
			return true, nil
		}
	}
	return false, nil
}

func (w *Workspace) correctionIntentStates(ctx context.Context, s *core.Store) ([]string, error) {
	intents, err := w.correctionIntents()
	if err != nil || len(intents) == 0 || s == nil {
		return nil, err
	}
	var out []string
	for _, intent := range intents {
		err := s.DryRun(ctx, intent.ActorSource, func(t *core.Tx) error { return recoverOneCorrection(t, intent) })
		if err == nil {
			out = append(out, fmt.Sprintf("pending correction intent %s for %s %s", intent.EventKey, intent.Metric, intent.RootImportKey))
		} else if !alreadyRecovered(ctx, s, intent) {
			out = append(out, fmt.Sprintf("conflicting correction intent %s for %s %s: %v", intent.EventKey, intent.Metric, intent.RootImportKey, err))
		}
	}
	return out, nil
}

func alreadyRecovered(ctx context.Context, s *core.Store, intent correctionIntent) bool {
	ok := false
	_ = s.DryRun(ctx, intent.ActorSource, func(t *core.Tx) error {
		id, err := t.MeasurementByKey(intent.ActorSource, intent.Metric, intent.EventKey)
		if err == nil && id != 0 && verifyEventRow(t, intent, id) == nil {
			ok = true
		}
		return nil
	})
	return ok
}
