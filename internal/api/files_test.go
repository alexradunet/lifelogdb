package api_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"image/jpeg"
	"log"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"lifelog/internal/api/apitest"
	"lifelog/internal/api/siren"
	"lifelog/internal/photo"
	"lifelog/internal/photo/phototest"
)

// jpegFile writes a synthetic w×h JPEG and returns its path and the SHA-256 of its bytes.
func jpegFile(t *testing.T, name string, w, h int) (string, string) {
	t.Helper()
	b := phototest.JPEG(w, h)
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(b)
	return p, hex.EncodeToString(sum[:])
}

func props(e *siren.Entity) map[string]any { p, _ := e.Properties.(map[string]any); return p }

// TestAPictureThatCannotBeSentIsLogged: when the client of a picture went away, the write of its bytes fails. The
// server's log has it one time, under the route of the picture; nothing else can be done.
func TestAPictureThatCannotBeSentIsLogged(t *testing.T) {
	c, h, _ := apitest.Fresh(t, "cli")
	path, _ := jpegFile(t, "IMG_0001.JPG", 64, 48)
	e, err := c.Do(t.Context(), apitest.Action(t, apitest.Must(t)(c.Get(t.Context(), "/")), "add-file"), map[string]string{"title": "Lake.jpg", "original": path})
	if err != nil {
		t.Fatal(err)
	}
	var logged bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&logged) // no test of this package runs in parallel
	t.Cleanup(func() { log.SetOutput(prev) })
	w := &goneClient{header: http.Header{}}
	h.ServeHTTP(w, httptest.NewRequest("GET", apitest.Href(e, "preview"), nil))
	if got := w.header.Get("Content-Type"); got != "image/jpeg" {
		t.Errorf("Content-Type %q, want the picture", got)
	}
	if n := strings.Count(logged.String(), "lifelog: GET /pages/{id}/preview: the picture was not sent: the client went away"); n != 1 {
		t.Errorf("the log has the failed write %d times, want 1: %q", n, logged.String())
	}
}

func TestAddFileKeepsAPhotoAndItsPicture(t *testing.T) {
	c, h, _ := apitest.Fresh(t, "cli")
	add := apitest.Action(t, apitest.Must(t)(c.Get(t.Context(), "/")), "add-file")
	path, sum := jpegFile(t, "IMG_0001.JPG", 2400, 1800)
	e, err := c.Do(t.Context(), add, map[string]string{"title": "2026-10-04 Lake.jpg", "body": "The lake with [[Sam]].", "original": path})
	if err != nil {
		t.Fatal(err)
	}
	file, _ := props(e)["file"].(map[string]any)
	if file["sha256"] != sum || file["mime"] != "image/jpeg" || file["preview"] != true || props(e)["entity_type"] != "file" {
		t.Fatalf("the file page: %+v", props(e))
	}
	pic := apitest.Href(e, "preview")
	for _, path := range []string{pic, "/previews?title=2026-10-04+LAKE.jpg"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		m, err := jpeg.Decode(rec.Body)
		if rec.Code != 200 || rec.Header().Get("Content-Type") != "image/jpeg" || err != nil || m.Bounds().Dx() != 1280 || m.Bounds().Dy() != 960 {
			t.Errorf("GET %s: %d %s %v, want 1280x960 (docs/plans/128-smaller-pictures.md)", path, rec.Code, rec.Header().Get("Content-Type"), err)
		}
	}
	for path, want := range map[string]string{pic: "private, max-age=31536000, immutable", "/previews?title=2026-10-04+LAKE.jpg": ""} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		if got := rec.Header().Get("Cache-Control"); got != want {
			t.Errorf("GET %s: Cache-Control %q, want %q", path, got, want)
		}
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/previews?title=Nothing", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("a title with no picture: %d", rec.Code)
	}
	again, err := c.Do(t.Context(), add, map[string]string{"title": "Another name.jpg", "original": path})
	if res, _ := again.Result.(map[string]any); err != nil || res["existing"] != true || apitest.Href(again, "self") != apitest.Href(e, "self") {
		t.Errorf("the same original again: %v %+v", err, again.Result)
	}
	if got := names(e); got != "save-body,plan,tick,carry,schedule,rename,file-pages,tombstone-pages" {
		t.Errorf("a file page offers %s (never promote)", got)
	}
	if list := apitest.Must(t)(c.Get(t.Context(), "/browse?kind=media")); len(list.Entities) != 1 || list.Entities[0].Title != "2026-10-04 Lake.jpg" {
		t.Errorf("/browse?kind=media: %+v", list.Entities)
	}
}

