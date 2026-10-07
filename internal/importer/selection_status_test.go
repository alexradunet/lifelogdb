package importer

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"lifelog/internal/core"
	"lifelog/internal/db"
	"lifelog/internal/photo"
	"lifelog/internal/photo/phototest"
)

func statusSelectionWorkspace(t *testing.T) *fixture {
	t.Helper()
	f := setup(t)
	// setup owns this synthetic t.TempDir source; use only the files this test selects.
	if err := os.RemoveAll(f.w.Source); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(f.w.Source, 0700); err != nil {
		t.Fatal(err)
	}
	f.approveRules(t, rulesBody)
	return f
}

func TestSelectionStatusRetainsCompletedPhotoHistory(t *testing.T) {
	f := statusSelectionWorkspace(t)
	for i, name := range []string{"one.jpg", "two.jpg"} {
		if err := os.WriteFile(filepath.Join(f.w.Source, name), phototest.JPEG(i+3, 2, photo.Meta{Orientation: 1}, true), 0600); err != nil {
			t.Fatal(err)
		}
	}
	mustLedger(t, f)
	for _, name := range []string{"one.jpg", "two.jpg"} {
		if _, err := f.w.DraftSelectedPhoto(ctx, name, "", name, "", PhotoChoices{Capture: "none", GPS: "none"}); err != nil {
			t.Fatal(err)
		}
		if err := ownerApproves(f.w, selectedPhotoFile); err != nil {
			t.Fatal(err)
		}
		if _, err := f.w.SelectedPhoto(ctx, f.s, false); err != nil {
			t.Fatal(err)
		}
		status, err := f.w.Status(ctx, f.s, f.trial)
		if err != nil {
			t.Fatal(err)
		}
		if len(status.Mismatches) != 0 {
			t.Errorf("after applying %s, completed selected history is reported as mismatched: %v", name, status.Mismatches)
		}
	}
	var count int
	if err := f.s.DB.R.QueryRow(`SELECT count(*) FROM files`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("two distinct stored photo control: count=%d err=%v", count, err)
	}
}

