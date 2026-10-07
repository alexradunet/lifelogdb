package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"lifelog/internal/api"
	"lifelog/internal/client"
	"lifelog/internal/core"
	"lifelog/internal/db"
	"lifelog/internal/importer"
	"lifelog/internal/photo"
	"lifelog/internal/photo/phototest"
)

// TestSnapshot is `lifelog snapshot` on a live file the writer still holds open (docs/cookbook/take-a-snapshot.md):
// the dated name, the time added on a second snapshot that day, never an existing file, the restore check passing
// and leaving the snapshot as it was.
func TestSnapshot(t *testing.T) {
	ctx := context.Background()
	live := filepath.Join(t.TempDir(), "life.db")
	if err := db.Init(live); err != nil {
		t.Fatal(err)
	}
	d, err := db.Open(live)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if _, _, err := (&core.Store{DB: d}).Capture(ctx, "cli", "2026-10-02", "Walked to [[Lakeside]] with [[Sam]].", nil); err != nil {
		t.Fatal(err)
	}
	day := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := range 300 { // enough rows that PRAGMA optimize would have statistics to write into the snapshot
		if _, _, err := (&core.Store{DB: d}).Capture(ctx, "cli", day.AddDate(0, 0, i).Format("2006-01-02"), fmt.Sprintf("Ran with [[Sam]] past [[Note %d]].", i), nil); err != nil {
			t.Fatal(err)
		}
	}

	dir := t.TempDir()
	now := time.Date(2026, 10, 2, 14, 30, 5, 0, time.Local)
	first, res, err := takeSnapshot(ctx, live, dir, now)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(first) != "life-2026-10-02.db" {
		t.Errorf("the first snapshot of the day is %s, want life-2026-10-02.db", filepath.Base(first))
	}
	if !res.OK {
		t.Errorf("the restore check failed on a clean file: %+v", res)
	}
	// the restore check, as takeSnapshot runs it, on a snapshot no check has touched yet
	untouched, err := db.Snapshot(live, t.TempDir(), now)
	if err != nil {
		t.Fatal(err)
	}
	b0 := mustRead(t, untouched)
	r, err := db.OpenSnapshot(untouched)
	if err != nil {
		t.Fatal(err)
	}
	var n int
	r.R.QueryRow("SELECT count(*) FROM entity_names WHERE title IN ('2026-10-02', 'Lakeside', 'Sam')").Scan(&n)
	if res, err := (&core.Store{DB: r}).Integrity(ctx); err != nil || !res.OK {
		t.Errorf("the restore check on the snapshot: %+v %v", res, err)
	}
	r.Close()
	if n != 3 {
		t.Errorf("the snapshot holds %d of the 3 pages", n)
	}
	if !bytes.Equal(b0, mustRead(t, untouched)) {
		t.Error("the restore check changed the snapshot (PRAGMA optimize at close?)")
	}

	before := mustRead(t, first)
	second, res, err := takeSnapshot(ctx, live, dir, now)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(second) != "life-2026-10-02T143005.db" || !res.OK {
		t.Errorf("the second snapshot of the day is %s (restore check ok: %v), want life-2026-10-02T143005.db", filepath.Base(second), res.OK)
	}
	after, _ := os.ReadFile(first)
	if !bytes.Equal(before, after) {
		t.Error("the restore check or the second snapshot changed the first snapshot")
	}
	if _, _, err := takeSnapshot(ctx, live, dir, now); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("a third snapshot in the same second: %v, want a refusal", err)
	}
	if !bytes.Equal(before, mustRead(t, first)) {
		t.Error("a refused snapshot changed an existing file")
	}
	// SQLite itself writes into an existing empty file: the refusal is the writer's own
	next := now.AddDate(0, 0, 1)
	for _, name := range []string{"life-2026-10-03.db", "life-2026-10-03T143005.db"} {
		os.WriteFile(filepath.Join(dir, name), nil, 0o644)
	}
	if _, _, err := takeSnapshot(ctx, live, dir, next); err == nil {
		t.Error("a snapshot onto an existing empty file was not refused")
	}
	if b := mustRead(t, filepath.Join(dir, "life-2026-10-03T143005.db")); len(b) != 0 {
		t.Errorf("a refused snapshot wrote %d bytes into an existing empty file", len(b))
	}
}

