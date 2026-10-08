package inventory

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// an ingest folder: a notes folder (no .obsidian), a Takeout-like extraction with a few stray notes, a camera dump
// and two archives. Every content holds the marker that must never appear in the report.
const secret = "SECRET_CONTENT"

func ingest(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"Notes/Journal/2031-04-11.md":                                   secret + " swam",
		"Notes/Journal/2031-04-12.md":                                   secret,
		"Notes/Journal/2031_04_13.md":                                   secret, // day-like, not a daily note
		"Notes/People/Bob Sample.md":                                    secret,
		"Notes/photo.png":                                               secret,
		"Notes/scan 2031.pdf":                                           secret,
		"Takeout/Fit/Daily/2031-04-11.csv":                              "Date,Steps\n2031-04-11," + secret,
		"Takeout/Fit/Daily/2031-04-12.csv":                              secret,
		"Takeout/Drive/repo/README.md":                                  secret,
		"Takeout/Drive/repo/docs/x.md":                                  secret,
		"Takeout/Google Photos/IMG_0001.jpg":                            secret,
		"Takeout/Google Photos/IMG_0002.jpg":                            secret,
		"Takeout/Google Photos/IMG_0002.jpg.supplemental-metadata.json": secret,
		"Camera/DSC00001.JPG":                                           secret,
		"Camera/DSC00002.JPG":                                           secret,
		"Camera/DSC00001.MP4":                                           secret,
		".hidden/x.md":                                                  secret,
		"Notes/.obsidian/app.json":                                      secret,
	}
	for p, body := range files {
		full := filepath.Join(root, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// a zip of a code repository and a tar.gz of exported notes
	zf, err := os.Create(filepath.Join(root, "old-repo.zip"))
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(zf)
	for _, name := range []string{"repo-main/", "repo-main/src/main.go", "repo-main/src/util.go", "repo-main/docs/2031-01-01.md", "loose.txt"} {
		if strings.HasSuffix(name, "/") {
			if _, err := zw.Create(name); err != nil {
				t.Fatal(err)
			}
			continue
		}
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(secret))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	zf.Close()
	tf, err := os.Create(filepath.Join(root, "Camera", "export.tar.gz"))
	if err != nil {
		t.Fatal(err)
	}
	gw := gzip.NewWriter(tf)
	tw := tar.NewWriter(gw)
	for _, name := range []string{"export/a 2031.md", "export/b.md"} {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(secret))}); err != nil {
			t.Fatal(err)
		}
		tw.Write([]byte(secret))
	}
	tw.Close()
	gw.Close()
	tf.Close()
	if err := os.WriteFile(filepath.Join(root, "broken.zip"), []byte("not a zip "+secret), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestRunSurveysWithoutContents(t *testing.T) {
	root := ingest(t)
	r, err := Run(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	out := string(b)
	if strings.Contains(out, secret) {
		t.Fatalf("the report holds a content:\n%s", out)
	}
	for _, name := range []string{"2031-04-11", "Bob Sample", "IMG_0001", "DSC00001", "main.go", "app.json", "loose.txt", "README"} {
		if strings.Contains(out, name) {
			t.Errorf("the report names a file (%s):\n%s", name, out)
		}
	}
	if r.Files != 19 || r.Hidden != 2 {
		t.Errorf("files %d hidden %d, want 19 and 2", r.Files, r.Hidden)
	}
	byPath := map[string]Folder{}
	for _, f := range r.Folders {
		byPath[f.Path] = f
	}
	notes := byPath["Notes/Journal"]
	if notes.Files != 3 || notes.FilesBelow != 3 || len(notes.Extensions) != 1 || notes.Extensions[0] != (Count{".md", 3}) {
		t.Errorf("Notes/Journal: %+v", notes)
	}
	if got := notes.Patterns; len(got) != 2 || got[0] != (Count{"N-N-N.md", 2}) || got[1] != (Count{"<one of a kind>", 1}) {
		t.Errorf("patterns: %+v", got)
	}
	if byPath["Notes"].FilesBelow != 6 || byPath["Notes"].Files != 2 || byPath["."].FilesBelow != 19 {
		t.Errorf("subtree counts: %+v %+v", byPath["Notes"], byPath["."])
	}
	if byPath["Camera"].Extensions[0] != (Count{".jpg", 2}) {
		t.Errorf("extensions are lower-cased: %+v", byPath["Camera"].Extensions)
	}

	kinds := map[string]Source{}
	for _, s := range r.Sources {
		kinds[s.Path] = s
	}
	if n := kinds["Notes"]; n.Kind != "notes" || n.Markdown != 4 || n.DailyNotes != 2 || n.Files != 6 {
		t.Errorf("the notes folder: %+v", n)
	}
	if tk := kinds["Takeout"]; tk.Kind != "takeout" || tk.Files != 7 || len(tk.Products) != 3 {
		t.Errorf("the takeout: %+v", tk)
	} else if tk.Products[0] != (Count{"Drive", 2}) || tk.Products[1] != (Count{"Fit", 2}) || tk.Products[2] != (Count{"Google Photos", 3}) {
		t.Errorf("products: %+v", tk.Products)
	}
	if _, ok := kinds["Takeout/Drive/repo"]; ok || len(r.Sources) != 2 {
		t.Errorf("stray notes inside a recognised source, or a camera dump, became a source: %+v", r.Sources)
	}

	archives := map[string]Archive{}
	for _, a := range r.Archives {
		archives[a.Path] = a
	}
	if a := archives["old-repo.zip"]; a.Entries != 4 || a.Unread || len(a.Folders) != 2 ||
		a.Folders[0] != (Count{"repo-main/src", 2}) || a.Folders[1] != (Count{"<one of a kind>", 2}) {
		t.Errorf("zip listing: %+v", a)
	}
	if a := archives["Camera/export.tar.gz"]; a.Entries != 2 || a.Unread || len(a.Folders) != 1 || a.Folders[0] != (Count{"<one of a kind>", 2}) {
		t.Errorf("tar listing: %+v", a)
	}
	if a := archives["broken.zip"]; !a.Unread || a.Entries != 0 {
		t.Errorf("a broken archive is reported unreadable, not named in an error: %+v", a)
	}
}

func TestRunRefusals(t *testing.T) {
	root := ingest(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Run(ctx, root); !errors.Is(err, context.Canceled) {
		t.Errorf("a cancelled survey: %v", err)
	}
	missing := filepath.Join(root, "absent")
	if _, err := Run(context.Background(), missing); err == nil || strings.Contains(err.Error(), "absent") {
		t.Errorf("a missing folder is refused without its name: %v", err)
	}
	if _, err := Run(context.Background(), filepath.Join(root, "broken.zip")); err == nil {
		t.Error("a file is not a folder")
	}
}

func TestMaskAndGlobs(t *testing.T) {
	for in, want := range map[string]string{"IMG_20240102_123456.jpg": "IMG_N_N.jpg", "2031-04-11.md": "N-N-N.md", "notes.md": "notes.md", "PXL_1.MP.jpg": "PXL_N.MP.jpg"} {
		if got := mask(in); got != want {
			t.Errorf("mask(%q) = %q, want %q", in, got, want)
		}
	}
}
