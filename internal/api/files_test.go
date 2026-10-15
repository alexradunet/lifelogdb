package api_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"image"
	"image/color"
	"image/jpeg"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"lifelog/internal/api"
	"lifelog/internal/photo"
	"lifelog/internal/photo/phototest"
)

// jpegFile writes a synthetic w×h JPEG and returns its path and the SHA-256 of its bytes.
func jpegFile(t *testing.T, name string, w, h int) (string, string) {
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
	add := find(must(c.Get(t.Context(), "/")), "add-file")
	path, sum := jpegFile(t, "IMG_0001.JPG", 2400, 1800)
	e, err := c.DoFiles(t.Context(), add, map[string]string{"title": "2026-10-04 Lake.jpg", "body": "The lake with [[Sam]]."}, map[string]string{"original": path})
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
	again, err := c.DoFiles(t.Context(), add, map[string]string{"title": "Another name.jpg"}, map[string]string{"original": path})
	if res, _ := again.Result.(map[string]any); err != nil || res["existing"] != true || href(again, "self") != href(e, "self") {
		t.Errorf("the same original again: %v %+v", err, again.Result)
	}
	if got := names(e); got != "save-body,rename,link,unlink,file-pages,tombstone" {
		t.Errorf("a file page offers %s (never promote)", got)
	}
	if list := must(c.Get(t.Context(), "/browse?kind=media")); len(list.Entities) != 1 || list.Entities[0].Title != "2026-10-04 Lake.jpg" {
		t.Errorf("/browse?kind=media: %+v", list.Entities)
	}
}

func TestAddFileFromAnAgentAndWithAPicture(t *testing.T) {
	c, _ := fresh(t)
	add := find(must(c.Get(t.Context(), "/")), "add-file")
	e, err := c.Do(t.Context(), add, map[string]string{"title": "Memo.m4a", "sha256": strings.Repeat("ab", 32), "mime": "audio/mp4", "body": "the transcript"})
	if err != nil || props(e)["body"] != "the transcript" || href(e, "preview") != "" {
		t.Fatalf("an agent's keep: %v %+v", err, props(e))
	}
	if _, err := c.Do(t.Context(), add, map[string]string{"title": "Memo 2.m4a", "body": "x"}); err == nil {
		t.Error("a keep with neither the original nor its hash")
	}
	// a format lifelog cannot read keeps no picture until one is sent with the same original
	heic := filepath.Join(t.TempDir(), "IMG_0002.HEIC")
	os.WriteFile(heic, []byte("not really a HEIC, but its bytes are hashed"), 0o644)
	e = must(c.DoFiles(t.Context(), add, map[string]string{"title": "IMG_0002.HEIC"}, map[string]string{"original": heic}))
	if file, _ := props(e)["file"].(map[string]any); file["mime"] != "image/heic" || file["preview"] != false {
		t.Fatalf("a HEIC: %+v", props(e))
	}
	frame, _ := jpegFile(t, "frame.jpg", 300, 200)
	e = must(c.DoFiles(t.Context(), add, map[string]string{"title": "IMG_0002.HEIC"}, map[string]string{"original": heic, "preview": frame}))
	if res, _ := e.Result.(map[string]any); res["preview_added"] != true || href(e, "preview") == "" {
		t.Errorf("a picture sent later: %+v", e.Result)
	}
	if _, err := c.DoFiles(t.Context(), add, map[string]string{"title": "x", "sha256": strings.Repeat("0", 64)}, map[string]string{"original": heic}); err == nil {
		t.Error("a sha256 that is not the original's was accepted")
	}
}

func TestAddFileKeepsMalformedHEIFWithUnknownMetadata(t *testing.T) {
	c, _ := fresh(t)
	add := find(must(c.Get(t.Context(), "/")), "add-file")
	heic := filepath.Join(t.TempDir(), "bad.HEIC")
	if err := os.WriteFile(heic, phototest.HEICOverflowingLocation(), 0o644); err != nil {
		t.Fatal(err)
	}
	e := must(c.DoFiles(t.Context(), add, map[string]string{"title": "bad.HEIC", "body": "kept despite bad metadata"}, map[string]string{"original": heic}))
	file, _ := props(e)["file"].(map[string]any)
	res, _ := e.Result.(map[string]any)
	if file["mime"] != "image/heic" || file["preview"] != false || props(e)["body"] != "kept despite bad metadata" {
		t.Fatalf("malformed HEIF was not kept as an unknown file: props=%+v result=%+v", props(e), res)
	}
	if res["day"] != nil || res["place"] != nil || res["unmatched"] != nil || res["linked"] == true {
		t.Errorf("bad metadata derived photo facts: %+v", res)
	}
}

