package importer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"lifelog/internal/db"
)

func TestReplaySerializesWorkspaceSourceMutation(t *testing.T) {
	f := setup(t)
	f.approveRules(t, rulesBody)
	writeSource(t, f, "bound.csv", "Date,Distance (m)\n2020-01-02,10\n")
	mustLedger(t, f)
	registerMetrics(t, f, Metric{Name: "Distance", Unit: "m", Note: "Source"})
	if _, e := f.w.DraftPrepared(ctx, "bound.csv", "fit-date-csv-v1", "", nil, map[string]string{"distance": "Distance"}); e != nil {
		t.Fatal(e)
	}
	if e := ownerApproves(f.w, preparedFile); e != nil {
		t.Fatal(e)
	}
	if _, e := f.w.Prepared(ctx, f.s, false); e != nil {
		t.Fatal(e)
	}
	started := make(chan struct{})
	finished := make(chan error, 1)
	target := filepath.Join(t.TempDir(), "target.db")
	_, e := f.w.replayAfterRehearsal(ctx, f.s, target, func() error {
		go func() {
			close(started)
			finished <- f.w.DraftRules(strings.Replace(rulesBody, "import:notebook", "import:concurrent", 1))
		}()
		<-started
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
	if e = <-finished; e != nil {
		t.Fatal(e)
	}
	d, e := db.Open(target)
	if e != nil {
		t.Fatal(e)
	}
	defer d.Close()
	var source string
	if e = d.R.QueryRow(`SELECT source FROM measurements`).Scan(&source); e != nil || source != "import:notebook" {
		t.Fatalf("linearized source %s %v", source, e)
	}
	if raw, e := os.ReadFile(filepath.Join(f.w.Dir, "rules.md")); e != nil || !strings.Contains(string(raw), "import:concurrent") {
		t.Fatal("blocked mutation was lost")
	}
}