func TestSnapshotLiteralSpecialCharacterPaths(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	liveDir := filepath.Join(root, "live café %23")
	live := filepath.Join(liveDir, "life %23.db")
	writeSnapshotSentinel(t, filepath.Join(liveDir, "life #.db"))
	if err := os.MkdirAll(liveDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := db.Init(live); err != nil {
		t.Fatalf("Init(%q) failed with alternate sentinel %q: %v", live, filepath.Join(liveDir, "life #.db"), err)
	}
	d, err := db.Open(live)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := (&core.Store{DB: d}).Capture(ctx, "cli", "2026-10-05", "Literal snapshot path for [[Sam]].", nil); err != nil {
		d.Close()
		t.Fatal(err)
	}
	d.Close()

	dir := filepath.Join(root, "snapshots %23 café")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	destinationSentinel := filepath.Join(root, "snapshots # café", "life-2026-10-05.db")
	writeSnapshotSentinel(t, destinationSentinel)
	now := time.Date(2026, 10, 5, 8, 9, 10, 0, time.Local)
	got, res, err := takeSnapshot(ctx, live, dir, now)
	if err != nil {
		t.Fatalf("takeSnapshot(%q, %q) failed: %v", live, dir, err)
	}
	want := filepath.Join(dir, "life-2026-10-05.db")
	assertSnapshotDestination(t, got, want)
	if !res.OK {
		t.Fatalf("restore check failed on literal snapshot path: %+v", res)
	}
	assertSnapshotSentinel(t, filepath.Join(liveDir, "life #.db"))
	assertSnapshotSentinel(t, destinationSentinel)
	snap, err := db.OpenSnapshot(want)
	if err != nil {
		t.Fatal(err)
	}
	defer snap.Close()
	var n int
	if err := snap.R.QueryRow(`SELECT count(*) FROM entity_names WHERE title IN ('2026-10-05', 'Sam')`).Scan(&n); err != nil || n != 2 {
		t.Fatalf("literal snapshot %q has %d captured pages (%v), want 2", want, n, err)
	}
}

const snapshotSentinel = "literal snapshot sentinel\n"

func assertSnapshotDestination(t *testing.T, got, want string) {
	t.Helper()
	// Resolving the destination directory can expand Windows short names. The
	// dated filename and actual destination file must still match exactly.
	if filepath.Base(got) != filepath.Base(want) {
		t.Errorf("snapshot filename = %q, want %q", filepath.Base(got), filepath.Base(want))
	}
	gotInfo, err := os.Lstat(got)
	if err != nil {
		t.Errorf("returned snapshot %q does not exist: %v", got, err)
		return
	}
	wantInfo, err := os.Lstat(want)
	if err != nil {
		t.Errorf("literal snapshot destination %q does not exist: %v", want, err)
		return
	}
	if !gotInfo.Mode().IsRegular() || !wantInfo.Mode().IsRegular() || !os.SameFile(gotInfo, wantInfo) {
		t.Errorf("snapshot %q is not the regular file at literal destination %q", got, want)
	}
}

func writeSnapshotSentinel(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(snapshotSentinel), 0o644); err != nil {
		t.Fatal(err)
	}
}

func assertSnapshotSentinel(t *testing.T, p string) {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("alternate sentinel %q was removed: %v", p, err)
	}
	if string(b) != snapshotSentinel {
		t.Fatalf("alternate sentinel %q changed to %q", p, string(b))
	}
}