func TestFileViews(t *testing.T) {
	c, h := fresh(t)
	root := must(c.Get(t.Context(), "/"))
	path, _ := jpegFile(t, "lake.jpg", 40, 30)
	f := must(c.DoFiles(t.Context(), find(root, "add-file"), map[string]string{"title": "Lake.jpg", "body": "dawn"}, map[string]string{"original": path}))
	day := must(c.Do(t.Context(), find(root, "capture"), map[string]string{"text": "Swam. ![[Lake.jpg|the lake]]"}))
	for path, want := range map[string][]string{
		"/browse?kind=document": {`enctype="multipart/form-data"`, `type="file" name="original"`, `type="hidden" name="sha256"`},
		"/browse?kind=media":    {"Lake.jpg", `action="/files"`, `<img src="` + href(f, "preview") + `" alt="" loading="lazy">`},
		href(f, "self"):         {`<img class="preview" src="` + href(f, "preview") + `"`, "image/jpeg", "Backlinks"},
		href(day, "page"):       {`<img src="/previews?title=Lake.jpg" alt="the lake"`},
	} {
		body := browse(t, h, path)
		for _, w := range want {
			if !strings.Contains(body, w) {
				t.Errorf("%s does not show %s", path, w)
			}
		}
	}
}

// TestAPhotosDay is a photo's own day, read from the original: the photo is filed under it and shown on it. Its
// position is never read (D21).
func TestAPhotosDay(t *testing.T) {
	c, h := fresh(t)
	root := must(c.Get(t.Context(), "/"))
	write := func(name string, b []byte) string {
		p := filepath.Join(t.TempDir(), name)
		if err := os.WriteFile(p, b, 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	add := find(root, "add-file")
	jpg := write("IMG_1.jpg", phototest.JPEG(64, 48, photo.Meta{Taken: "2019-06-03 07:41:12"}, true))
	e := must(c.DoFiles(t.Context(), add, map[string]string{"title": "2019-06-03 IMG_1.jpg"}, map[string]string{"original": jpg}))
	res, _ := e.Result.(map[string]any)
	if res["embedded"] != true || res["day"] != "2019-06-03" || props(e)["day"] != "2019-06-03" || res["place"] != nil || res["unmatched"] != nil {
		t.Fatalf("a photo with a day: %+v", res)
	}
	day := must(c.Get(t.Context(), "/days/2019-06-03"))
	if body := browse(t, h, href(day, "page")); !strings.Contains(body, `<img src="/previews?title=2019-06-03+IMG_1.jpg"`) {
		t.Errorf("the day page does not show the photo")
	}
	// a HEIC: no picture, no embed, its day still read
	heic := write("IMG_2.HEIC", phototest.HEIC(photo.Meta{Taken: "2019-06-04 10:00:00"}, false, false))
	e = must(c.DoFiles(t.Context(), add, map[string]string{"title": "2019-06-04 IMG_2.HEIC"}, map[string]string{"original": heic}))
	res, _ = e.Result.(map[string]any)
	if res["day"] != "2019-06-04" || res["embedded"] == true {
		t.Fatalf("a HEIC: %+v", res)
	}
}

func TestPromotedFileDay(t *testing.T) {
	c, _ := fresh(t)
	root := must(c.Get(t.Context(), "/"))
	capture := find(must(c.Get(t.Context(), "/actions")), "capture")
	if _, err := c.Do(t.Context(), capture, map[string]string{"day": "2026-10-04", "text": "Dawn: ![[Lake.jpg]]"}); err != nil {
		t.Fatal(err)
	}
	path, _ := jpegFile(t, "lake.jpg", 40, 30)
	e := must(c.DoFiles(t.Context(), find(root, "add-file"), map[string]string{"title": "lake.jpg", "day": "2026-10-04"}, map[string]string{"original": path}))
	res, _ := e.Result.(map[string]any)
	if res["promoted"] != true || props(e)["entity_type"] != "file" || props(e)["title"] != "Lake.jpg" || props(e)["day"] != "2026-10-04" {
		t.Fatalf("promoted file: result %+v props %+v", res, props(e))
	}
	day := must(c.Get(t.Context(), "/days/2026-10-04"))
	listed := false
	for _, row := range props(day)["view"].([]any) {
		r, _ := row.(map[string]any)
		if what, _ := r["what"].(string); strings.HasPrefix(what, "page") && r["detail"] == "Lake.jpg" {
			listed = true
		}
	}
	if !listed {
		t.Errorf("the day view does not list the promoted file: %+v", props(day))
	}
}

func TestAddFileDryRunWritesNothing(t *testing.T) {
	c, _ := fresh(t)
	root := must(c.Get(t.Context(), "/"))
	add := find(root, "add-file")
	path, _ := jpegFile(t, "x.jpg", 40, 30)
	e := must(c.DoFiles(t.Context(), add, map[string]string{"title": "x.jpg", "dry_run": "1"}, map[string]string{"original": path}))
	if res, _ := e.Result.(map[string]any); !strings.Contains(strings.Join(e.Class, ","), "dry-run") || res == nil || res["id"] != nil {
		t.Errorf("a dry run: %+v %+v", e.Class, e.Result)
	}
	if list := must(c.Get(t.Context(), "/browse?kind=media")); len(list.Entities) != 0 {
		t.Error("a dry run kept a file")
	}
	capture := find(must(c.Get(t.Context(), "/actions")), "capture")
	if _, err := c.Do(t.Context(), capture, map[string]string{"day": "2026-10-04", "text": "Dawn: ![[Dry.jpg]]"}); err != nil {
		t.Fatal(err)
	}
	path, _ = jpegFile(t, "dry.jpg", 40, 30)
	e = must(c.DoFiles(t.Context(), add, map[string]string{"title": "dry.jpg", "day": "2026-10-04", "dry_run": "1"}, map[string]string{"original": path}))
	if res, _ := e.Result.(map[string]any); res["promoted"] != true || res["id"] != nil {
		t.Errorf("a dry-run promotion report: %+v", e.Result)
	}
	ghost := must(c.Get(t.Context(), "/pages?title=Dry.jpg"))
	if props(ghost)["entity_type"] != "page" || props(ghost)["day"] != nil || props(ghost)["file"] != nil {
		t.Errorf("a dry-run promotion wrote to the ghost: %+v", props(ghost))
	}
	if list := must(c.Get(t.Context(), "/browse?kind=media")); len(list.Entities) != 0 {
		t.Error("a dry-run promotion kept a file")
	}
}

func TestBrowserMutationFeedbackPhoto(t *testing.T) {
	_, h := fresh(t)
	for _, dry := range []bool{true, false} {
		var b bytes.Buffer
		mw := multipart.NewWriter(&b)
		mw.WriteField("title", "Dated synthetic.jpg")
		if dry {
			mw.WriteField("dry_run", "1")
		}
		part, err := mw.CreateFormFile("original", "synthetic.jpg")
		if err != nil {
			t.Fatal(err)
		}
		part.Write(phototest.JPEG(64, 48, photo.Meta{Taken: "2019-06-04 10:00:00"}, true))
		mw.Close()
		req := httptest.NewRequest("POST", "/files", &b)
		req.Header.Set("Content-Type", mw.FormDataContentType())
		req.Header.Set("Accept", "text/html")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		body := rec.Body.String()
		if dry {
			if rec.Code != 200 || rec.Header().Get("Location") != "" || !strings.Contains(body, "nothing written") {
				t.Fatalf("dry run: %d %s", rec.Code, body)
			}
		} else {
			if rec.Code != 303 {
				t.Fatalf("photo POST: %d %s", rec.Code, body)
			}
			location := rec.Header().Get("Location")
			if strings.Contains(location, "41.1496") || strings.Contains(location, "synthetic") {
				t.Fatal("private redirect")
			}
			body = browse(t, h, location)
		}
		for _, want := range []string{"Result", "Filed under"} { // the toast says the result in words
			if !strings.Contains(body, want) {
				t.Errorf("photo feedback missing %q", want)
			}
		}
	}
}

func TestAutomaticEmbedAPIRefusal(t *testing.T) {
	c, _ := fresh(t)
	add := find(must(c.Get(t.Context(), "/")), "add-file")
	path, _ := jpegFile(t, "Lake [1].jpg", 40, 30)
	for _, dry := range []string{"1", "0"} {
		_, err := c.DoFiles(t.Context(), add, map[string]string{"title": "Lake [1].jpg", "day": "2026-10-04", "dry_run": dry}, map[string]string{"original": path})
		if !clientStatus(err, 422) {
			t.Fatalf("dry=%s: %v", dry, err)
		}
	}
}
