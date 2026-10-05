package api_test

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"lifelog/internal/api"
	"lifelog/internal/client"
	"lifelog/internal/core"
	"lifelog/internal/db"
	"lifelog/internal/importer"
)

func approveWorkspaceFile(t *testing.T, ws *importer.Workspace, name string) {
	t.Helper()
	rv, err := ws.Review(name)
	if err != nil {
		t.Fatal(err)
	}
	if err := ws.Approve(name, time.Now(), rv.Hash); err != nil {
		t.Fatal(err)
	}
}

func replayFixture(t *testing.T) (http.Handler, string) {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()
	src := filepath.Join(root, "Notes")
	os.MkdirAll(src, 0o755)
	os.WriteFile(filepath.Join(src, "a.md"), []byte("Lunch at the Pier Cafe.\n"), 0o644)
	ws, err := importer.Open(src + ".lifelog")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ws.Setup(""); err != nil {
		t.Fatal(err)
	}
	if err := ws.DraftRules("source: import:notes\n"); err != nil {
		t.Fatal(err)
	}
	approveWorkspaceFile(t, ws, "rules.md")
	if _, err := ws.MakeLedger(); err != nil {
		t.Fatal(err)
	}
	if err := ws.WriteFacts("a.md", []byte(`{"file":"a.md","writes":[{"place":{"title":"Pier Cafe"},"quote":"at the Pier Cafe"}]}`)); err != nil {
		t.Fatal(err)
	}
	d, err := db.Open(ws.TrialDB())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	s := &core.Store{DB: d}
	if _, err := ws.Apply(ctx, s, "a.md"); err != nil {
		t.Fatal(err)
	}
	return api.New(s, ws), filepath.Join(t.TempDir(), "life.db")
}

