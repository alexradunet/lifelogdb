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
	correctionAfterReadyHook     func(correctionIntent) error
	correctionAfterCommitHook    func(correctionIntent) error
	correctionTempWriteHook      func(correctionIntent) error
	correctionFileSyncHook       func(correctionIntent) error
	correctionPreRenameHook      func(correctionIntent) error
	correctionPostRenameSyncHook func(correctionIntent) error
	correctionResyncHook         func(correctionIntent) error
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
	published := false
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
		if postRename || e == nil {
			published = true
		}
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
		if published && !strings.Contains(err.Error(), "pending recovery") {
			return 0, true, pendingCorrection("correction intent %s is pending recovery after publication: %v", intent.EventKey, err)
		}
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
	if correctionTempWriteHook != nil {
		if err := correctionTempWriteHook(intent); err != nil {
			tmp.Close()
			return false, err
		}
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return false, err
	}
	if correctionFileSyncHook != nil {
		if err := correctionFileSyncHook(intent); err != nil {
			tmp.Close()
			return false, err
		}
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return false, err
	}
	if err := tmp.Close(); err != nil {
		return false, err
	}
	ready := filepath.Join(dir, intent.EventKey+".json")
	if correctionPreRenameHook != nil {
		if err := correctionPreRenameHook(intent); err != nil {
			return false, err
		}
	}
	if err := os.Rename(tmpName, ready); err != nil {
		return false, err
	}
	cleanup = false
	if correctionPostRenameSyncHook != nil {
		if err := correctionPostRenameSyncHook(intent); err != nil {
			return true, err
		}
	}
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
	syncErr := f.Sync()
	closeErr := f.Close()
	return errors.Join(syncErr, closeErr)
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
	intents, err := w.orderedCorrectionIntents()
	if err != nil {
		return err
	}
	known := map[string]correctionIntent{}
	for _, intent := range intents {
		known[intent.EventKey] = intent
	}
	for _, intent := range intents {
		legacy, err := w.legacyState(intent.RootSource, intent.Metric, intent.RootImportKey, nil)
		if err != nil {
			return err
		}
		if legacy.fingerprint != intent.LegacyFingerprint {
			return refuse("correction intent %s conflicts with edits to legacy corrections for %s %s", intent.EventKey, intent.Metric, intent.RootImportKey)
		}
		if err := w.resyncCorrectionIntent(intent); err != nil {
			return err
		}
		if err := s.Do(ctx, intent.ActorSource, func(t *core.Tx) error { return recoverOneCorrection(t, intent, known) }); err != nil {
			return err
		}
	}
	return nil
}

