package importer

import (
	"context"
	"fmt"
	"os"
	"strings"

	"lifelog/internal/core"
	"lifelog/internal/db"
)

// ReplayResult is what a replay did to its target, and how the target compares with the trial.
type ReplayResult struct {
	Target      string                `json:"target"`
	Initialised bool                  `json:"initialised"`
	Vault       *VaultResult          `json:"vault,omitempty"`
	Metrics     []string              `json:"metrics,omitempty"`
	Files       []Report              `json:"files"`
	Corrections int                   `json:"corrections_made"`
	Integrity   *core.IntegrityResult `json:"integrity"`
	Trial       *core.Counts          `json:"trial_counts"`
	Counts      *core.Counts          `json:"target_counts"`
	Differences []string              `json:"differences"`
}

// Replay applies the whole workspace to another database with no model (the guide's "Trial, then the real
// run"): the vault plan, the approved metrics, every done or waiting facts file in ledger order (a file that
// names a row not written yet, or links a plain page another file promotes, is retried after the rest), the
// owner's corrections, then the integrity checks.
// A target that does not exist is initialised; an existing one is never re-initialised.
func (w *Workspace) Replay(ctx context.Context, trial *core.Store, target string) (*ReplayResult, error) {
	res := &ReplayResult{Target: target, Files: []Report{}, Differences: []string{}}
	if same(target, w.TrialDB()) {
		return nil, refuse("the target is the trial database itself")
	}
	if _, err := os.Stat(target); err != nil {
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

	if p, ok, err := w.LoadPlan(); err != nil {
		return nil, err
	} else if ok {
		fresh := &Plan{Notes: make([]Note, len(p.Notes))}
		copy(fresh.Notes, p.Notes)
		for i := range fresh.Notes {
			fresh.Notes[i].Appended = false // the trial's record is not this database's: the text check decides
			fresh.Notes[i].Problems = nil
		}
		if res.Vault, err = w.applyPlan(ctx, ts, fresh, false); err != nil {
			return res, err
		}
	}
	if g, _ := w.Gate("metrics.md"); g == "approved" {
		if res.Metrics, err = w.RegisterMetrics(ctx, ts); err != nil {
			return res, err
		}
	}
	lines, _, err := w.Ledger()
	if err != nil {
		return res, err
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
		for _, file := range pending {
			f, pos, rules, err := w.prepare(file)
			if err != nil {
				return res, err
			}
			r, err := w.write(ctx, ts, f, pos, rules, false)
			if err != nil {
				if waitsForAnother(err) {
					later, lastErr = append(later, file), err
					continue
				}
				return res, err
			}
			res.Files = append(res.Files, *r)
		}
		if len(later) == len(pending) {
			return res, lastErr // no progress: a reference no file writes
		}
		pending = later
	}
	if res.Corrections, err = w.replayCorrections(ctx, ts); err != nil {
		return res, err
	}
	if res.Integrity, err = ts.Integrity(ctx); err != nil {
		return res, err
	}
	if res.Trial, err = trial.Counts(ctx); err != nil {
		return res, err
	}
	if res.Counts, err = ts.Counts(ctx); err != nil {
		return res, err
	}
	if d := compare(res.Trial, res.Counts); d != nil {
		res.Differences = d
	}
	return res, nil
}

// replayCorrections brings each corrected imported reading to the value the owner gave it on the trial.
func (w *Workspace) replayCorrections(ctx context.Context, ts *core.Store) (int, error) {
	cs, err := w.Corrections()
	if err != nil || len(cs) == 0 {
		return 0, err
	}
	n := 0
	err = ts.Do(ctx, "cli", func(t *core.Tx) error {
		for _, c := range cs {
			id, err := t.MeasurementByKey(c.Source, c.Metric, c.Key)
			if err != nil {
				return err
			}
			if id == 0 {
				return refuse("a correction names %s %s, which the replay did not write", c.Metric, c.Key)
			}
			last, value, ok, err := t.CurrentOf(id)
			if err != nil {
				return err
			}
			if (c.Value == nil && !ok) || (c.Value != nil && ok && *c.Value == value) {
				continue // already so
			}
			if _, err := t.Correct(last, c.Value); err != nil {
				return err
			}
			n++
		}
		return nil
	})
	return n, err
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
