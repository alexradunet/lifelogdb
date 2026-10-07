package importer

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"lifelog/internal/core"
	"lifelog/internal/text"
)

const preparedFile = "prepared.md"

// PreparedBatch contains source-derived records and explicit owner-reviewed target handles.
// Handles survive rename through the retained-name registry; keys never depend on them.
type PreparedBatch struct {
	PreparedSource
	Source  string            `json:"source"`
	Kind    string            `json:"kind,omitempty"`
	Metrics map[string]string `json:"metrics"`
}

func (w *Workspace) DraftPrepared(ctx context.Context, file, profile, kind string, binding *FitSessionBinding, metrics map[string]string) (*PreparedBatch, error) {
	w.selectionMu.RLock()
	defer w.selectionMu.RUnlock()
	w.mu.Lock()
	defer w.mu.Unlock()
	rules, err := w.snapshotRules()
	if err != nil {
		return nil, err
	}
	derived, err := w.PrepareSupportedSource(ctx, file, profile, binding)
	if err != nil {
		return nil, err
	}
	b := &PreparedBatch{PreparedSource: *derived, Source: rules.Source, Kind: kind, Metrics: metrics}
	data, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return nil, err
	}
	if err = w.validatePrepared(ctx, b); err != nil {
		return nil, err
	}
	if err = writeAtomic(w.file(preparedFile), append([]byte("status: draft\n"), append(data, 10)...)); err != nil {
		return nil, err
	}
	return b, nil
}

func (w *Workspace) validatePrepared(ctx context.Context, b *PreparedBatch) error {
	if b.Version != 1 {
		return refuse("unsupported prepared version")
	}
	rules, err := w.snapshotRules()
	if err != nil {
		return err
	}
	if b.Source != rules.Source {
		return refuse("prepared namespace differs from rules")
	}
	derived, err := w.PrepareSupportedSource(ctx, b.File, b.Profile, b.FitBinding)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(*derived, b.PreparedSource) {
		return refuse("prepared source fingerprint or interpretation changed")
	}
	codes := map[string]string{}
	targets := map[string]string{}
	for _, r := range b.Records {
		for _, q := range r.Quantities {
			codes[q.Code] = q.Unit
		}
	}
	if len(codes) != len(b.Metrics) {
		return refuse("prepared quantity mappings must be exact")
	}
	for code := range codes {
		m := b.Metrics[code]
		if !text.ValidTitle(m) {
			return refuse("missing prepared metric mapping")
		}
		key := text.TitleKey(m)
		if targets[key] != "" {
			return refuse("distinct quantities cannot collapse onto a metric")
		}
		targets[key] = code
	}
	if b.Profile != "fit-date-csv-v1" && !text.ValidTitle(b.Kind) {
		return refuse("prepared session kind required")
	}
	return nil
}
func (w *Workspace) readPrepared(ctx context.Context, approved bool) (*PreparedBatch, error) {
	body, err := w.artifactBody(preparedFile, approved)
	if err != nil {
		return nil, err
	}
	return w.parsePrepared(ctx, body)
}
func (w *Workspace) parsePrepared(ctx context.Context, body string) (*PreparedBatch, error) {
	if len(body) > 4*maxProfileBytes {
		return nil, refuse("prepared artifact exceeds byte limit")
	}
	if err := validateSourceJSON([]byte(body)); err != nil {
		return nil, refuse("malformed prepared artifact")
	}
	var b PreparedBatch
	dec := json.NewDecoder(strings.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&b); err != nil {
		return nil, refuse("unsupported prepared artifact")
	}
	if err := w.validatePrepared(ctx, &b); err != nil {
		return nil, err
	}
	return &b, nil
}

// Prepared applies or checks the stamped batch through the same transaction and core operations.
// The immutable external binding is published before SQL; ledger publication follows commit.
func (w *Workspace) Prepared(ctx context.Context, s *core.Store, dry bool) (*Report, error) {
	w.selectionMu.RLock()
	defer w.selectionMu.RUnlock()
	return w.prepared(ctx, s, dry, nil)
}