func TestAddFileFromAnAgentAndWithAPicture(t *testing.T) {
	c, _, _ := apitest.Fresh(t, "cli")
	add := apitest.Action(t, apitest.Must(t)(c.Get(t.Context(), "/")), "add-file")
	e, err := c.Do(t.Context(), add, map[string]string{"title": "Memo.m4a", "sha256": strings.Repeat("ab", 32), "mime": "audio/mp4", "body": "the transcript"})
	if err != nil || props(e)["body"] != "the transcript" || apitest.Href(e, "preview") != "" {
		t.Fatalf("an agent's keep: %v %+v", err, props(e))
	}
	if _, err := c.Do(t.Context(), add, map[string]string{"title": "Memo 2.m4a", "body": "x"}); err == nil {
		t.Error("a keep with neither the original nor its hash")
	}
	// a format lifelog cannot read keeps no picture until one is sent with the same original
	heic := filepath.Join(t.TempDir(), "IMG_0002.HEIC")
	os.WriteFile(heic, []byte("not really a HEIC, but its bytes are hashed"), 0o644)
	e = apitest.Must(t)(c.Do(t.Context(), add, map[string]string{"title": "IMG_0002.HEIC", "original": heic}))
	if file, _ := props(e)["file"].(map[string]any); file["mime"] != "image/heic" || file["preview"] != false {
		t.Fatalf("a HEIC: %+v", props(e))
	}
	frame, _ := jpegFile(t, "frame.jpg", 300, 200)
	e = apitest.Must(t)(c.Do(t.Context(), add, map[string]string{"title": "IMG_0002.HEIC", "original": heic, "preview": frame}))
	if res, _ := e.Result.(map[string]any); res["preview_added"] != true || apitest.Href(e, "preview") == "" {
		t.Errorf("a picture sent later: %+v", e.Result)
	}
	if _, err := c.Do(t.Context(), add, map[string]string{"title": "x", "sha256": strings.Repeat("0", 64), "original": heic}); err == nil {
		t.Error("a sha256 that is not the original's was accepted")
	}
}

func TestAddFileKeepsMalformedHEIFWithUnknownMetadata(t *testing.T) {
	c, _, _ := apitest.Fresh(t, "cli")
	add := apitest.Action(t, apitest.Must(t)(c.Get(t.Context(), "/")), "add-file")
	heic := filepath.Join(t.TempDir(), "bad.HEIC")
	overflow := phototest.HEIF(photo.Meta{Taken: "2019-06-03 07:41:12"}, phototest.Layout{InIdat: true, Version: 1, Overflow: true})
	if err := os.WriteFile(heic, overflow, 0o644); err != nil {
		t.Fatal(err)
	}
	e := apitest.Must(t)(c.Do(t.Context(), add, map[string]string{"title": "bad.HEIC", "body": "kept despite bad metadata", "original": heic}))
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
	c, h, _ := apitest.Fresh(t, "cli")
	root := apitest.Must(t)(c.Get(t.Context(), "/"))
	path, _ := jpegFile(t, "lake.jpg", 40, 30)
	f := apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "add-file"), map[string]string{"title": "Lake.jpg", "body": "dawn", "original": path}))
	day := apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "capture"), map[string]string{"text": "Swam. ![[Lake.jpg|the lake]]"}))
	for path, want := range map[string][]string{
		"/browse?kind=document": {`enctype="multipart/form-data"`, `type="file" name="original"`, `type="hidden" name="sha256"`},
		"/browse?kind=media":    {"Lake.jpg", `action="/files"`, `<img src="` + apitest.Href(f, "preview") + `" alt="" loading="lazy">`},
		apitest.Href(f, "self"): {`<img class="preview" src="` + apitest.Href(f, "preview") + `"`, "image/jpeg", "Backlinks"},
		// the first picture of a day page is its cover, drawn above it and not again in the body
		apitest.Href(day, "page"): {`<figure class="cover"><img src="` + apitest.Href(f, "preview") + `" alt="Lake.jpg">`, "Swam."},
	} {
		body := browse(t, h, path)
		for _, w := range want {
			if !strings.Contains(body, w) {
				t.Errorf("%s does not show %s", path, w)
			}
		}
	}
	if body := browse(t, h, apitest.Href(day, "page")); strings.Contains(body, `/previews?title=Lake.jpg`) {
		t.Errorf("the day page draws its cover twice")
	}
}

