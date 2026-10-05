package importer

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"lifelog/internal/core"
	"lifelog/internal/db"
)

func TestCorrectionStatus(t *testing.T) {
	resetCorrectionHooks(t)
	f, root := importedMoodCorrectionFixture(t)
	v := 4.0
	if _, _, err := f.w.CorrectImported(ctx, f.s, "agent:owner", root, &v); err != nil {
		t.Fatal(err)
	}
	intents, err := f.w.correctionIntents()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(f.w.Dir, correctionIntentDir, intents[0].EventKey+".json")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	rows, value, _ := measurementRowsAndValue(t, f)
	st, err := statusWithoutChanges(t, f, f.s)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Corrections) != 0 || len(st.Outside) != 0 {
		t.Fatalf("verified correction: %+v", st)
	}
	if _, err := f.s.Record(ctx, "agent:direct", core.Reading{Metric: "Mood", Day: "2031-06-02", Value: 2}); err != nil {
		t.Fatal(err)
	}
	st, err = statusWithoutChanges(t, f, f.s)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Outside) != 1 || !strings.HasPrefix(st.Outside[0], "1 rows") {
		t.Fatalf("outside: %v", st.Outside)
	}
	a, _, err := f.s.CreatePage(ctx, "agent:direct", "Synthetic agent A", "")
	if err != nil {
		t.Fatal(err)
	}
	bID, _, err := f.s.CreatePage(ctx, "agent:direct", "Synthetic agent B", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.s.Link(ctx, "agent:direct", a, bID, "part-of", ""); err != nil {
		t.Fatal(err)
	}
	st, err = statusWithoutChanges(t, f, f.s)
	if err != nil || len(st.Outside) != 1 || !strings.HasPrefix(st.Outside[0], "4 rows") {
		t.Fatalf("direct entity/link status %+v error %v", st, err)
	}
	rules := filepath.Join(f.w.Dir, "rules.md")
	b, err := os.ReadFile(rules)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rules, append(b, []byte("\nDraft change\n")...), 0600); err != nil {
		t.Fatal(err)
	}
	st, err = statusWithoutChanges(t, f, f.s)
	if err != nil {
		t.Fatal(err)
	}
	if st.Gates["rules.md"] == "approved" || !strings.Contains(st.DoNow, "Stop:") {
		t.Fatalf("draft became approved/clean: %+v", st)
	}
	if len(st.Corrections) != 1 || !strings.Contains(st.Corrections[0], "verification blocked by approval") {
		t.Fatalf("draft: %v", st.Corrections)
	}
	if err := f.w.RecoverCorrections(ctx, f.s); err == nil {
		t.Fatal("draft recovery allowed")
	}
	if err := f.w.DraftRules(rulesBody); err != nil {
		t.Fatal(err)
	}
	st, err = statusWithoutChanges(t, f, f.s)
	if err != nil || st.Gates["rules.md"] != "draft" || !strings.Contains(st.DoNow, "Stop:") || len(st.Corrections) != 1 || !strings.Contains(st.Corrections[0], "verification blocked by approval") {
		t.Fatalf("actual draft status %+v error %v", st, err)
	}
	if err := f.w.RecoverCorrections(ctx, f.s); err == nil {
		t.Fatal("actual draft recovery allowed")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("status changed intent")
	}
	if got, val, _ := measurementRowsAndValue(t, f); got != rows+1 || val != value {
		t.Fatalf("status changed rows/value: %d %g", got, val)
	}
}

func TestCorrectionStatusCanonical(t *testing.T) {
	f := setupReadingKeyFixture(t)
	file := "Medical/Status.md"
	writeSource(t, f, file, "2031-10-01 first: 5 ng/mL\n")
	mustLedger(t, f)
	registerMetrics(t, f, Metric{Name: "ferritin", Unit: "ng/mL", Note: "synthetic"})
	oldKey := file + "|reading|Ferritin|2031-10-01|1"
	root := recordReading(t, f, "ferritin", "2031-10-01", "", 5, oldKey)
	if err := f.facts(t, file, map[string]any{"file": file, "writes": []any{readingFact("ferritin", "2031-10-01", "5 ng/mL", "2031-10-01 first: 5 ng/mL", nil)}}); err != nil {
		t.Fatal(err)
	}
	v := 4.0
	event, _, err := f.w.CorrectImported(ctx, f.s, "agent:owner", root, &v)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.w.CorrectImported(ctx, f.s, "agent:owner", event, nil); err != nil {
		t.Fatal(err)
	}
	target := setupReadingKeyFixture(t)
	registerMetrics(t, target, Metric{Name: "ferritin", Unit: "ng/mL", Note: "synthetic"})
	recordReading(t, target, "ferritin", "2031-10-01", "", 5, file+"|reading|ferritin|2031-10-01|1")
	if _, err := f.w.replayCorrections(ctx, target.s); err != nil {
		t.Fatal(err)
	}
	for _, store := range []*core.Store{f.s, target.s} {
		st, err := statusWithoutChanges(t, f, store)
		states := st.Corrections
		if err != nil || len(states) != 0 {
			t.Fatalf("states %v error %v", states, err)
		}
	}
	if err := f.facts(t, file, map[string]any{"file": file, "writes": []any{readingFact("ferritin", "2031-10-01", "6 ng/mL", "2031-10-01 first: 5 ng/mL", nil)}}); err != nil {
		t.Fatal(err)
	}
	st, err := statusWithoutChanges(t, f, target.s)
	states := st.Corrections
	if err != nil || len(states) == 0 {
		t.Fatalf("edited facts states %v error %v", states, err)
	}
	if err := f.facts(t, file, map[string]any{"file": file, "writes": []any{readingFact("ferritin", "2031-10-01", "5 ng/mL", "2031-10-01 first: 5 ng/mL", nil)}}); err != nil {
		t.Fatal(err)
	}
	last := latestMeasurementIDImporter(t, target)
	if _, _, err := target.s.Correct(ctx, "cli", last, nil); err != nil {
		t.Fatal(err)
	}
	st, err = statusWithoutChanges(t, f, target.s)
	states = st.Corrections
	if err != nil || len(states) == 0 {
		t.Fatalf("unknown successor states %v error %v", states, err)
	}
}

