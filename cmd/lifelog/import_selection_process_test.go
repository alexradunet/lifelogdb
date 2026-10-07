package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"lifelog/internal/core"
	"lifelog/internal/db"
	"lifelog/internal/importer"
	"lifelog/internal/photo"
	"lifelog/internal/photo/phototest"
)

func TestExecutablePreparedSourceAndOwnerStampBoundary(t *testing.T) {
	root := t.TempDir()
	binary := filepath.Join(root, "lifelog.exe")
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if out, e := exec.CommandContext(ctx, "go", "build", "-mod=readonly", "-o", binary, ".").CombinedOutput(); e != nil {
		t.Fatalf("build %v %s", e, out)
	}
	source := filepath.Join(root, "Source")
	if e := os.Mkdir(source, 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(source, "daily.csv"), []byte("Date,Distance (m)\n2020-01-02,10\n"), 0600); e != nil {
		t.Fatal(e)
	}
	ws, e := importer.Open(source + ".lifelog")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = ws.Setup(""); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(source, "one.jpg"), phototest.JPEG(3, 2, photo.Meta{Orientation: 1}, true), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e = ws.MakeLedger(); e != nil {
		t.Fatal(e)
	}
	if e = ws.DraftRules("source: import:synthetic\n"); e != nil {
		t.Fatal(e)
	}
	approve := func(name string) {
		t.Helper()
		r, e := ws.Review(name)
		if e != nil {
			t.Fatal(e)
		}
		if e = ws.Approve(name, time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC), r.Hash); e != nil {
			t.Fatal(e)
		}
	}
	approve("rules.md")
	d, e := db.Open(ws.TrialDB())
	if e != nil {
		t.Fatal(e)
	}
	if _, e = (&core.Store{DB: d}).RegisterMetric(ctx, "cli", "Distance", "m", ""); e != nil {
		t.Fatal(e)
	}
	if e = d.Close(); e != nil {
		t.Fatal(e)
	}
	call := func(wantSuccess bool, args ...string) string {
		t.Helper()
		cmd := exec.CommandContext(ctx, binary, append([]string{"--workspace", ws.Dir}, args...)...)
		cmd.Dir = root
		out, e := cmd.CombinedOutput()
		if (e == nil) != wantSuccess {
			t.Fatalf("%v %v %s", args, e, out)
		}
		return string(out)
	}
	call(true, "do", "draft-prepared", "file=daily.csv", "profile=fit-date-csv-v1", `metrics={"distance":"Distance"}`)
	call(false, "do", "apply-prepared")
	if out := call(false, "import", "approve", "prepared"); !strings.Contains(out, "interactive terminal") {
		t.Fatal("process stamp boundary missing")
	}
	approve("prepared.md")
	call(true, "do", "check-prepared")
	call(true, "do", "apply-prepared")
	d, e = db.Open(ws.TrialDB())
	if e != nil {
		t.Fatal(e)
	}
	defer d.Close()
	var value float64
	var day, src string
	if e = d.R.QueryRow(`SELECT value,day,source FROM measurements`).Scan(&value, &day, &src); e != nil || value != 10 || day != "2020-01-02" || src != "import:synthetic" {
		t.Fatalf("process meaning %g %s %s %v", value, day, src, e)
	}
	if e = d.Close(); e != nil {
		t.Fatal(e)
	}
	call(true, "do", "draft-selected-photo", "file=one.jpg", "title=One.jpg", `choices={"capture":"none","gps":"none"}`)
	call(false, "do", "apply-selected-photo")
	approve("selected-photo.md")
	call(true, "do", "check-selected-photo")
	call(true, "do", "apply-selected-photo")
	d, e = db.Open(ws.TrialDB())
	if e != nil {
		t.Fatal(e)
	}
	defer d.Close()
	var unknown bool
	if e = d.R.QueryRow(`SELECT e.day IS NULL FROM files f JOIN entities e ON e.id=f.id`).Scan(&unknown); e != nil || !unknown {
		t.Fatal("process selected unknown day")
	}

}
