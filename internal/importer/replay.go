package importer

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"lifelog/internal/core"
	"lifelog/internal/db"
)

// ReplayResult is what a replay did to its target (in a dry run, what it would do), and how the target compares
// with the trial.
type ReplayResult struct {
	Target      string                `json:"target"`
	DryRun      bool                  `json:"dry_run"`
	Initialised bool                  `json:"initialised"`
	Failures    []Failure             `json:"failures"`
	Vault       *VaultResult          `json:"vault,omitempty"`
	Metrics     []string              `json:"metrics,omitempty"`
	Files       []Report              `json:"files"`
	Corrections int                   `json:"corrections_made"`
	Integrity   *core.IntegrityResult `json:"integrity"`
	Trial       *core.Counts          `json:"trial_counts"`
	Counts      *core.Counts          `json:"target_counts"`
	Differences []string              `json:"differences"`
}

// Failure is one part of a rehearsed replay that failed: a step (vault, metrics, corrections, integrity) or one
// facts file.
type Failure struct {
	Step  string `json:"step"`
	File  string `json:"file,omitempty"`
	Error string `json:"error"`
}

func (f Failure) String() string {
	if f.File != "" {
		return fmt.Sprintf("%s %s: %s", f.Step, f.File, f.Error)
	}
	return f.Step + ": " + f.Error
}

func newReplayResult(target string) *ReplayResult {
	return &ReplayResult{Target: target, Failures: []Failure{}, Files: []Report{}, Differences: []string{}}
}

// failed records a failure when rehearsing, so that the rehearsal goes on and reports every one; otherwise the
// failure is the error that stops the replay.
func (r *ReplayResult) failed(rehearse bool, step, file string, err error) error {
	if !rehearse {
		return err
	}
	r.Failures = append(r.Failures, Failure{Step: step, File: file, Error: err.Error()})
	return nil
}

// Replay applies the whole workspace to another database with no model (the guide's "Trial, then the real
// run"). It rehearses first (Rehearse) and writes the target only when the rehearsal failed nowhere and its
// integrity checks were clean; otherwise the target is left as it was, not even created, and the refusal lists
// every failure. A target that does not exist is initialised; an existing one is never re-initialised.
func (w *Workspace) Replay(ctx context.Context, trial *core.Store, target string) (*ReplayResult, error) {
	rehearsal, err := w.Rehearse(ctx, trial, target)
	if err != nil {
		return nil, err
	}
	if len(rehearsal.Failures) > 0 {
		lines := make([]string, len(rehearsal.Failures))
		for i, f := range rehearsal.Failures {
			lines[i] = "- " + f.String()
		}
		return nil, refuse("the replay failed when rehearsed on a copy of %s, so nothing was written to it:\n%s",
			target, strings.Join(lines, "\n"))
	}
	res := newReplayResult(target)
	if !exists(target) {
		if err := db.Init(target); err != nil {
			return nil, err
		}
		res.Initialised = true
	}
	d, err := db.Open(target)
	if err != nil {
		return nil, err
	}
	defer d.Close()
	ts := &core.Store{DB: d}
	if err := w.replayInto(ctx, ts, res, false); err != nil {
		return res, fmt.Errorf("the replay into %s failed after a clean rehearsal, and what it wrote before stays "+
			"(every write is idempotent: fix the cause and replay again): %w", target, err)
	}
	return res, w.compareWithTrial(ctx, trial, ts, res)
}

// Rehearse is a replay's dry run: the whole replay on a throwaway copy of the target (VACUUM INTO, as the trial
// was made; a new database when the target does not exist), going on past each failure so that the result lists
// all of them, then the integrity checks and the comparison with the trial. The target is only read, and the copy
// is removed before Rehearse returns.
func (w *Workspace) Rehearse(ctx context.Context, trial *core.Store, target string) (res *ReplayResult, err error) {
	if err := w.RecoverCorrections(ctx, trial); err != nil {
		return nil, err
	}
	if same(target, w.TrialDB()) {
		return nil, refuse("the target is the trial database itself")
	}
	dir, err := os.MkdirTemp(w.Dir, ".rehearsal-*") // inside the workspace: the copy holds the owner's data
	if err != nil {
		return nil, err
	}
	defer func() {
		if rmErr := os.RemoveAll(dir); rmErr != nil {
			res, err = nil, errors.Join(err, fmt.Errorf("the rehearsal copy %s could not be removed: %w", dir, rmErr))
		}
	}()
	res = newReplayResult(target)
	res.DryRun = true
	copyPath := filepath.Join(dir, "target.db")
	if exists(target) {
		err = db.Copy(target, copyPath)
	} else {
		err, res.Initialised = db.Init(copyPath), true
	}
	if err != nil {
		return nil, err
	}
	d, err := db.Open(copyPath)
	if err != nil {
		return nil, err
	}
	defer d.Close() // before the folder is removed: deferred calls run last in, first out
	ts := &core.Store{DB: d}
	if err := w.replayInto(ctx, ts, res, true); err != nil {
		return nil, err
	}
	if err := w.compareWithTrial(ctx, trial, ts, res); err != nil {
		return nil, err
	}
	if !res.Integrity.OK {
		res.Failures = append(res.Failures, Failure{Step: "integrity", Error: "the integrity checks are not clean on the rehearsal copy"})
	}
	return res, nil
}