// TestAPhotosDay is a photo's own day, read from the original: the file keeps that day, and the page of that day
// shows the photo. Its position is never read (D21).
func TestAPhotosDay(t *testing.T) {
	c, h, _ := apitest.Fresh(t, "cli")
	root := apitest.Must(t)(c.Get(t.Context(), "/"))
	write := func(name string, b []byte) string {
		p := filepath.Join(t.TempDir(), name)
		if err := os.WriteFile(p, b, 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	add := apitest.Action(t, root, "add-file")
	jpg := write("IMG_1.jpg", phototest.WithExif(phototest.JPEG(64, 48), photo.Meta{Taken: "2019-06-03 07:41:12"}, true))
	e := apitest.Must(t)(c.Do(t.Context(), add, map[string]string{"title": "2019-06-03 IMG_1.jpg", "original": jpg}))
	res, _ := e.Result.(map[string]any)
	if res["embedded"] != true || res["day"] != "2019-06-03" || props(e)["day"] != "2019-06-03" || res["place"] != nil || res["unmatched"] != nil {
		t.Fatalf("a photo with a day: %+v", res)
	}
	day := apitest.Must(t)(c.Get(t.Context(), "/days/2019-06-03"))
	if body := browse(t, h, apitest.Href(day, "page")); !strings.Contains(body, `<figure class="cover"><img src="`+apitest.Href(e, "preview")+`"`) {
		t.Errorf("the day page does not show the photo as its cover")
	}
	// a HEIC: no picture, no embed, its day still read
	heic := write("IMG_2.HEIC", phototest.HEIF(photo.Meta{Taken: "2019-06-04 10:00:00"}, phototest.Layout{Version: 1}))
	e = apitest.Must(t)(c.Do(t.Context(), add, map[string]string{"title": "2019-06-04 IMG_2.HEIC", "original": heic}))
	res, _ = e.Result.(map[string]any)
	if res["day"] != "2019-06-04" || res["embedded"] == true {
		t.Fatalf("a HEIC: %+v", res)
	}
}

func TestPromotedFileDay(t *testing.T) {
	c, _, _ := apitest.Fresh(t, "cli")
	root := apitest.Must(t)(c.Get(t.Context(), "/"))
	capture := apitest.Action(t, apitest.Must(t)(c.Get(t.Context(), "/actions")), "capture")
	if _, err := c.Do(t.Context(), capture, map[string]string{"day": "2026-10-04", "text": "Dawn: ![[Lake.jpg]]"}); err != nil {
		t.Fatal(err)
	}
	path, _ := jpegFile(t, "lake.jpg", 40, 30)
	e := apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "add-file"), map[string]string{"title": "lake.jpg", "day": "2026-10-04", "original": path}))
	res, _ := e.Result.(map[string]any)
	if res["promoted"] != true || props(e)["entity_type"] != "file" || props(e)["title"] != "Lake.jpg" || props(e)["day"] != "2026-10-04" {
		t.Fatalf("promoted file: result %+v props %+v", res, props(e))
	}
	day := apitest.Must(t)(c.Get(t.Context(), "/days/2026-10-04"))
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
	c, _, _ := apitest.Fresh(t, "cli")
	root := apitest.Must(t)(c.Get(t.Context(), "/"))
	add := apitest.Action(t, root, "add-file")
	path, _ := jpegFile(t, "x.jpg", 40, 30)
	e := apitest.Must(t)(c.Do(t.Context(), add, map[string]string{"title": "x.jpg", "dry_run": "1", "original": path}))
	if res, _ := e.Result.(map[string]any); !strings.Contains(strings.Join(e.Class, ","), "dry-run") || res == nil || res["id"] != nil {
		t.Errorf("a dry run: %+v %+v", e.Class, e.Result)
	}
	if list := apitest.Must(t)(c.Get(t.Context(), "/browse?kind=media")); len(list.Entities) != 0 {
		t.Error("a dry run kept a file")
	}
	capture := apitest.Action(t, apitest.Must(t)(c.Get(t.Context(), "/actions")), "capture")
	if _, err := c.Do(t.Context(), capture, map[string]string{"day": "2026-10-04", "text": "Dawn: ![[Dry.jpg]]"}); err != nil {
		t.Fatal(err)
	}
	path, _ = jpegFile(t, "dry.jpg", 40, 30)
	e = apitest.Must(t)(c.Do(t.Context(), add, map[string]string{"title": "dry.jpg", "day": "2026-10-04", "dry_run": "1", "original": path}))
	if res, _ := e.Result.(map[string]any); res["promoted"] != true || res["id"] != nil {
		t.Errorf("a dry-run promotion report: %+v", e.Result)
	}
	ghost := apitest.Must(t)(c.Get(t.Context(), "/pages?title=Dry.jpg"))
	if props(ghost)["entity_type"] != "page" || props(ghost)["day"] != nil || props(ghost)["file"] != nil {
		t.Errorf("a dry-run promotion wrote to the ghost: %+v", props(ghost))
	}
	if list := apitest.Must(t)(c.Get(t.Context(), "/browse?kind=media")); len(list.Entities) != 0 {
		t.Error("a dry-run promotion kept a file")
	}
}