func recoverOneCorrection(t *core.Tx, intent correctionIntent, known map[string]correctionIntent) error {
	if id, err := t.MeasurementByKey(intent.ActorSource, intent.Metric, intent.EventKey); err != nil {
		return err
	} else if id != 0 {
		return verifyEventRow(t, intent, id, known)
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

func verifyEventRow(t *core.Tx, intent correctionIntent, id int64, known map[string]correctionIntent) error {
	root, _, err := t.MeasurementRootAndLeaf(id)
	if err != nil {
		return err
	}
	row, err := t.MeasurementRow(id)
	if err != nil {
		return err
	}
	if row.Source != intent.ActorSource || row.ImportKey != intent.EventKey {
		return refuse("correction intent %s matches a row with another event identity", intent.EventKey)
	}
	if root.Source != intent.RootSource || root.ImportKey != intent.RootImportKey || text.TitleKey(root.Metric) != text.TitleKey(intent.Metric) {
		return refuse("correction intent %s matches a row with another root", intent.EventKey)
	}
	if (intent.Retracted && row.Value != nil) || (!intent.Retracted && (row.Value == nil || intent.Value == nil || *row.Value != *intent.Value)) {
		return refuse("correction intent %s matches a row with another value", intent.EventKey)
	}
	pred, err := t.MeasurementRow(row.Supersedes)
	if err != nil {
		return err
	}
	switch intent.PredecessorKind {
	case "legacy":
		if !valueOK(intent.PredecessorValue, intent.PredecessorRetracted, valueOfRow(pred), pred.Value != nil) {
			return refuse("correction intent %s has a different legacy predecessor", intent.EventKey)
		}
	case "event":
		if pred.ImportKey != intent.PredecessorEventKey {
			return refuse("correction intent %s predecessor is %s, not %s", intent.EventKey, pred.ImportKey, intent.PredecessorEventKey)
		}
		parent, ok := known[intent.PredecessorEventKey]
		if !ok {
			return refuse("correction intent %s predecessor %s is not a known intent", intent.EventKey, intent.PredecessorEventKey)
		}
		if parent.RootSource != intent.RootSource || parent.RootImportKey != intent.RootImportKey || text.TitleKey(parent.Metric) != text.TitleKey(intent.Metric) || pred.Source != parent.ActorSource || !valueOK(parent.Value, parent.Retracted, valueOfRow(pred), pred.Value != nil) {
			return refuse("correction intent %s predecessor event %s does not match its intent", intent.EventKey, intent.PredecessorEventKey)
		}
	default:
		return refuse("correction intent %s has unknown predecessor kind %q", intent.EventKey, intent.PredecessorKind)
	}
	leafID, _, _, err := t.CurrentOf(root.ID)
	if err != nil {
		return err
	}
	if leafID != row.ID {
		leaf, err := t.MeasurementRow(leafID)
		if err != nil {
			return err
		}
		if _, ok := known[leaf.ImportKey]; !ok {
			return refuse("correction intent %s is followed by an unknown later correction", intent.EventKey)
		}
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
	seen := map[string]bool{}
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
		if err := w.validateCorrectionIntent(e.Name(), intent); err != nil {
			return nil, err
		}
		if seen[intent.EventKey] {
			return nil, refuse("duplicate correction intent %s", intent.EventKey)
		}
		seen[intent.EventKey] = true
		intents = append(intents, intent)
	}
	return intents, nil
}

func (w *Workspace) validateCorrectionIntent(filename string, intent correctionIntent) error {
	switch {
	case intent.Version != correctionIntentVersion:
		return refuse("correction intent %s has version %d", filename, intent.Version)
	case intent.EventKey == "" || filename != intent.EventKey+".json":
		return refuse("correction intent filename %s does not match event key %q", filename, intent.EventKey)
	case intent.WorkspaceSource == "" || intent.RootSource == "" || intent.RootImportKey == "" || intent.Metric == "" || intent.ActorSource == "" || intent.LegacyFingerprint == "" || intent.CreatedAt == "":
		return refuse("correction intent %s is missing required identity fields", intent.EventKey)
	case intent.WorkspaceSource != intent.RootSource:
		return refuse("correction intent %s workspace source %s does not match root source %s", intent.EventKey, intent.WorkspaceSource, intent.RootSource)
	case intent.Retracted == (intent.Value != nil):
		return refuse("correction intent %s has inconsistent value/retraction fields", intent.EventKey)
	case intent.PredecessorRetracted == (intent.PredecessorValue != nil):
		return refuse("correction intent %s has inconsistent predecessor value/retraction fields", intent.EventKey)
	case intent.PredecessorKind != "legacy" && intent.PredecessorKind != "event":
		return refuse("correction intent %s has predecessor kind %q", intent.EventKey, intent.PredecessorKind)
	case intent.PredecessorKind == "event" && intent.PredecessorEventKey == "":
		return refuse("correction intent %s has no predecessor event key", intent.EventKey)
	}
	src, err := w.approvedImportSource()
	if err != nil {
		return err
	}
	if src != intent.WorkspaceSource {
		return refuse("correction intent %s belongs to %s, not workspace %s", intent.EventKey, intent.WorkspaceSource, src)
	}
	return nil
}

func (w *Workspace) orderedCorrectionIntents() ([]correctionIntent, error) {
	intents, err := w.correctionIntents()
	if err != nil || len(intents) == 0 {
		return intents, err
	}
	groups := map[correctionRoot][]correctionIntent{}
	for _, intent := range intents {
		groups[intent.root()] = append(groups[intent.root()], intent)
	}
	var roots []correctionRoot
	for root := range groups {
		roots = append(roots, root)
	}
	sort.Slice(roots, func(i, j int) bool {
		return roots[i].source+roots[i].metric+roots[i].key < roots[j].source+roots[j].metric+roots[j].key
	})
	var ordered []correctionIntent
	for _, root := range roots {
		chain, err := orderIntentChain(groups[root])
		if err != nil {
			return nil, err
		}
		ordered = append(ordered, chain...)
	}
	return ordered, nil
}

func orderIntentChain(events []correctionIntent) ([]correctionIntent, error) {
	byKey := map[string]correctionIntent{}
	children := map[string][]correctionIntent{}
	var first []correctionIntent
	for _, event := range events {
		byKey[event.EventKey] = event
		if event.PredecessorKind == "legacy" {
			first = append(first, event)
		} else {
			children[event.PredecessorEventKey] = append(children[event.PredecessorEventKey], event)
		}
	}
	if len(first) != 1 {
		return nil, refuse("correction events for %s %s have %d legacy starts", events[0].Metric, events[0].RootImportKey, len(first))
	}
	var out []correctionIntent
	for cur := first[0]; ; {
		out = append(out, cur)
		next := children[cur.EventKey]
		if len(next) == 0 {
			break
		}
		if len(next) > 1 {
			return nil, refuse("correction event %s has multiple successors", cur.EventKey)
		}
		cur = next[0]
		delete(children, out[len(out)-1].EventKey)
		if len(out) > len(events) {
			return nil, refuse("correction events for %s %s contain a cycle", cur.Metric, cur.RootImportKey)
		}
	}
	if len(out) != len(events) {
		return nil, refuse("correction events for %s %s have missing predecessors", events[0].Metric, events[0].RootImportKey)
	}
	_ = byKey
	return out, nil
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

func (w *Workspace) resyncCorrectionIntent(intent correctionIntent) error {
	if correctionResyncHook != nil {
		if err := correctionResyncHook(intent); err != nil {
			return err
		}
	}
	p := filepath.Join(w.Dir, correctionIntentDir, intent.EventKey+".json")
	f, err := os.OpenFile(p, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return pendingCorrection("correction intent %s is pending recovery after file resync failed: %v", intent.EventKey, err)
	}
	if err := f.Close(); err != nil {
		return pendingCorrection("correction intent %s is pending recovery after file close failed: %v", intent.EventKey, err)
	}
	if err := syncDir(filepath.Dir(p)); err != nil {
		return pendingCorrection("correction intent %s is pending recovery after directory resync failed: %v", intent.EventKey, err)
	}
	return nil
}

func (w *Workspace) correctionIntentStates(ctx context.Context, s *core.Store) ([]string, error) {
	intents, err := w.orderedCorrectionIntents()
	if err != nil || len(intents) == 0 || s == nil {
		return nil, err
	}
	known := map[string]correctionIntent{}
	for _, intent := range intents {
		known[intent.EventKey] = intent
	}
	var out []string
	for _, intent := range intents {
		legacy, err := w.legacyState(intent.RootSource, intent.Metric, intent.RootImportKey, nil)
		if err != nil {
			return nil, err
		}
		if legacy.fingerprint != intent.LegacyFingerprint {
			out = append(out, fmt.Sprintf("conflict: correction intent %s for %s %s conflicts with edited legacy corrections", intent.EventKey, intent.Metric, intent.RootImportKey))
			continue
		}
		state, err := inspectCorrectionIntent(ctx, s, intent, known)
		if err != nil {
			return nil, err
		}
		if state != "" {
			out = append(out, state)
		}
	}
	return out, nil
}

func inspectCorrectionIntent(ctx context.Context, s *core.Store, intent correctionIntent, known map[string]correctionIntent) (string, error) {
	var state string
	err := s.DryRun(ctx, intent.ActorSource, func(t *core.Tx) error {
		id, err := t.MeasurementByKey(intent.ActorSource, intent.Metric, intent.EventKey)
		if err != nil {
			return err
		}
		if id != 0 {
			if err := verifyEventRow(t, intent, id, known); err != nil {
				state = fmt.Sprintf("conflict: correction intent %s for %s %s: %v", intent.EventKey, intent.Metric, intent.RootImportKey, err)
			}
			return nil
		}
		rootID, err := t.MeasurementByKey(intent.RootSource, intent.Metric, intent.RootImportKey)
		if err != nil {
			return err
		}
		if rootID == 0 {
			state = fmt.Sprintf("conflict: correction intent %s for %s %s names a missing root", intent.EventKey, intent.Metric, intent.RootImportKey)
			return nil
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
			if valueOK(intent.PredecessorValue, intent.PredecessorRetracted, value, ok) && leaf.ID == intent.PredecessorLocalID {
				state = fmt.Sprintf("pending correction intent %s for %s %s", intent.EventKey, intent.Metric, intent.RootImportKey)
			} else {
				state = fmt.Sprintf("conflict: correction intent %s for %s %s predecessor no longer matches", intent.EventKey, intent.Metric, intent.RootImportKey)
			}
		case "event":
			if leaf.ImportKey == intent.PredecessorEventKey {
				state = fmt.Sprintf("pending correction intent %s for %s %s", intent.EventKey, intent.Metric, intent.RootImportKey)
			} else {
				state = fmt.Sprintf("conflict: correction intent %s for %s %s predecessor is %s", intent.EventKey, intent.Metric, intent.RootImportKey, leaf.ImportKey)
			}
		}
		return nil
	})
	return state, err
}
