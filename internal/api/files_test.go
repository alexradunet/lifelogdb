package api_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"lifelog/internal/api"
)

// photo writes a synthetic w×h JPEG and returns its path and the SHA-256 of its bytes.
func photo(t *testing.T, name string, w, h int) (string, string) {
	t.Helper()
	m := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			m.SetRGBA(x, y, color.RGBA{uint8(x), uint8(y), 128, 255})
		}
	}
	var b bytes.Buffer
	if err := jpeg.Encode(&b, m, nil); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, b.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(b.Bytes())
	return p, hex.EncodeToString(sum[:])
}

func props(e *api.Entity) map[string]any { p, _ := e.Properties.(map[string]any); return p }

func TestAddFileKeepsAPhotoAndItsPicture(t *testing.T) {
	c, h := fresh(t)
	add := find(must(c.Get("/")), "add-file")
	path, sum := photo(t, "IMG_0001.JPG", 2400, 1800)
	e, err := c.DoFiles(add, map[string]string{"title": "2026-10-04 Lake.jpg", "body": "The lake with [[Sam]]."}, map[string]string{"original": path})
	if err != nil {
		t.Fatal(err)
	}
	file, _ := props(e)["file"].(map[string]any)
	if file["sha256"] != sum || file["mime"] != "image/jpeg" || file["preview"] != true || props(e)["entity_type"] != "file" {
		t.Fatalf("the file page: %+v", props(e))
	}
	pic := href(e, "preview")
	for _, path := range []string{pic, "/previews?title=2026-10-04+LAKE.jpg"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		m, err := jpeg.Decode(rec.Body)
		if rec.Code != 200 || rec.Header().Get("Content-Type") != "image/jpeg" || err != nil || m.Bounds().Dx() != 1600 || m.Bounds().Dy() != 1200 {
			t.Errorf("GET %s: %d %s %v", path, rec.Code, rec.Header().Get("Content-Type"), err)
		}
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/previews?title=Nothing", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("a title with no picture: %d", rec.Code)
	}
	again, err := c.DoFiles(add, map[string]string{"title": "Another name.jpg"}, map[string]string{"original": path})
	if res, _ := again.Result.(map[string]any); err != nil || res["existing"] != true || href(again, "self") != href(e, "self") {
		t.Errorf("the same original again: %v %+v", err, again.Result)
	}
	if got := names(e); got != "save-body,link,unlink,tombstone" {
		t.Errorf("a file page offers %s (never promote or rename)", got)
	}
	if list := must(c.Get("/files")); len(list.Entities) != 1 || list.Entities[0].Title != "2026-10-04 Lake.jpg" {
		t.Errorf("/files: %+v", list.Entities)
	}
}

func TestAddFileFromAnAgentAndWithAPicture(t *testing.T) {
	c, _ := fresh(t)
	add := find(must(c.Get("/")), "add-file")
	e, err := c.Do(add, map[string]string{"title": "Memo.m4a", "sha256": strings.Repeat("ab", 32), "mime": "audio/mp4", "body": "the transcript"})
	if err != nil || props(e)["body"] != "the transcript" || href(e, "preview") != "" {
		t.Fatalf("an agent's keep: %v %+v", err, props(e))
	}
	if _, err := c.Do(add, map[string]string{"title": "Memo 2.m4a", "body": "x"}); err == nil {
		t.Error("a keep with neither the original nor its hash")
	}
	// a format lifelog cannot read keeps no picture until one is sent with the same original
	heic := filepath.Join(t.TempDir(), "IMG_0002.HEIC")
	os.WriteFile(heic, []byte("not really a HEIC, but its bytes are hashed"), 0o644)
	e = must(c.DoFiles(add, map[string]string{"title": "IMG_0002.HEIC"}, map[string]string{"original": heic}))
	if file, _ := props(e)["file"].(map[string]any); file["mime"] != "image/heic" || file["preview"] != false {
		t.Fatalf("a HEIC: %+v", props(e))
	}
	frame, _ := photo(t, "frame.jpg", 300, 200)
	e = must(c.DoFiles(add, map[string]string{"title": "IMG_0002.HEIC"}, map[string]string{"original": heic, "preview": frame}))
	if res, _ := e.Result.(map[string]any); res["preview_added"] != true || href(e, "preview") == "" {
		t.Errorf("a picture sent later: %+v", e.Result)
	}
	if _, err := c.DoFiles(add, map[string]string{"title": "x", "sha256": strings.Repeat("0", 64)}, map[string]string{"original": heic}); err == nil {
		t.Error("a sha256 that is not the original's was accepted")
	}
}

func TestFileViews(t *testing.T) {
	c, h := fresh(t)
	root := must(c.Get("/"))
	path, _ := photo(t, "lake.jpg", 40, 30)
	f := must(c.DoFiles(find(root, "add-file"), map[string]string{"title": "Lake.jpg", "body": "dawn"}, map[string]string{"original": path}))
	day := must(c.Do(find(root, "capture"), map[string]string{"text": "Swam. ![[Lake.jpg|the lake]]"}))
	for path, want := range map[string][]string{
		"/":               {`enctype="multipart/form-data"`, `type="file" name="original"`, `type="hidden" name="sha256"`},
		"/files":          {"Lake.jpg", `action="/files"`},
		href(f, "self"):   {`<img class="preview" src="` + href(f, "preview") + `"`, "image/jpeg", "Backlinks"},
		href(day, "page"): {`<img src="/previews?title=Lake.jpg" alt="the lake"`},
	} {
		body := browse(t, h, path)
		for _, w := range want {
			if !strings.Contains(body, w) {
				t.Errorf("%s does not show %s", path, w)
			}
		}
	}
}