func TestBrowserMutationFeedbackPhoto(t *testing.T) {
	_, h, _ := apitest.Fresh(t, "cli")
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
		part.Write(phototest.WithExif(phototest.JPEG(64, 48), photo.Meta{Taken: "2019-06-04 10:00:00"}, true))
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
		for _, want := range []string{"Result", "Its own day is"} { // the toast says the result in words
			if !strings.Contains(body, want) {
				t.Errorf("photo feedback missing %q", want)
			}
		}
	}
}

func TestAutomaticEmbedAPIRefusal(t *testing.T) {
	c, _, _ := apitest.Fresh(t, "cli")
	add := apitest.Action(t, apitest.Must(t)(c.Get(t.Context(), "/")), "add-file")
	path, _ := jpegFile(t, "Lake [1].jpg", 40, 30)
	for _, dry := range []string{"1", "0"} {
		_, err := c.Do(t.Context(), add, map[string]string{"title": "Lake [1].jpg", "day": "2026-10-04", "dry_run": dry, "original": path})
		if apitest.Status(err) != 422 {
			t.Fatalf("dry=%s: %v", dry, err)
		}
	}
}

func TestBrowserMultipartRecoversAcceptedBody(t *testing.T) {
	for _, kind := range []string{"title", "preview", "overflow", "conflict"} {
		t.Run(kind, func(t *testing.T) {
			c, h, _ := apitest.Fresh(t, "cli")
			if kind == "conflict" {
				apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, apitest.Must(t)(c.Get(t.Context(), "/")), "create-place"), map[string]string{"title": "Occupied"}))
			}
			var b bytes.Buffer
			mw := multipart.NewWriter(&b)
			mw.WriteField("body", "transcript <script> & draft")
			title := "bad/name"
			if kind != "title" {
				title = "Occupied"
			}
			mw.WriteField("title", title)
			mw.WriteField("sha256", strings.Repeat("a", 64))
			mw.WriteField("mime", "audio/mp4")
			switch kind {
			case "preview":
				p, _ := mw.CreateFormFile("preview", "bad.png")
				p.Write([]byte("invalid"))
			case "overflow":
				mw.WriteField("unknown", strings.Repeat("x", 16<<20))
			}
			mw.Close()
			req := httptest.NewRequest("POST", "/files", &b)
			req.Header.Set("Content-Type", mw.FormDataContentType())
			req.Header.Set("Accept", "text/html")
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code < 400 || !strings.Contains(rec.Body.String(), "transcript &lt;script&gt; &amp; draft</textarea>") {
				t.Fatalf("%s: status %d recovered=%v", kind, rec.Code, strings.Contains(rec.Body.String(), "draft</textarea>"))
			}
			if len(apitest.Must(t)(c.Get(t.Context(), "/browse?kind=media")).Entities) != 0 {
				t.Fatal("wrote file")
			}
		})
	}
}