func TestImportTakeoutInventoryDoesNotNeedDatabaseOrLeakValues(t *testing.T) {
	o, err := parse([]string{"import", "takeout", "inventory", filepath.Join("..", "..", "internal", "takeout", "testdata", "phase-a")})
	if err != nil {
		t.Fatal(err)
	}
	out := captureStdout(t, func() {
		if err := run(o); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "Timeline Semantic Visits") || !strings.Contains(out, "Fitbit Steps") {
		t.Fatalf("inventory output missed expected public families:\n%s", out)
	}
	if strings.Contains(out, "PRIVATE_MARKER") {
		t.Fatalf("inventory output leaked a synthetic private marker:\n%s", out)
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	f, err := os.CreateTemp(t.TempDir(), "stdout-*.json")
	if err != nil {
		t.Fatal(err)
	}
	closed := false
	os.Stdout = f
	defer func() {
		os.Stdout = old
		if !closed {
			f.Close()
		}
	}()
	fn()
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	closed = true
	b, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestSnapshotRefusesAGitWorkTree(t *testing.T) {
	live := filepath.Join(t.TempDir(), "life.db")
	if err := db.Init(live); err != nil {
		t.Fatal(err)
	}
	repo := t.TempDir()
	os.Mkdir(filepath.Join(repo, ".git"), 0o755)
	sub := filepath.Join(repo, "backups")
	os.Mkdir(sub, 0o755)
	if _, _, err := takeSnapshot(context.Background(), live, sub, time.Now()); err == nil || !strings.Contains(err.Error(), "git work tree") {
		t.Errorf("a snapshot inside a git work tree: %v, want a refusal", err)
	}
	if ents, _ := os.ReadDir(sub); len(ents) != 0 {
		t.Errorf("a refused snapshot left %d files", len(ents))
	}
}

func mustRead(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestFileCommand is `lifelog file PATH` on a throwaway database: the original is hashed and never stored, its text
// read from --text, its picture made from it; the same file again keeps one page.
func TestFileCommand(t *testing.T) {
	dir := t.TempDir()
	live := filepath.Join(dir, "life.db")
	if err := db.Init(live); err != nil {
		t.Fatal(err)
	}
	m := image.NewGray(image.Rect(0, 0, 64, 48))
	var b bytes.Buffer
	if err := jpeg.Encode(&b, m, nil); err != nil {
		t.Fatal(err)
	}
	pic, text := filepath.Join(dir, "IMG_0001.jpg"), filepath.Join(dir, "caption.txt")
	os.WriteFile(pic, b.Bytes(), 0o644)
	os.WriteFile(text, []byte("Dawn at the lake with [[Sam]]."), 0o644)
	for range 2 {
		o, err := parse([]string{"file", pic, "--db", live, "--text", text, "--day", "2026-10-04"})
		if err != nil {
			t.Fatal(err)
		}
		if err := run(o); err != nil {
			t.Fatal(err)
		}
	}
	d, err := db.Open(live)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	s := &core.Store{DB: d}
	id, _ := s.PageID(context.Background(), "IMG_0001.jpg")
	p, err := s.PageByID(context.Background(), id)
	if err != nil || p.Type != "file" || p.Body != "Dawn at the lake with [[Sam]]." || p.Day != "2026-10-04" || p.File == nil || !p.File.Preview {
		t.Fatalf("the file page: %v %+v", err, p)
	}
	var n int
	d.R.QueryRow(`SELECT count(*) FROM files`).Scan(&n)
	if n != 1 {
		t.Errorf("%d files rows after keeping one file twice", n)
	}
}

// TestKeepTheFewPhotosOfADay is `lifelog file FOLDER` (docs/plans/033): a dry run writes nothing; the batch keeps
// the photos, links their days and shows them there; the photos near no place come in one group per place; naming one
// photo's place lets the next run link the rest of its group; a run again writes nothing.
func TestKeepTheFewPhotosOfADay(t *testing.T) {
	dir := t.TempDir()
	live := filepath.Join(dir, "life.db")
	if err := db.Init(live); err != nil {
		t.Fatal(err)
	}
	d, err := db.Open(live)
	if err != nil {
		t.Fatal(err)
	}
	s := &core.Store{DB: d}
	lake, err := s.CreatePlace(context.Background(), "cli", "Lakeside")
	if err == nil {
		err = s.Locate(context.Background(), "cli", lake, core.Point{Lat: 46.1, Lon: 7.2, RadiusM: 300, LinkDays: true})
	}
	if err != nil {
		t.Fatal(err)
	}
	d.Close()
	picks := filepath.Join(dir, "picks")
	os.MkdirAll(picks, 0o755)
	put := func(name string, b []byte) { os.WriteFile(filepath.Join(picks, name), b, 0o644) }
	gps := func(day string, lat, lon float64) photo.Meta {
		return photo.Meta{Taken: day + " 09:00:00", Lat: lat, Lon: lon, HasGPS: true}
	}
	put("lake1.jpg", phototest.JPEG(40, 30, gps("2019-06-03", 46.1001, 7.2001), true))
	put("lake2.jpg", phototest.JPEG(41, 30, gps("2019-06-03", 46.0999, 7.1999), false))
	put("lake3.HEIC", phototest.HEIC(gps("2019-06-03", 46.1002, 7.2002), false, false))
	put("porto1.jpg", phototest.JPEG(42, 30, gps("2019-06-04", 41.1496, -8.6110), true))
	put("porto2.jpg", phototest.JPEG(43, 30, gps("2019-06-05", 41.1510, -8.6100), true))
	put("undated.jpg", phototest.JPEG(44, 30, photo.Meta{}, true))
	put("notes.txt", []byte("not a photo"))
	keep := func(args ...string) {
		t.Helper()
		o, err := parse(append(append([]string{"file"}, args...), "--db", live))
		if err != nil {
			t.Fatal(err)
		}
		if err := run(o); err != nil {
			t.Fatal(err)
		}
	}
	count := func(q string, args ...any) int {
		t.Helper()
		d, err := db.Open(live)
		if err != nil {
			t.Fatal(err)
		}
		defer d.Close()
		var n int
		if err := d.R.QueryRow(q, args...).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	rows := func() int {
		return count(`SELECT (SELECT count(*) FROM entities) + (SELECT count(*) FROM links) + (SELECT count(*) FROM files) + (SELECT count(*) FROM places)`)
	}
	before := rows()
	keep(picks, "--dry-run")
	if rows() != before {
		t.Fatalf("a dry run wrote rows")
	}
	keep(picks, "--human")
	if n := count(`SELECT count(*) FROM files`); n != 6 {
		t.Errorf("%d files kept, want 6 (the text file is no photo)", n)
	}
	at := func(day string) int {
		return count(`SELECT count(*) FROM links l JOIN entities e ON e.id = l.from_id WHERE e.preferred_name_key = ? AND l.kind = 'at'`, day)
	}
	body := func(day string) int {
		return count(`SELECT (length(body) - length(replace(body, '![[', ''))) / 3 FROM entities WHERE preferred_name_key = ?`, day)
	}
	if at("2019-06-03") != 1 || body("2019-06-03") != 2 || at("2019-06-04") != 0 || at("2019-06-05") != 0 {
		t.Errorf("after the batch: at %d/%d/%d, embeds on the 3rd %d (the HEIC has no picture)", at("2019-06-03"), at("2019-06-04"), at("2019-06-05"), body("2019-06-03"))
	}
	keep(filepath.Join(picks, "porto1.jpg"), "--at", "Porto", "--radius", "2000")
	keep(picks)
	if at("2019-06-04") != 1 || at("2019-06-05") != 1 {
		t.Errorf("Porto named once: at %d/%d", at("2019-06-04"), at("2019-06-05"))
	}
	before = rows()
	keep(picks)
	if rows() != before {
		t.Errorf("a run again wrote rows")
	}
	if err := run(opts{args: []string{"file", picks}, title: "One title", db: live, source: "cli"}); err == nil {
		t.Error("--title was accepted for a batch")
	}
}

func TestABatchReportGroupsThePhotosNearNoPlace(t *testing.T) {
	u := func(lat, lon float64) map[string]any {
		return map[string]any{"lat": lat, "lon": lon, "map": core.MapLink(lat, lon)}
	}
	b, err := summarise([]keptFile{
		{Path: "a.jpg", Result: map[string]any{"day": "2019-06-04", "unmatched": u(41.1496, -8.6110)}},
		{Path: "b.jpg", Result: map[string]any{"day": "2019-06-05", "unmatched": u(41.1510, -8.6100)}},
		{Path: "c.jpg", Result: map[string]any{"day": "2019-06-05", "unmatched": u(38.7, -9.1)}},
		{Path: "d.jpg", Result: map[string]any{"day": "2019-06-03", "place": "Lakeside", "linked": true, "existing": true}},
		{Path: "e.jpg", Result: map[string]any{"day": "2019-06-03", "place": "Home"}},
		{Path: "f.jpg", Error: "422: refused"},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Unmatched) != 2 || b.Unmatched[0].Count != 2 || strings.Join(b.Unmatched[0].Days, ",") != "2019-06-04,2019-06-05" ||
		!strings.Contains(b.Unmatched[0].NameWith, `"a.jpg"`) {
		t.Errorf("groups: %+v", b.Unmatched)
	}
	if b.Kept != 4 || b.Already != 1 || b.Failed != 1 {
		t.Errorf("counts: %d kept, %d already, %d failed", b.Kept, b.Already, b.Failed)
	}
	if d := b.Days["2019-06-03"]; d.Photos != 2 || strings.Join(d.At, ",") != "Lakeside" || strings.Join(d.Known, ",") != "Home" {
		t.Errorf("a day: %+v", d)
	}
}

func TestDoDispatchesPageAndImportFind(t *testing.T) {
	c := commandClientWithWorkspace(t)
	if _, err := do(c, "find", map[string]string{"title": "Ana"}); err != nil {
		t.Fatalf("page find: %v", err)
	}
	matches, err := do(c, "import-find", map[string]string{"text": "Ana"})
	if err != nil {
		t.Fatalf("import find: %v", err)
	}
	if matches.Title != "Find Ana" || cmdHref(matches, "self") != "/import/find?text=Ana" {
		t.Fatalf("import-find returned %+v", matches)
	}
}

func commandClientWithWorkspace(t *testing.T) *client.Client {
	t.Helper()
	p := filepath.Join(t.TempDir(), "life.db")
	if err := db.Init(p); err != nil {
		t.Fatal(err)
	}
	d, err := db.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "Notebook"), 0o755); err != nil {
		t.Fatal(err)
	}
	ws, err := importer.Open(filepath.Join(root, "Notebook.lifelog"))
	if err != nil {
		t.Fatal(err)
	}
	c := client.InProcess(api.New(&core.Store{DB: d}, ws), "cli")
	if _, err := c.Do(cmdAction(t, mustEntity(c.Get("/")), "create-page"), map[string]string{"title": "Ana", "body": "Synthetic page"}); err != nil {
		t.Fatal(err)
	}
	return c
}

func cmdAction(t *testing.T, e *api.Entity, name string) api.Action {
	t.Helper()
	for _, a := range e.Actions {
		if a.Name == name {
			return a
		}
	}
	t.Fatalf("%s not found in %v", name, e.Actions)
	return api.Action{}
}

func mustEntity(e *api.Entity, err error) *api.Entity {
	if err != nil {
		panic(err)
	}
	return e
}

func cmdHref(e *api.Entity, rel string) string {
	for _, l := range e.Links {
		if len(l.Rel) > 0 && l.Rel[0] == rel {
			return l.Href
		}
	}
	return ""
}

func TestImportTakeoutInventoryExtractionRoot(t *testing.T) {
	root := t.TempDir()
	for rel, body := range map[string]string{
		"Takeout/Fit/Sessions/PRIVATE_MARKER.json":    `{"sessions":[{"startTime":"2020-01-10","PRIVATE_MARKER":"PRIVATE_MARKER"}]}`,
		"Takeout/Fitbit/exercise-PRIVATE_MARKER.json": `[{"startTime":"2020-01-10"},{"PRIVATE_MARKER":"PRIVATE_MARKER"}]`,
		"Timeline.json": `{"semanticSegments":[{"startTime":"2020-01-10","visit":{},"PRIVATE_MARKER":"PRIVATE_MARKER"}]}`,
	} {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	o, err := parse([]string{"import", "takeout", "inventory", root})
	if err != nil {
		t.Fatal(err)
	}
	out := captureStdout(t, func() {
		if err := run(o); err != nil {
			t.Fatal(err)
		}
	})
	for _, want := range []string{"Timeline On-Device", `"unsupported_files": 1`, `"metric": "exercise"`} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %s: %s", want, out)
		}
	}
	if strings.Contains(out, "PRIVATE_MARKER") || strings.Contains(out, root) {
		t.Fatal("private inventory detail leaked")
	}
	if _, err := os.Stat(filepath.Join(root, "life.db")); !os.IsNotExist(err) {
		t.Fatal("inventory created database")
	}
}

func TestSnapshotPhysicalDestination(t *testing.T) {
	for _, kind := range []string{"symlink", "junction"} {
		for _, marker := range []string{"directory", "file"} {
			for _, layout := range []string{"destination", "ancestor", "lexical", "safe"} {
				t.Run(kind+"/"+marker+"/"+layout, func(t *testing.T) {
					if kind == "junction" && runtime.GOOS != "windows" {
						t.Skip("Windows directory junctions require Windows")
					}
					root := t.TempDir()
					live := filepath.Join(root, "life.db")
					if err := db.Init(live); err != nil {
						t.Fatal(err)
					}
					before := mustRead(t, live)
					repo := filepath.Join(root, "repo")
					target := filepath.Join(repo, "backups")
					if layout == "safe" || layout == "lexical" {
						target = filepath.Join(root, "safe #%'")
					}
					for _, dir := range []string{repo, target} {
						if err := os.MkdirAll(dir, 0o755); err != nil {
							t.Fatal(err)
						}
					}
					git := filepath.Join(repo, ".git")
					if marker == "directory" {
						if err := os.Mkdir(git, 0o755); err != nil {
							t.Fatal(err)
						}
					} else if err := os.WriteFile(git, []byte("gitdir: synthetic-marker\n"), 0o644); err != nil {
						t.Fatal(err)
					}
					link := filepath.Join(root, "linked")
					if layout == "lexical" {
						link = filepath.Join(repo, "linked")
					}
					if kind == "junction" {
						if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput(); err != nil {
							t.Skipf("cannot create test-owned junction: %v: %s", err, out)
						}
					} else if err := os.Symlink(target, link); err != nil {
						t.Skipf("cannot create test-owned directory symlink: %v", err)
					}
					if kind == "junction" {
						info, err := os.Lstat(link)
						if err != nil {
							t.Fatal(err)
						}
						got, err := os.Readlink(link)
						if err != nil || !strings.EqualFold(got, target) {
							t.Fatalf("junction Readlink: %q %v, want %q", got, err, target)
						}
						t.Logf("junction mode %s; Readlink resolves target", info.Mode())
					}
					dest := link
					physical := target
					if layout == "ancestor" {
						dest = filepath.Join(link, "child")
						physical = filepath.Join(target, "child")
						if err := os.Mkdir(physical, 0o755); err != nil {
							t.Fatal(err)
						}
					}
					now := time.Date(2026, 10, 2, 14, 30, 5, 0, time.Local)
					if layout == "safe" {
						p, res, err := takeSnapshot(context.Background(), live, dest, now)
						if err != nil || !res.OK {
							t.Fatalf("safe linked destination: %s %+v %v", p, res, err)
						}
						resolved, err := filepath.EvalSymlinks(physical)
						if err != nil {
							t.Fatal(err)
						}
						if filepath.Dir(p) != resolved {
							t.Errorf("copy path %s is not physical destination %s", p, resolved)
						}
					} else {
						o, err := parse([]string{"snapshot", "--db", live, "--to", dest})
						if err != nil {
							t.Fatal(err)
						}
						if err := run(o); err == nil || !strings.Contains(err.Error(), "git work tree") {
							t.Errorf("command refusal: %v", err)
						}
						for _, dir := range []string{dest, physical} {
							entries, err := os.ReadDir(dir)
							if err != nil {
								t.Fatal(err)
							}
							if len(entries) != 0 {
								t.Errorf("refused destination %s contains artifacts: %v", dir, entries)
							}
						}
					}
					if !bytes.Equal(before, mustRead(t, live)) {
						t.Error("snapshot changed source bytes")
					}
				})
			}
		}
	}
}

func TestCommandCancellation(t *testing.T) {
	for _, args := range [][]string{{"get", "blocked"}, {"capture", "synthetic"}, {"import", "status"}, {"actions"}} {
		t.Run(strings.Join(args, "-"), func(t *testing.T) {
			started, stopped, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				close(started)
				select {
				case <-r.Context().Done():
					close(stopped)
				case <-release:
				}
			}))
			defer srv.Close()
			// This independent escape must run before server shutdown even on Fatal.
			defer close(release)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- runContext(ctx, opts{args: args, url: srv.URL, source: "cli"}) }()
			select {
			case <-started:
			case <-time.After(10 * time.Second):
				t.Fatal("not started")
			}
			cancel()
			select {
			case <-stopped:
			case <-time.After(10 * time.Second):
				t.Fatal("handler not canceled")
			}
			select {
			case err := <-done:
				if !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
			case <-time.After(10 * time.Second):
				t.Fatal("command did not return after cancellation")
			}
		})
	}
}

