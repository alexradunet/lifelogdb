package importer

import (
	"path/filepath"
	"testing"

	"lifelog/internal/core"
	"lifelog/internal/db"
)

func TestCorrectionCopiedTarget(t *testing.T) {
	resetCorrectionHooks(t)
	f, root := importedMoodCorrectionFixture(t)
	v4, v5 := 4.0, 5.0
	legacy, key, err := f.s.Correct(ctx, "cli", root, &v4)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.w.RecordCorrection(key); err != nil {
		t.Fatal(err)
	}
	event, _, err := f.w.CorrectImported(ctx, f.s, "agent:owner", legacy, &v5)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.w.CorrectImported(ctx, f.s, "cli", event, nil); err != nil {
		t.Fatal(err)
	}
	copied := filepath.Join(t.TempDir(), "copy.db")
	if err := db.Copy(f.trial, copied); err != nil {
		t.Fatal(err)
	}
	d, err := db.Open(copied)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	target := &fixture{s: &core.Store{DB: d}, trial: copied}
	for pass := 0; pass < 2; pass++ {
		if n, err := f.w.replayCorrections(ctx, target.s); err != nil || n != 0 {
			t.Fatalf("copied replay %d: count %d, err %v", pass, n, err)
		}
		if rows, _, live := measurementRowsAndValue(t, target); rows != 4 || live {
			t.Fatalf("copied replay changed rows/current to %d/%v", rows, live)
		}
	}
	last := latestMeasurementIDImporter(t, target)
	if _, _, err := target.s.Correct(ctx, "cli", last, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := f.w.replayCorrections(ctx, target.s); err == nil {
		t.Fatal("accepted unrelated later owner retraction on copied target")
	}
	if rows, _, live := measurementRowsAndValue(t, target); rows != 5 || live {
		t.Fatalf("conflict changed rows/current to %d/%v", rows, live)
	}
}