func TestPhotoDaySyncJSONAndBrowserReceipt(t *testing.T) {
	for _, browser := range []bool{false, true} {
		t.Run(map[bool]string{false: "JSON", true: "browser"}[browser], func(t *testing.T) {
			c, h, _ := apitest.Fresh(t, "cli")
			apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, apitest.Must(t)(c.Get(t.Context(), "/")), "create-page"), map[string]string{"title": "2026-10-04", "body": "[[Old synthetic target]]"}))
			target := apitest.Must(t)(c.Get(t.Context(), "/pages?title=Old+synthetic+target"))
			apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, target, "tombstone-pages"), nil))
			var b bytes.Buffer
			mw := multipart.NewWriter(&b)
			for key, value := range map[string]string{"title": "Next synthetic.jpg", "day": "2026-10-04", "body": "[[File synthetic target]]"} {
				if err := mw.WriteField(key, value); err != nil {
					t.Fatal(err)
				}
			}
			part, err := mw.CreateFormFile("original", "synthetic.jpg")
			if err != nil {
				t.Fatal(err)
			}
			path, _ := jpegFile(t, "synthetic.jpg", 16, 16)
			// The fixture is generated locally and contains no metadata.
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := part.Write(data); err != nil {
				t.Fatal(err)
			}
			if err := mw.Close(); err != nil {
				t.Fatal(err)
			}
			r := httptest.NewRequest("POST", "/files", &b)
			r.Header.Set("Content-Type", mw.FormDataContentType())
			if browser {
				r.Header.Set("Accept", "text/html")
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			body := w.Body.String()
			if browser {
				if w.Code != 303 {
					t.Fatalf("POST: %d %s", w.Code, body)
				}
				body = browse(t, h, w.Header().Get("Location"))
			} else if w.Code != 200 && w.Code != 201 {
				t.Fatalf("POST: %d %s", w.Code, body)
			}
			wants := []string{"day_sync", "revived", "Old synthetic target", "File synthetic target"}
			if browser { // the toast says the result in words
				wants = []string{"In its day page: ", "Revived", "Old synthetic target", "File synthetic target"}
			}
			for _, want := range wants {
				if !strings.Contains(body, want) {
					t.Errorf("missing %s: %s", want, body)
				}
			}
		})
	}
}

// TestAFileIsTitledByItsNameWhenNoTitleIsSent: add-file with an original and no title takes the original's file name,
// on every surface that sends the original (docs/plans/131-the-pareto-pass.md, part 1). A sha256 has no name: with no
// title it is refused, and nothing is kept.
func TestAFileIsTitledByItsNameWhenNoTitleIsSent(t *testing.T) {
	c, _, _ := apitest.Fresh(t, "cli")
	add := apitest.Action(t, apitest.Must(t)(c.Get(t.Context(), "/")), "add-file")
	path, _ := jpegFile(t, "IMG_0042.jpg", 32, 24)
	if got := apitest.Must(t)(c.Do(t.Context(), add, map[string]string{"original": path})).Title; got != "IMG_0042.jpg" {
		t.Errorf("the file is titled %q, want its name IMG_0042.jpg", got)
	}
	_, err := c.Do(t.Context(), add, map[string]string{"sha256": strings.Repeat("a", 64), "mime": "image/jpeg"})
	if apitest.Status(err) != http.StatusUnprocessableEntity {
		t.Errorf("a sha256 with no title: %v, want 422", err)
	}
}