// afterCommit is the narrow publication interruption boundary, not a joint SQL/filesystem transaction.
func (w *Workspace) prepared(ctx context.Context, s *core.Store, dry bool, afterCommit func() error) (*Report, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	b, err := w.readPrepared(ctx, true)
	if err != nil {
		return nil, err
	}
	if err = w.admitSource(b.File); err != nil {
		return nil, err
	}
	if err = w.bindPrepared(b, false); err != nil {
		return nil, err
	}
	state, e := w.ledgerState(b.File)
	if e != nil {
		return nil, e
	}
	if state == "x" {
		if _, e = w.readBoundedWorkspace(preparedBindingName(b), 4*maxProfileBytes); e != nil {
			return nil, refuse("done source is missing its immutable reservation")
		}
	}
	if !dry {
		if _, err = writePrepared(ctx, s, b, true); err != nil {
			return nil, err
		}
	}
	if err = w.bindPrepared(b, !dry); err != nil {
		return nil, err
	}
	report, err := writePrepared(ctx, s, b, dry)
	if err != nil || dry {
		return report, err
	}
	report.Applied = true
	if afterCommit != nil {
		if err := afterCommit(); err != nil {
			return report, err
		}
	}
	if err = w.completePrepared(b); err != nil {
		return report, err
	}
	err = w.markLocked(b.File, func(l *Line) error {
		l.State = "x"
		l.Note = "prepared source applied; excluded metadata remains excluded"
		return nil
	})
	return report, err
}
func (w *Workspace) bindPrepared(b *PreparedBatch, publish bool) error {
	data, err := json.Marshal(b)
	if err != nil {
		return err
	}
	entries, e := os.ReadDir(w.Dir)
	if e != nil {
		return e
	}
	if len(entries) > 10000 {
		return refuse("workspace receipt limit exceeded")
	}
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), ".prepared-binding-") || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		raw, e := w.readBoundedWorkspace(entry.Name(), 4*maxProfileBytes)
		if e != nil {
			return e
		}
		if validateSourceJSON(raw) != nil {
			return refuse("corrupt prepared reservation")
		}
		var have PreparedBatch
		dec := json.NewDecoder(strings.NewReader(string(raw)))
		dec.DisallowUnknownFields()
		if dec.Decode(&have) != nil || entry.Name() != preparedBindingName(&have) {
			return refuse("corrupt prepared reservation identity")
		}
		if have.File == b.File {
			old, e := json.Marshal(&have)
			if e != nil {
				return e
			}
			if string(old) != string(data) {
				return refuse("workspace logical source interpretation is immutable")
			}
		}
	}
	name := ".prepared-binding-" + bodyHash(b.Source+"\n"+b.Profile+"\n"+b.File) + ".json"
	old, err := w.readBoundedWorkspace(name, 4*maxProfileBytes)
	if err == nil {
		if string(old) != string(data) {
			return refuse("applied prepared binding is immutable")
		}
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if publish {
		return writeAtomic(w.file(name), data)
	}
	return nil
}
func preparedKey(profile, key, quantity string) string {
	b, _ := json.Marshal([]string{"prepared-v1", profile, key, quantity})
	return string(b)
}
func writePrepared(ctx context.Context, s *core.Store, b *PreparedBatch, dry bool) (*Report, error) {
	return writePreparedAfter(ctx, s, b, dry, nil)
}
func writePreparedAfter(ctx context.Context, s *core.Store, b *PreparedBatch, dry bool, afterReading func(*core.Tx) error) (*Report, error) {

	report := &Report{File: b.File, Outcomes: []Outcome{}, Summary: "prepared source; excluded metadata not imported"}
	fn := func(tx *core.Tx) error {
		seenMetrics := map[int64]bool{}
		for _, name := range b.Metrics {
			id, err := tx.MetricID(name)
			if err != nil {
				return err
			}
			if seenMetrics[id] {
				return refuse("distinct source quantities share a metric identity")
			}
			seenMetrics[id] = true
		}
		for _, r := range b.Records {
			var session int64
			if b.Profile != "fit-date-csv-v1" {
				var err error
				var existing bool
				session, existing, err = tx.CaptureSession(core.SessionInput{Kind: b.Kind, Day: r.Day, Key: preparedKey(b.Profile, r.Key, "session"), StartAt: r.StartAt, StartLocal: r.StartLocal, StartOffset: r.StartOffset, EndAt: r.EndAt, EndLocal: r.EndLocal, EndOffset: r.EndOffset})
				if err != nil {
					return err
				}
				status := "new"
				if existing {
					status = "existing"
				}
				report.Outcomes = append(report.Outcomes, Outcome{Kind: "session", Status: status})
			}
			for _, q := range r.Quantities {
				metric := b.Metrics[q.Code]
				unit, found, err := tx.MetricUnit(metric)
				if err != nil {
					return err
				}
				if !found || unit != q.Unit {
					return refuse("prepared metric unit is unregistered or incompatible")
				}
				value, err := strconv.ParseFloat(q.Value, 64)
				if err != nil {
					return refuse("unrepresentable prepared quantity")
				}
				id, err := tx.RecordPrepared(core.Reading{Metric: metric, Day: r.Day, Key: preparedKey(b.Profile, r.Key, q.Code), Value: value, SessionID: session})
				if err != nil {
					return err
				}
				status := "existing"
				if id != 0 {
					status = "new"
				}
				report.Outcomes = append(report.Outcomes, Outcome{Kind: "reading", Status: status})
				if afterReading != nil {
					if e := afterReading(tx); e != nil {
						return e
					}
				}
			}
		}
		return nil
	}
	var err error
	if dry {
		err = s.DryRun(ctx, b.Source, fn)
	} else {
		err = s.Do(ctx, b.Source, fn)
	}
	return report, err
}