// The replay action: dry_run=1 rehearses and writes nothing, an agent may run neither, and the real run writes.
func TestRepeatedImportedCorrectionsReplay(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	src := filepath.Join(root, "Notebook")
	if err := os.MkdirAll(filepath.Join(src, "Medical"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "Medical", "Ferritin.md"), []byte("2031-03-01 48 ng/mL\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ws, err := importer.Open(src + ".lifelog")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ws.Setup(""); err != nil {
		t.Fatal(err)
	}
	if err := ws.DraftRules("source: import:notebook\n"); err != nil {
		t.Fatal(err)
	}
	approveWorkspaceFile(t, ws, "rules.md")
	if err := ws.ProposeMetric(importer.Metric{Name: "Ferritin", Unit: "ng/mL", Note: "synthetic test metric"}); err != nil {
		t.Fatal(err)
	}
	approveWorkspaceFile(t, ws, "metrics.md")
	if _, err := ws.MakeLedger(); err != nil {
		t.Fatal(err)
	}
	d, err := db.Open(ws.TrialDB())
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	trial := &core.Store{DB: d}
	if _, err := ws.RegisterMetrics(ctx, trial); err != nil {
		t.Fatal(err)
	}
	facts := []byte(`{"file":"Medical/Ferritin.md","writes":[{"reading":{"metric":"Ferritin","day":"2031-03-01","value":"48 ng/mL"},"quote":"2031-03-01 48 ng/mL"}]}`)
	if err := ws.WriteFacts("Medical/Ferritin.md", facts); err != nil {
		t.Fatal(err)
	}
	if _, err := ws.Apply(ctx, trial, "Medical/Ferritin.md"); err != nil {
		t.Fatal(err)
	}

	var imported int64
	if err := trial.Do(ctx, "import:notebook", func(tx *core.Tx) error {
		var err error
		imported, err = tx.MeasurementByKey("import:notebook", "Ferritin", "Medical/Ferritin.md|reading|Ferritin|2031-03-01|1")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	h := api.New(trial, ws)
	owner := client.InProcess(h, "cli")
	catalog, err := owner.Catalog()
	if err != nil {
		t.Fatal(err)
	}
	actions := &api.Entity{Actions: catalog}
	correct := find(actions, "correct")
	retract := find(actions, "retract")

	first := must(owner.Do(correct, map[string]string{"id": strconv.FormatInt(imported, 10), "value": "47"}))
	second := must(owner.Do(correct, map[string]string{"id": measurementID(first), "value": "46"}))
	if _, err := owner.Do(retract, map[string]string{"id": measurementID(second)}); err != nil {
		t.Fatal(err)
	}
	if day, err := trial.Day(ctx, "2031-03-01"); err != nil || len(day.Readings) != 0 {
		t.Fatalf("after retraction readings = %+v, err %v; want none", day, err)
	}
	retracted := latestMeasurementID(t, trial)
	if _, err := owner.Do(correct, map[string]string{"id": strconv.FormatInt(retracted, 10), "value": "45"}); err != nil {
		t.Fatal(err)
	}
	corrections, err := ws.Corrections()
	if err != nil {
		t.Fatal(err)
	}
	if len(corrections) != 4 {
		t.Fatalf("corrections log has %d records, want 4", len(corrections))
	}
	for i, c := range corrections {
		if c.Source != "import:notebook" || c.Key != "Medical/Ferritin.md|reading|Ferritin|2031-03-01|1" || c.Metric != "Ferritin" {
			t.Fatalf("correction %d = %+v, want same imported root", i, c)
		}
	}
	logBefore, err := os.ReadFile(filepath.Join(src+".lifelog", "corrections.json"))
	if err != nil {
		t.Fatal(err)
	}

	target := filepath.Join(t.TempDir(), "life.db")
	res, err := ws.Replay(ctx, trial, target)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Initialised || res.Corrections != 1 || len(res.Differences) != 0 || !res.Integrity.OK {
		t.Fatalf("first replay = %+v", res)
	}
	if value, rows := targetReadingState(t, target); value != 45 || rows != 2 {
		t.Fatalf("target after first replay value %g rows %d, want value 45 with root+one correction", value, rows)
	}
	res, err = ws.Replay(ctx, trial, target)
	if err != nil {
		t.Fatal(err)
	}
	if res.Corrections != 0 || len(res.Differences) != 0 {
		t.Fatalf("second replay = %+v", res)
	}
	if value, rows := targetReadingState(t, target); value != 45 || rows != 2 {
		t.Fatalf("target after second replay value %g rows %d, want no total-row growth", value, rows)
	}
	if logAfter, err := os.ReadFile(filepath.Join(src+".lifelog", "corrections.json")); err != nil || string(logAfter) != string(logBefore) {
		t.Fatalf("replay changed corrections log: err %v\nbefore %s\nafter %s", err, logBefore, logAfter)
	}

	bad := 44.0
	if err := ws.RecordCorrection(core.CorrectedKey{Source: "import:notebook", Metric: "Ferritin", Key: "missing-root", Value: &bad}); err != nil {
		t.Fatal(err)
	}
	before := targetMeasurementRows(t, target)
	if _, err := ws.Replay(ctx, trial, target); err == nil || !strings.Contains(err.Error(), "so nothing was written") {
		t.Fatalf("missing-root replay error = %v, want refusal before target write", err)
	}
	if after := targetMeasurementRows(t, target); after != before {
		t.Fatalf("missing-root replay changed target rows from %d to %d", before, after)
	}
}

func latestMeasurementID(t *testing.T, s *core.Store) int64 {
	t.Helper()
	var id int64
	if err := s.DB.R.QueryRowContext(context.Background(), `SELECT id FROM measurements ORDER BY id DESC LIMIT 1`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func targetReadingState(t *testing.T, path string) (float64, int) {
	t.Helper()
	d, err := db.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	var value float64
	if err := d.R.QueryRowContext(context.Background(), `SELECT me.value FROM measurement_values me JOIN pages p ON p.id = me.metric_id WHERE p.title = 'Ferritin'`).Scan(&value); err != nil {
		t.Fatal(err)
	}
	return value, targetMeasurementRowsOpen(t, d)
}

func targetMeasurementRows(t *testing.T, path string) int {
	t.Helper()
	d, err := db.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	return targetMeasurementRowsOpen(t, d)
}

func targetMeasurementRowsOpen(t *testing.T, d *db.DB) int {
	t.Helper()
	var rows int
	if err := d.R.QueryRowContext(context.Background(), `SELECT count(*) FROM measurements`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	return rows
}

func TestReplayDryRunAction(t *testing.T) {
	h, target := replayFixture(t)
	replay := func(c *client.Client, vals map[string]string) (*api.Entity, error) {
		actions, err := c.Catalog()
		if err != nil {
			t.Fatal(err)
		}
		for _, a := range actions {
			if a.Name == "replay" {
				return c.Do(a, vals)
			}
		}
		t.Fatal("no replay action")
		return nil, nil
	}
	owner := client.InProcess(h, "cli")

	e, err := replay(owner, map[string]string{"to": target, "dry_run": "1"})
	if err != nil {
		t.Fatal(err)
	}
	p := e.Properties.(map[string]any)
	if p["dry_run"] != true || len(p["failures"].([]any)) != 0 || len(p["files"].([]any)) != 1 {
		t.Errorf("a dry run: %v", p)
	}
	if _, err := os.Stat(target); err == nil {
		t.Error("a dry run created the target")
	}
	var ce *client.Error
	if _, err := replay(owner, map[string]string{"to": target, "dry_run": "yes"}); !errors.As(err, &ce) || ce.Status != 422 {
		t.Errorf("dry_run=yes: %v", err)
	}
	if _, err := replay(client.InProcess(h, "agent:test"), map[string]string{"to": target, "dry_run": "1"}); !errors.As(err, &ce) || ce.Status != 403 {
		t.Errorf("an agent's dry run: %v", err)
	}
	if e, err = replay(owner, map[string]string{"to": target}); err != nil {
		t.Fatal(err)
	}
	if p := e.Properties.(map[string]any); p["dry_run"] != false || p["initialised"] != true {
		t.Errorf("the real run: %v", p)
	}
	if _, err := os.Stat(target); err != nil {
		t.Error("the real run did not write the target")
	}
}