// Compare logical rows, not DB/WAL/SHM physical bytes: readers may change SQLite
// bookkeeping without changing stored facts. Every other workspace file is compared.
func statusWithoutChanges(t *testing.T, f *fixture, store *core.Store) (*Status, error) {
	t.Helper()
	files := func() map[string]string {
		out := map[string]string{}
		err := filepath.WalkDir(f.w.Dir, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				return nil
			}
			if strings.HasSuffix(path, ".db") || strings.HasSuffix(path, ".db-wal") || strings.HasSuffix(path, ".db-shm") || strings.HasSuffix(path, ".db-journal") {
				return nil
			}
			b, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			out[path] = string(b)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	rows := func(s *core.Store) map[string][]string {
		names, err := s.DB.R.QueryContext(ctx, `SELECT name FROM sqlite_schema WHERE type='table' ORDER BY name`)
		if err != nil {
			t.Fatal(err)
		}
		var tables []string
		for names.Next() {
			var name string
			if err := names.Scan(&name); err != nil {
				t.Fatal(err)
			}
			tables = append(tables, name)
		}
		if err := names.Err(); err != nil {
			t.Fatal(err)
		}
		names.Close()
		out := map[string][]string{}
		for _, name := range tables {
			rs, err := s.DB.R.QueryContext(ctx, `SELECT * FROM "`+strings.ReplaceAll(name, `"`, `""`)+`"`)
			if err != nil {
				t.Fatal(err)
			}
			cols, err := rs.Columns()
			if err != nil {
				t.Fatal(err)
			}
			out[name] = []string{}
			for rs.Next() {
				values := make([]any, len(cols))
				dest := make([]any, len(cols))
				for i := range dest {
					dest[i] = &values[i]
				}
				if err := rs.Scan(dest...); err != nil {
					t.Fatal(err)
				}
				b, err := json.Marshal(values)
				if err != nil {
					t.Fatal(err)
				}
				out[name] = append(out[name], string(b))
			}
			if err := rs.Err(); err != nil {
				t.Fatal(err)
			}
			rs.Close()
			sort.Strings(out[name])
		}
		return out
	}
	beforeFiles, beforeRows, trialRows := files(), rows(store), rows(f.s)
	st, err := f.w.Status(ctx, store, f.trial)
	if !reflect.DeepEqual(beforeFiles, files()) || !reflect.DeepEqual(beforeRows, rows(store)) || !reflect.DeepEqual(trialRows, rows(f.s)) {
		t.Fatal("status changed workspace bytes or logical database rows")
	}
	return st, err
}

func TestCorrectionStatusCopiedAndWrongWorkspace(t *testing.T) {
	f, root := importedMoodCorrectionFixture(t)
	v := 4.0
	if _, _, err := f.w.CorrectImported(ctx, f.s, "agent:owner", root, &v); err != nil {
		t.Fatal(err)
	}
	copyPath := filepath.Join(t.TempDir(), "copy.db")
	if err := db.Copy(f.trial, copyPath); err != nil {
		t.Fatal(err)
	}
	d, err := db.Open(copyPath)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	st, err := statusWithoutChanges(t, f, &core.Store{DB: d})
	if err != nil || len(st.Corrections) != 0 || len(st.Outside) != 0 {
		t.Fatalf("copied status %+v error %v", st, err)
	}
	intents, err := f.w.correctionIntents()
	if err != nil {
		t.Fatal(err)
	}
	intents[0].WorkspaceSource, intents[0].RootSource = "import:other", "import:other"
	b, err := json.Marshal(intents[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.w.Dir, correctionIntentDir, intents[0].EventKey+".json"), b, 0600); err != nil {
		t.Fatal(err)
	}
	st, err = statusWithoutChanges(t, f, f.s)
	if err != nil || len(st.Corrections) != 1 || !strings.Contains(st.Corrections[0], "belongs to import:other") {
		t.Fatalf("wrong-workspace status %+v error %v", st, err)
	}
}

func TestCorrectionStatusAmbiguousRoots(t *testing.T) {
	f := setupReadingKeyFixture(t)
	file := "Medical/AmbiguousStatus.md"
	writeSource(t, f, file, "2031-10-01 first: 5 ng/mL\n2031-10-01 second: 5 ng/mL\n")
	mustLedger(t, f)
	registerMetrics(t, f, Metric{Name: "ferritin", Unit: "ng/mL", Note: "synthetic"})
	if err := f.facts(t, file, map[string]any{"file": file, "writes": []any{readingFact("Ferritin", "2031-10-01", "5 ng/mL", "2031-10-01 first: 5 ng/mL", nil), readingFact("ferritin", "2031-10-01", "5 ng/mL", "2031-10-01 second: 5 ng/mL", nil)}}); err != nil {
		t.Fatal(err)
	}
	markLedgerDone(t, f, file)
	for _, metric := range []string{"Ferritin", "ferritin"} {
		recordReading(t, f, "ferritin", "2031-10-01", "", 5, fmt.Sprintf("%s|reading|%s|2031-10-01|1", file, metric))
	}
	st, err := statusWithoutChanges(t, f, f.s)
	if err != nil || !strings.Contains(strings.Join(st.Mismatches, "\n"), "ambiguous") {
		t.Fatalf("ambiguous status %+v error %v", st, err)
	}
}