// replayInto applies the workspace to ts: the vault plan, the approved metrics, every done or waiting facts file
// in ledger order (a file that names a row not written yet, or links a plain page another file promotes, is retried
// after the rest), then the owner's corrections. Rehearsing, a failing step or file is recorded and the rest goes
// on; otherwise the first stops it.
func (w *Workspace) replayInto(ctx context.Context, ts *core.Store, res *ReplayResult, rehearse bool) error {
	if p, ok, err := w.LoadPlan(); err != nil {
		return err
	} else if ok {
		fresh := &Plan{Notes: make([]Note, len(p.Notes))}
		copy(fresh.Notes, p.Notes)
		for i := range fresh.Notes {
			fresh.Notes[i].Appended = false // the trial's record is not this database's: the text check decides
			fresh.Notes[i].Problems = nil
		}
		if res.Vault, err = w.applyPlan(ctx, ts, fresh, false); err != nil {
			if err := res.failed(rehearse, "vault", "", err); err != nil {
				return err
			}
		}
	}
	if g, _ := w.Gate("metrics.md"); g == "approved" {
		var err error
		if res.Metrics, err = w.RegisterMetrics(ctx, ts); err != nil {
			if err := res.failed(rehearse, "metrics", "", err); err != nil {
				return err
			}
		}
	}
	lines, _, err := w.Ledger()
	if err != nil {
		return err
	}
	var pending []string
	for _, l := range lines {
		if l.State == "x" || l.State == "?" {
			pending = append(pending, l.File)
		}
	}
	for len(pending) > 0 {
		var later []string
		var lastErr error
		waits := map[string]error{} // why each file of later waits
		for _, file := range pending {
			f, pos, rules, err := w.prepare(file)
			if err != nil {
				if err := res.failed(rehearse, "facts", file, err); err != nil {
					return err
				}
				continue
			}
			r, err := w.write(ctx, ts, f, pos, rules, false)
			if err != nil {
				if waitsForAnother(err) {
					later, lastErr = append(later, file), err
					waits[file] = err
					continue
				}
				if err := res.failed(rehearse, "facts", file, err); err != nil {
					return err
				}
				continue
			}
			res.Files = append(res.Files, *r)
		}
		if len(later) == len(pending) { // no progress: a reference no file writes
			if !rehearse {
				return lastErr
			}
			for _, file := range later {
				res.failed(rehearse, "facts", file, waits[file])
			}
			break
		}
		pending = later
	}
	if res.Corrections, err = w.replayCorrections(ctx, ts); err != nil {
		if err := res.failed(rehearse, "corrections", "", err); err != nil {
			return err
		}
	}
	return nil
}

// compareWithTrial runs the integrity checks on ts and compares its counts with the trial's.
func (w *Workspace) compareWithTrial(ctx context.Context, trial, ts *core.Store, res *ReplayResult) error {
	var err error
	if res.Integrity, err = ts.Integrity(ctx); err != nil {
		return err
	}
	if res.Trial, err = trial.Counts(ctx); err != nil {
		return err
	}
	if res.Counts, err = ts.Counts(ctx); err != nil {
		return err
	}
	if d := compare(res.Trial, res.Counts); d != nil {
		res.Differences = d
	}
	return nil
}