func TestSelectedReplayDoesNotRecreateTombstonedTrialFile(t *testing.T) {
	f := statusSelectionWorkspace(t)
	if err := os.WriteFile(filepath.Join(f.w.Source, "one.jpg"), phototest.JPEG(3, 2, photo.Meta{Orientation: 1}, true), 0600); err != nil {
		t.Fatal(err)
	}
	mustLedger(t, f)
	if _, err := f.w.DraftSelectedPhoto(ctx, "one.jpg", "", "One.jpg", "", PhotoChoices{Capture: "none", GPS: "none"}); err != nil {
		t.Fatal(err)
	}
	if err := ownerApproves(f.w, selectedPhotoFile); err != nil {
		t.Fatal(err)
	}
	kept, err := f.w.SelectedPhoto(ctx, f.s, false)
	if err != nil {
		t.Fatal(err)
	}
	liveTarget := filepath.Join(t.TempDir(), "live-control.db")
	if _, err = f.w.Replay(ctx, f.s, liveTarget); err != nil {
		t.Fatalf("live selection replay control: %v", err)
	}
	liveDB, e := db.Open(liveTarget)
	if e != nil {
		t.Fatal(e)
	}
	defer liveDB.Close()
	liveStore := &core.Store{DB: liveDB}
	targetID, e := liveStore.PageID(ctx, "One.jpg")
	if e != nil {
		t.Fatal(e)
	}
	if e = liveStore.Tombstone(ctx, "cli", targetID); e != nil {
		t.Fatal(e)
	}
	targetPrior, e := liveStore.PageByID(ctx, targetID)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.w.Replay(ctx, f.s, liveTarget); e != nil {
		t.Fatalf("existing owner tombstone replay control %v", e)
	}
	targetAfter, e := liveStore.PageByID(ctx, targetID)
	if e != nil || !reflect.DeepEqual(targetPrior, targetAfter) {
		t.Fatal("existing target owner tombstone revived")
	}
	if err = f.s.Tombstone(ctx, "cli", kept.ID); err != nil {
		t.Fatal(err)
	}
	page, err := f.s.PageByID(ctx, kept.ID)
	if err != nil || page.DeletedAt == "" {
		t.Fatalf("trial tombstone setup: %+v %v", page, err)
	}
	beforePage := page
	for _, dry := range []bool{true, false} {
		same, e := f.w.SelectedPhoto(ctx, f.s, dry)
		if e != nil || !same.Deleted || same.ID != kept.ID {
			t.Fatalf("same-store tombstone %+v %v", same, e)
		}
	}
	after, e := f.s.PageByID(ctx, kept.ID)
	if e != nil || !reflect.DeepEqual(beforePage, after) {
		t.Fatal("same-store retry revived owner state")
	}
	// Apply may finish existing publication, but a subsequent status/check is strictly observational.
	beforeFiles := statusWorkspaceFiles(t, f.w)
	if _, e = f.w.Status(ctx, f.s, f.trial); e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(beforeFiles, statusWorkspaceFiles(t, f.w)) {
		t.Fatal("status changed tombstone receipt")
	}
	existing := filepath.Join(t.TempDir(), "existing.db")
	if e = db.Init(existing); e != nil {
		t.Fatal(e)
	}
	d, e := db.Open(existing)
	if e != nil {
		t.Fatal(e)
	}
	defer d.Close()
	store := &core.Store{DB: d}
	sentinel, _, e := store.CreatePage(ctx, "cli", "Sentinel", "Retained")
	if e != nil {
		t.Fatal(e)
	}
	prior, e := store.PageByID(ctx, sentinel)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.w.Rehearse(ctx, f.s, existing); e == nil {
		t.Fatal("unsupported tombstone rehearsed")
	}
	if _, e = f.w.Replay(ctx, f.s, existing); e == nil {
		t.Fatal("unsupported tombstone replayed")
	}
	current, e := store.PageByID(ctx, sentinel)
	if e != nil || !reflect.DeepEqual(prior, current) {
		t.Fatal("existing target owner state changed")
	}
	var photos int
	if e = d.R.QueryRow(`SELECT count(*) FROM files`).Scan(&photos); e != nil || photos != 0 {
		t.Fatal("existing target gained selected photo")
	}
	target := filepath.Join(t.TempDir(), "must-not-exist.db")
	if result, err := f.w.Replay(ctx, f.s, target); err == nil {
		d, openErr := db.OpenSnapshot(target)
		if openErr != nil {
			t.Fatal(openErr)
		}
		defer d.Close()
		var live bool
		if queryErr := d.R.QueryRow(`SELECT e.deleted_at IS NULL FROM files f JOIN entities e ON e.id=f.id`).Scan(&live); queryErr != nil {
			t.Fatal(queryErr)
		}
		t.Errorf("tombstoned source selection recreated in fresh target: live=%v result=%+v", live, result)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Errorf("tombstoned trial selection produced a target: %v", err)
	}
}

func TestPreparedStatusRequiresPersistedOriginalRoots(t *testing.T) {
	f := statusSelectionWorkspace(t)
	writeSource(t, f, "daily.csv", "Date,Distance (m)\n2020-01-02,10.5\n")
	mustLedger(t, f)
	registerMetrics(t, f, Metric{Name: "Distance", Unit: "m", Note: "Source distance"})
	if _, err := f.w.DraftPrepared(ctx, "daily.csv", "fit-date-csv-v1", "", nil, map[string]string{"distance": "Distance"}); err != nil {
		t.Fatal(err)
	}
	if err := ownerApproves(f.w, preparedFile); err != nil {
		t.Fatal(err)
	}
	if _, err := f.w.Prepared(ctx, f.s, false); err != nil {
		t.Fatal(err)
	}
	good, err := f.w.Status(ctx, f.s, f.trial)
	if err != nil || len(good.Mismatches) != 0 {
		t.Fatalf("actual applied control: %+v %v", good, err)
	}
	// A fresh canonical database has the metric but not the supposedly applied source root.
	path := filepath.Join(t.TempDir(), "missing-root.db")
	if err := db.Init(path); err != nil {
		t.Fatal(err)
	}
	d, err := db.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	s := &core.Store{DB: d}
	if _, err = s.RegisterMetric(ctx, "cli", "Distance", "m", "Source distance"); err != nil {
		t.Fatal(err)
	}
	missing, err := f.w.Status(ctx, s, path)
	if err != nil {
		t.Fatal(err)
	}
	if len(missing.Mismatches) == 0 {
		t.Errorf("missing applied root reported as consistent: do_now=%q readings=%d", missing.DoNow, missing.Counts.Readings)
	}
	var count int
	if err := d.R.QueryRow(`SELECT count(*) FROM measurements`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("status must not insert the missing root: %d %v", count, err)
	}
}