// Applied preparations retain the original approved interpretation, including prior source files.
func (w *Workspace) appliedPrepared(ctx context.Context) ([]*PreparedBatch, error) {
	entries, err := os.ReadDir(w.Dir)
	if err != nil {
		return nil, err
	}
	var out []*PreparedBatch
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, ".prepared-binding-") || !strings.HasSuffix(name, ".json") {
			continue
		}
		raw, err := w.readBoundedWorkspace(name, 4*maxProfileBytes)
		if err != nil {
			return nil, err
		}
		if validateSourceJSON(raw) != nil {
			return nil, refuse("invalid immutable prepared binding")
		}
		var b PreparedBatch
		dec := json.NewDecoder(strings.NewReader(string(raw)))
		dec.DisallowUnknownFields()
		if dec.Decode(&b) != nil {
			return nil, refuse("invalid immutable prepared binding")
		}
		expected := ".prepared-binding-" + bodyHash(b.Source+string(rune(10))+b.Profile+string(rune(10))+b.File) + ".json"
		if name != expected {
			return nil, refuse("prepared binding identity mismatch")
		}
		if err = w.validatePrepared(ctx, &b); err != nil {
			return nil, err
		}
		completed, e := w.preparedCompletion(&b)
		if e != nil {
			return nil, e
		}
		if completed {
			out = append(out, &b)
		}

	}
	sort.Slice(out, func(i, j int) bool { return out[i].File < out[j].File })
	return out, nil
}
func (w *Workspace) readBoundedWorkspace(name string, limit int64) ([]byte, error) {
	root, err := os.OpenRoot(w.Dir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	f, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	data, e := io.ReadAll(io.LimitReader(f, limit+1))
	ce := f.Close()
	if e != nil {
		return nil, e
	}
	if ce != nil {
		return nil, ce
	}
	if int64(len(data)) > limit {
		return nil, refuse("workspace artifact exceeds byte limit")
	}
	return data, nil
}

func (w *Workspace) admitSource(file string) error {
	state, err := w.ledgerState(file)
	if err != nil {
		return err
	}
	if state == "" || state == "-" {
		return refuse("selected source is missing from ledger or skipped")
	}
	return nil
}

func (w *Workspace) artifactBody(name string, approved bool) (string, error) {
	raw, ok, err := w.read(name)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", os.ErrNotExist
	}
	status, body := splitStatus(raw)
	if approved {
		m := stampRE.FindStringSubmatch(status)
		if m == nil || m[2] != bodyHash(body) {
			return "", refuse("owner artifact gate is closed")
		}
	}
	return body, nil
}
func (w *Workspace) snapshotRules() (*Rules, error) {
	body, err := w.artifactBody("rules.md", true)
	if err != nil {
		return nil, err
	}
	return parseRules(body)
}