// replayCorrections brings each corrected imported reading to the final value the owner gave it on the trial
// (docs/guides/importing.md). Legacy corrections.json remains readable; event intents replay by generated key and
// are idempotent on fresh and copied targets.
func (w *Workspace) replayCorrections(ctx context.Context, ts *core.Store) (int, error) {
	intents, err := w.orderedCorrectionIntents()
	if err != nil {
		return 0, err
	}
	if len(intents) == 0 {
		return w.replayLegacyCorrections(ctx, ts)
	}
	groups := map[correctionRoot][]correctionIntent{}
	for _, intent := range intents {
		legacy, err := w.legacyState(intent.RootSource, intent.Metric, intent.RootImportKey, nil)
		if err != nil {
			return 0, err
		}
		if legacy.fingerprint != intent.LegacyFingerprint {
			return 0, refuse("correction intent %s conflicts with edits to legacy corrections for %s %s", intent.EventKey, intent.Metric, intent.RootImportKey)
		}
		groups[intent.root()] = append(groups[intent.root()], intent)
	}
	n, err := w.replayLegacyRootsWithoutEvents(ctx, ts, groups)
	if err != nil {
		return 0, err
	}
	known := map[string]correctionIntent{}
	for _, intent := range intents {
		known[intent.EventKey] = intent
	}
	for _, events := range groups {
		wrote, err := replayEventCorrections(ctx, ts, events, known)
		if err != nil {
			return 0, err
		}
		n += wrote
	}
	return n, nil
}

func (w *Workspace) replayLegacyCorrections(ctx context.Context, ts *core.Store) (int, error) {
	cs, err := w.Corrections()
	if err != nil || len(cs) == 0 {
		return 0, err
	}
	latest := map[correctionRoot]core.CorrectedKey{}
	var order []correctionRoot
	for _, c := range cs {
		root := correctionRoot{c.Source, c.Metric, c.Key}
		if _, seen := latest[root]; !seen {
			order = append(order, root)
		}
		latest[root] = c
	}
	n := 0
	err = ts.Do(ctx, "cli", func(t *core.Tx) error {
		var missing []string
		for _, root := range order {
			c := latest[root]
			id, err := t.MeasurementByKey(c.Source, c.Metric, c.Key)
			if err != nil {
				return err
			}
			if id == 0 {
				missing = append(missing, c.Metric+" "+c.Key)
				continue
			}
			last, value, ok, err := t.CurrentOf(id)
			if err != nil {
				return err
			}
			if (c.Value == nil && !ok) || (c.Value != nil && ok && *c.Value == value) {
				continue
			}
			if _, err := t.Correct(last, c.Value); err != nil {
				return err
			}
			n++
		}
		if len(missing) > 0 {
			n = 0
			return refuse("corrections name readings the replay did not write: %s", strings.Join(missing, "; "))
		}
		return nil
	})
	return n, err
}

func (w *Workspace) replayLegacyRootsWithoutEvents(ctx context.Context, ts *core.Store, eventRoots map[correctionRoot][]correctionIntent) (int, error) {
	cs, err := w.Corrections()
	if err != nil || len(cs) == 0 {
		return 0, err
	}
	latest := map[correctionRoot]core.CorrectedKey{}
	var order []correctionRoot
	for _, c := range cs {
		root := correctionRoot{c.Source, c.Metric, c.Key}
		if _, seen := latest[root]; !seen {
			order = append(order, root)
		}
		latest[root] = c
	}
	n := 0
	for _, root := range order {
		if _, hasEvents := eventRoots[root]; hasEvents {
			continue
		}
		c := latest[root]
		wrote, err := applyCorrectionValue(ctx, ts, "cli", c.Source, c.Metric, c.Key, c.Value)
		if err != nil {
			return 0, err
		}
		if wrote {
			n++
		}
	}
	return n, nil
}

func replayEventCorrections(ctx context.Context, ts *core.Store, events []correctionIntent, known map[string]correctionIntent) (int, error) {
	n := 0
	if len(events) > 0 && !eventExists(ctx, ts, events[0], known) {
		wrote, err := applyCorrectionBaseline(ctx, ts, events[0])
		if err != nil {
			return 0, err
		}
		if wrote {
			n++
		}
	}
	for i, intent := range events {
		wrote, err := replayOneEventCorrection(ctx, ts, intent, i == 0, known)
		if err != nil {
			return 0, err
		}
		if wrote {
			n++
		}
	}
	return n, nil
}

func replayOneEventCorrection(ctx context.Context, ts *core.Store, intent correctionIntent, first bool, known map[string]correctionIntent) (bool, error) {
	wrote := false
	err := ts.Do(ctx, intent.ActorSource, func(t *core.Tx) error {
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
			return refuse("correction intent %s names a reading the replay did not write: %s", intent.EventKey, intent.RootImportKey)
		}
		leafID, value, ok, err := t.CurrentOf(rootID)
		if err != nil {
			return err
		}
		leaf, err := t.MeasurementRow(leafID)
		if err != nil {
			return err
		}
		if first && intent.PredecessorKind == "legacy" {
			if !valueOK(intent.PredecessorValue, intent.PredecessorRetracted, value, ok) {
				return refuse("correction intent %s legacy predecessor does not match target", intent.EventKey)
			}
		} else if intent.PredecessorKind != "event" || leaf.ImportKey != intent.PredecessorEventKey {
			return refuse("correction intent %s predecessor is %s, not %s", intent.EventKey, leaf.ImportKey, intent.PredecessorEventKey)
		}
		if intent.valueOK(value, ok) && leaf.ImportKey == intent.EventKey {
			return nil
		}
		if _, err := t.CorrectKeyed(leaf.ID, intent.Value, intent.EventKey); err != nil {
			return err
		}
		wrote = true
		return nil
	})
	return wrote, err
}