func TestCommandCancellationFileBatch(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	c := client.InProcess(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/actions" {
			fmt.Fprint(w, `{"actions":[{"name":"add-file","method":"POST","href":"/files","fields":[{"name":"title"},{"name":"original","type":"file"}]}]}`)
			return
		}
		calls++
		cancel()
		fmt.Fprint(w, `{}`)
	}), "cli")
	var paths []string
	for _, name := range []string{"one.txt", "two.txt"} {
		p := filepath.Join(t.TempDir(), name)
		if err := os.WriteFile(p, []byte("synthetic"), 0600); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, p)
	}
	if err := keepFileContext(ctx, opts{}, c, paths); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("started %d files after cancellation", calls)
	}
}

// TestImportSetupIntegrity uses only disposable synthetic source files and workspaces.
func TestImportSetupIntegrity(t *testing.T) {
	if os.Getenv("LIFELOG_SETUP_TEST_CHILD") == "1" {
		os.Args = append([]string{"lifelog"}, strings.Split(os.Getenv("LIFELOG_SETUP_TEST_ARGS"), "|")...)
		main()
		return
	}
	for _, kind := range []string{"clean", "orphan", "execution-error", "output-error"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			from := filepath.Join(root, "synthetic.db")
			if err := db.Init(from); err != nil {
				t.Fatal(err)
			}
			d, err := db.Open(from)
			if err != nil {
				t.Fatal(err)
			}
			if kind == "orphan" || kind == "output-error" {
				_, err = d.W.Exec(`PRAGMA foreign_keys=OFF; INSERT INTO entities(id, entity_type, preferred_name_key, created_at, updated_at, source) VALUES(999, 'page', 'missing-name', '2026-10-05T00:00:00.000Z', '2026-10-05T00:00:00.000Z', 'cli')`)
			} else if kind == "execution-error" {
				// A missing domain table makes the orphan query fail, rather than return OK=false.
				_, err = d.W.Exec(`DROP TABLE people`)
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := d.Close(); err != nil {
				t.Fatal(err)
			}
			source := filepath.Join(root, "Synthetic")
			if err := os.Mkdir(source, 0700); err != nil {
				t.Fatal(err)
			}
			ws, err := importer.Open(source + ".lifelog")
			if err != nil {
				t.Fatal(err)
			}
			var setupErr error
			var out string
			if kind == "output-error" {
				f, err := os.CreateTemp(root, "closed-output")
				if err != nil {
					t.Fatal(err)
				}
				f.Close()
				old := os.Stdout
				os.Stdout = f
				defer func() { os.Stdout = old }()
				setupErr = importOwner(opts{from: from}, ws, []string{"setup"})
				os.Stdout = old
				if !errors.Is(setupErr, os.ErrClosed) {
					t.Fatalf("output error = %v, want closed-file error", setupErr)
				}
			} else {
				out = captureStdout(t, func() { setupErr = importOwner(opts{from: from}, ws, []string{"setup"}) })
				switch kind {
				case "clean":
					if setupErr != nil || !strings.Contains(out, "integrity: ok") {
						t.Fatalf("clean setup: %v; %s", setupErr, out)
					}
				case "orphan":
					if setupErr == nil || !strings.Contains(setupErr.Error(), "failed trial integrity") || !strings.Contains(out, `"ok": false`) || !strings.Contains(out, "999") {
						t.Errorf("failed setup: %v; %s", setupErr, out)
					}
				case "execution-error":
					if setupErr == nil || !strings.Contains(setupErr.Error(), "people") || strings.Contains(out, "integrity: ok") {
						t.Fatalf("execution error: %v; %s", setupErr, out)
					}
				}
			}
			trial, err := db.Open(ws.TrialDB())
			if err != nil {
				t.Fatalf("diagnostic trial not retained: %v", err)
			}
			var entities, pages int
			if err := trial.R.QueryRow(`SELECT count(*) FROM entities`).Scan(&entities); err != nil {
				t.Fatal(err)
			}
			if err := trial.R.QueryRow(`SELECT count(*) FROM entity_names`).Scan(&pages); err != nil {
				t.Fatal(err)
			}
			trial.Close()
			want := 1
			if kind == "orphan" || kind == "output-error" {
				want = 2
			}
			if entities != want || pages != 1 {
				t.Fatalf("setup repaired or imported rows: entities=%d pages=%d", entities, pages)
			}
			entries, err := os.ReadDir(ws.Dir)
			if err != nil {
				t.Fatal(err)
			}
			for _, e := range entries {
				if e.Name() != "facts" && e.Name() != "trial.db" {
					t.Errorf("unexpected setup side effect: %s", e.Name())
				}
			}
			if kind == "orphan" {
				childSource := filepath.Join(root, "Child")
				if err := os.Mkdir(childSource, 0700); err != nil {
					t.Fatal(err)
				}
				cmd := exec.Command(os.Args[0], "-test.run=^TestImportSetupIntegrity$")
				cmd.Env = append(os.Environ(), "LIFELOG_SETUP_TEST_CHILD=1", "LIFELOG_SETUP_TEST_ARGS="+strings.Join([]string{"import", "setup", "--from", from, "--workspace", childSource + ".lifelog"}, "|"))
				output, err := cmd.CombinedOutput()
				var exit *exec.ExitError
				if !errors.As(err, &exit) || exit.ExitCode() != 1 || !strings.Contains(string(output), `"ok": false`) || !strings.Contains(string(output), "failed trial integrity") {
					t.Errorf("entrypoint: %v; %s", err, output)
				}
				if _, err := os.Stat(filepath.Join(childSource+".lifelog", "trial.db")); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestServeAuthorityAddress(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"127.0.0.1:7777", "127.0.0.1:7777"}, {"127.2.3.4:0", "127.2.3.4:0"}, {"localhost:0", "127.0.0.1:0"}, {"LOCALHOST:7777", "127.0.0.1:7777"}, {"[::1]:0", "[::1]:0"},
	} {
		for _, argv := range [][]string{{"serve", "--addr", tc.input}, {"--addr=" + tc.input, "serve"}} {
			o, err := parse(argv)
			if err != nil {
				t.Fatal(err)
			}
			got, err := api.LoopbackAddress(o.addr)
			if err != nil || got != tc.want {
				t.Fatalf("%q got=%q err=%v", argv, got, err)
			}
		}
	}
	for _, addr := range []string{"", ":7777", "0.0.0.0:7777", "[::]:7777", "192.0.2.1:7777", "custom.invalid:7777", "localhoſt:7777", "localhost:http", "localhost:65536", "localhost:-1", "localhost:+1", "localhost.", "[::1%zone]:7777", "[localhost]:7777", "127.0.0.1", "localhost:"} {
		// Address refusal precedes database opening, even when the file is absent.
		err := runContext(context.Background(), opts{args: []string{"serve"}, addr: addr, db: filepath.Join(t.TempDir(), "absent.db")})
		if err == nil || !strings.Contains(err.Error(), "serve address") {
			t.Fatalf("addr=%q error=%v", addr, err)
		}
	}
}

func TestServeAuthorityMount(t *testing.T) {
	// Run the real serve branch against a disposable synthetic database. Capture
	// the printed ephemeral listener address rather than guessing a free port.
	p := filepath.Join(t.TempDir(), "life.db")
	if err := db.Init(p); err != nil {
		t.Fatal(err)
	}
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stderr
	os.Stderr = write
	defer func() { os.Stderr = old; read.Close(); write.Close() }()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	var serveErr error
	go func() {
		defer close(done)
		serveErr = runContext(ctx, opts{args: []string{"serve"}, addr: "localhost:0", db: p, source: "cli"})
	}()
	defer func() {
		cancel()
		select {
		case <-done:
			if serveErr != nil {
				t.Error(serveErr)
			}
		case <-time.After(10 * time.Second):
			t.Error("serve shutdown timed out")
		}
	}()
	line := make(chan string, 1)
	go func() { s, _ := bufio.NewReader(read).ReadString('\n'); line <- s }()
	var output string
	select {
	case output = <-line:
	case <-done:
		t.Fatalf("serve stopped before startup: %v", serveErr)
	case <-time.After(10 * time.Second):
		t.Fatal("serve startup timed out")
	}
	_, base, ok := strings.Cut(output, " on ")
	if !ok {
		t.Fatalf("startup: %q", output)
	}
	base = strings.TrimSpace(base)
	if strings.HasSuffix(base, ":0") {
		t.Fatalf("ephemeral port not reported: %s", base)
	}
	tr := &http.Transport{Proxy: nil}
	defer tr.CloseIdleConnections()
	hc := &http.Client{Transport: tr, Timeout: 10 * time.Second}
	for _, tc := range []struct {
		host string
		want int
	}{{"", 200}, {"arbitrary.invalid:7777", 403}} {
		req, _ := http.NewRequest("GET", base+"/actions", nil)
		if tc.host != "" {
			req.Host = tc.host
		}
		res, err := hc.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != tc.want {
			t.Fatalf("Host=%q status=%d", tc.host, res.StatusCode)
		}
	}
}