func preparedBindingName(b *PreparedBatch) string {
	return ".prepared-binding-" + bodyHash(b.Source+string(rune(10))+b.Profile+string(rune(10))+b.File) + ".json"
}
func (w *Workspace) completePrepared(b *PreparedBatch) error {
	data, err := json.Marshal(b)
	if err != nil {
		return err
	}
	return writeAtomic(w.file(preparedBindingName(b)+".complete"), []byte(bodyHash(string(data))))
}
func (w *Workspace) preparedCompletion(b *PreparedBatch) (bool, error) {
	raw, err := w.readBoundedWorkspace(preparedBindingName(b)+".complete", 64)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	data, err := json.Marshal(b)
	if err != nil {
		return false, err
	}
	if string(raw) != bodyHash(string(data)) {
		return false, refuse("corrupt prepared completion evidence")
	}
	return true, nil
}
func verifyPrepared(ctx context.Context, s *core.Store, b *PreparedBatch) error {
	r, err := writePrepared(ctx, s, b, true)
	if err != nil {
		return err
	}
	for _, o := range r.Outcomes {
		if o.Status != "existing" {
			return refuse("required trial source root is missing")
		}
	}
	return nil
}

// selectionSnapshot validates externally editable evidence at the rehearsal/application boundary.
func (w *Workspace) selectionSnapshot(ctx context.Context) (string, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	entries, err := os.ReadDir(w.Dir)
	if err != nil {
		return "", err
	}
	if len(entries) > 10000 {
		return "", refuse("workspace receipt limit exceeded")
	}
	var evidence []string
	for _, e := range entries {
		name := e.Name()
		if name != "rules.md" && name != "ledger.md" && name != preparedFile && name != selectedPhotoFile && !strings.HasPrefix(name, ".prepared-binding-") && !strings.HasPrefix(name, ".selected-binding-") {
			continue
		}
		raw, e := w.readBoundedWorkspace(name, 4*maxProfileBytes)
		if e != nil {
			return "", e
		}
		evidence = append(evidence, name+":"+bodyHash(string(raw)))
	}
	if _, ok, e := w.read(preparedFile); e != nil {
		return "", e
	} else if ok {
		if _, e = w.readPrepared(ctx, true); e != nil {
			return "", e
		}
	}
	if _, ok, e := w.read(selectedPhotoFile); e != nil {
		return "", e
	} else if ok {
		if _, _, e = w.readSelectedPhoto(ctx, true); e != nil {
			return "", e
		}
	}
	if _, err = w.appliedPrepared(ctx); err != nil {
		return "", err
	}
	photos, err := w.appliedSelectedPhotos(ctx)
	if err != nil {
		return "", err
	}
	for _, p := range photos {
		copy := *p
		if _, e := w.deriveSelectedPhoto(ctx, &copy); e != nil {
			return "", e
		}
		if copy.OriginalSHA256 != p.OriginalSHA256 || copy.SidecarSHA256 != p.SidecarSHA256 {
			return "", refuse("selected receipt source changed")
		}
	}
	sort.Strings(evidence)
	return bodyHash(strings.Join(evidence, string(rune(10)))), nil
}

func (w *Workspace) PreparedReview(ctx context.Context) (*PreparedBatch, error) {
	w.selectionMu.RLock()
	defer w.selectionMu.RUnlock()
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.readPrepared(ctx, false)
}