func eventExists(ctx context.Context, ts *core.Store, intent correctionIntent, known map[string]correctionIntent) bool {
	ok := false
	_ = ts.DryRun(ctx, intent.ActorSource, func(t *core.Tx) error {
		id, err := t.MeasurementByKey(intent.ActorSource, intent.Metric, intent.EventKey)
		ok = err == nil && id != 0 && verifyEventRow(t, intent, id, known) == nil
		return nil
	})
	return ok
}

func applyCorrectionBaseline(ctx context.Context, ts *core.Store, intent correctionIntent) (bool, error) {
	wrote := false
	err := ts.Do(ctx, "cli", func(t *core.Tx) error {
		id, err := t.MeasurementByKey(intent.RootSource, intent.Metric, intent.RootImportKey)
		if err != nil {
			return err
		}
		if id == 0 {
			return refuse("correction intent %s names a reading the replay did not write: %s", intent.EventKey, intent.RootImportKey)
		}
		last, got, ok, err := t.CurrentOf(id)
		if err != nil {
			return err
		}
		leaf, err := t.MeasurementRow(last)
		if err != nil {
			return err
		}
		if valueOK(intent.PredecessorValue, intent.PredecessorRetracted, got, ok) {
			return nil
		}
		if leaf.Supersedes != 0 {
			return refuse("correction intent %s target has an unrelated later correction", intent.EventKey)
		}
		if _, err := t.Correct(last, intent.PredecessorValue); err != nil {
			return err
		}
		wrote = true
		return nil
	})
	return wrote, err
}

func applyCorrectionValue(ctx context.Context, ts *core.Store, actor, source, metric, key string, value *float64) (bool, error) {
	wrote := false
	err := ts.Do(ctx, actor, func(t *core.Tx) error {
		id, err := t.MeasurementByKey(source, metric, key)
		if err != nil {
			return err
		}
		if id == 0 {
			return refuse("corrections name readings the replay did not write: %s %s", metric, key)
		}
		last, got, ok, err := t.CurrentOf(id)
		if err != nil {
			return err
		}
		if (value == nil && !ok) || (value != nil && ok && *value == got) {
			return nil
		}
		if _, err := t.Correct(last, value); err != nil {
			return err
		}
		wrote = true
		return nil
	})
	return wrote, err
}

// compare lists where the target's counts differ from the trial's; rows agents wrote on the trial are expected
// to be missing and are named as such.
func compare(a, b *core.Counts) []string {
	var out []string
	if a.Pages != b.Pages {
		out = append(out, fmt.Sprintf("pages: trial %d, target %d", a.Pages, b.Pages))
	}
	if a.Readings != b.Readings {
		out = append(out, fmt.Sprintf("current readings: trial %d, target %d", a.Readings, b.Readings))
	}
	if a.Metrics != b.Metrics {
		out = append(out, fmt.Sprintf("metrics: trial %d, target %d", a.Metrics, b.Metrics))
	}
	if a.Habits != b.Habits {
		out = append(out, fmt.Sprintf("habit periods: trial %d, target %d", a.Habits, b.Habits))
	}
	for _, m := range []struct {
		name string
		x, y map[string]int
	}{{"entities", a.ByType, b.ByType}, {"links", a.Links, b.Links}} {
		keys := map[string]bool{}
		for k := range m.x {
			keys[k] = true
		}
		for k := range m.y {
			keys[k] = true
		}
		for k := range keys {
			if m.x[k] != m.y[k] {
				out = append(out, fmt.Sprintf("%s %s: trial %d, target %d", m.name, k, m.x[k], m.y[k]))
			}
		}
	}
	for k, n := range a.BySource {
		if strings.HasPrefix(k, "agent:") && b.BySource[k] != n {
			out = append(out, fmt.Sprintf("%d entities by %s on the trial were written outside the facts and are not replayed", n, k))
		}
	}
	return out
}

func same(a, b string) bool {
	sa, err1 := os.Stat(a)
	sb, err2 := os.Stat(b)
	return err1 == nil && err2 == nil && os.SameFile(sa, sb)
}
