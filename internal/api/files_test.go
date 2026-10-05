package api_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
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
	add := find(must(c.Get("/")), "add-file")
	path, sum := jpegFile(t, "IMG_0001.JPG", 2400, 1800)
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
	frame, _ := jpegFile(t, "frame.jpg", 300, 200)
	e = must(c.DoFiles(add, map[string]string{"title": "IMG_0002.HEIC"}, map[string]string{"original": heic, "preview": frame}))
	if res, _ := e.Result.(map[string]any); res["preview_added"] != true || href(e, "preview") == "" {
		t.Errorf("a picture sent later: %+v", e.Result)
	}
	if _, err := c.DoFiles(add, map[string]string{"title": "x", "sha256": strings.Repeat("0", 64)}, map[string]string{"original": heic}); err == nil {
		t.Error("a sha256 that is not the original's was accepted")
	}
}

func TestAddFileKeepsMalformedHEIFWithUnknownMetadata(t *testing.T) {
	c, _ := fresh(t)
	add := find(must(c.Get("/")), "add-file")
	heic := filepath.Join(t.TempDir(), "bad.HEIC")
	if err := os.WriteFile(heic, phototest.HEICOverflowingLocation(), 0o644); err != nil {
		t.Fatal(err)
	}
	e := must(c.DoFiles(add, map[string]string{"title": "bad.HEIC", "body": "kept despite bad metadata"}, map[string]string{"original": heic}))
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
	root := must(c.Get("/"))
	path, _ := jpegFile(t, "lake.jpg", 40, 30)
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

// TestAPhotosPlaceAndDay is plan 031 through the API: a photo's own day and position, read from the original.
func TestAPhotosPlaceAndDay(t *testing.T) {
	c, h := fresh(t)
	root := must(c.Get("/"))
	lisbon := must(c.Do(find(root, "create-place"), map[string]string{"title": "Lisbon"}))
	lisbon = must(c.Do(find(lisbon, "locate"), map[string]string{"lat": "38.7223", "lon": "-9.1393", "radius_m": "10000"}))
	if pt, _ := props(lisbon)["point"].(map[string]any); pt["radius_m"] != float64(10000) || pt["link_days"] != true || href(lisbon, "map") == "" {
		t.Fatalf("Lisbon located: %+v", props(lisbon))
	}
	write := func(name string, b []byte) string {
		p := filepath.Join(t.TempDir(), name)
		if err := os.WriteFile(p, b, 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	add := find(root, "add-file")
	jpg := write("IMG_1.jpg", phototest.JPEG(64, 48, photo.Meta{Taken: "2019-06-03 07:41:12", Lat: 38.7139, Lon: -9.1394, HasGPS: true}, true))
	e := must(c.DoFiles(add, map[string]string{"title": "2019-06-03 IMG_1.jpg"}, map[string]string{"original": jpg}))
	res, _ := e.Result.(map[string]any)
	if res["place"] != "Lisbon" || res["linked"] != true || res["embedded"] != true || res["day"] != "2019-06-03" || props(e)["day"] != "2019-06-03" {
		t.Fatalf("a photo in Lisbon: %+v", res)
	}
	day := must(c.Get("/days/2019-06-03"))
	if !strings.Contains(fmt.Sprint(day.Properties), "Lisbon") {
		t.Errorf("the day view does not say where the day was: %+v", day.Properties)
	}
	if body := browse(t, h, href(day, "page")); !strings.Contains(body, `<img src="/previews?title=2019-06-03+IMG_1.jpg"`) {
		t.Errorf("the day page does not show the photo")
	}
	// a HEIC in Porto: no picture, no embed; near no place, then named
	heic := write("IMG_2.HEIC", phototest.HEIC(photo.Meta{Taken: "2019-06-04 10:00:00", Lat: 41.1496, Lon: -8.6110, HasGPS: true}, false, false))
	e = must(c.DoFiles(add, map[string]string{"title": "2019-06-04 IMG_2.HEIC"}, map[string]string{"original": heic}))
	res, _ = e.Result.(map[string]any)
	u, _ := res["unmatched"].(map[string]any)
	if u == nil || !strings.Contains(fmt.Sprint(u["map"]), "openstreetmap.org") || res["linked"] == true || res["embedded"] == true {
		t.Fatalf("a HEIC near no place: %+v", res)
	}
	e = must(c.DoFiles(add, map[string]string{"title": "x", "at": "Porto", "radius": "6000"}, map[string]string{"original": heic}))
	if res, _ = e.Result.(map[string]any); res["existing"] != true || res["point_set"] != true || res["linked"] != true || res["place"] != "Porto" {
		t.Errorf("named: %+v", res)
	}
	porto := must(c.Get("/pages?title=Porto"))
	if pt, _ := props(porto)["point"].(map[string]any); pt["radius_m"] != float64(6000) || find(porto, "locate").Name == "" {
		t.Errorf("Porto: %+v", props(porto))
	}
	if body := browse(t, h, href(porto, "self")); !strings.Contains(body, "6000 m") || !strings.Contains(body, `action="`+href(porto, "self")+`/locate"`) {
		t.Errorf("a place page shows no point or no locate form")
	}
	if _, err := c.Do(find(porto, "locate"), map[string]string{"lat": "0", "lon": "0", "radius_m": "100"}); err == nil {
		t.Error("0°, 0° was accepted as a place's point")
	}
	if _, err := c.DoFiles(add, map[string]string{"title": "y", "radius": "big"}, map[string]string{"original": heic}); err == nil {
		t.Error("a radius that is not a number was accepted")
	}
}

func TestPromotedFileDay(t *testing.T) {
	c, _ := fresh(t)
	root := must(c.Get("/"))
	capture := find(must(c.Get("/actions")), "capture")
	if _, err := c.Do(capture, map[string]string{"day": "2026-10-04", "text": "Dawn: ![[Lake.jpg]]"}); err != nil {
		t.Fatal(err)
	}
	path, _ := jpegFile(t, "lake.jpg", 40, 30)
	e := must(c.DoFiles(find(root, "add-file"), map[string]string{"title": "lake.jpg", "day": "2026-10-04"}, map[string]string{"original": path}))
	res, _ := e.Result.(map[string]any)
	if res["promoted"] != true || props(e)["entity_type"] != "file" || props(e)["title"] != "Lake.jpg" || props(e)["day"] != "2026-10-04" {
		t.Fatalf("promoted file: result %+v props %+v", res, props(e))
	}
	day := must(c.Get("/days/2026-10-04"))
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
	root := must(c.Get("/"))
	add := find(root, "add-file")
	path, _ := jpegFile(t, "x.jpg", 40, 30)
	e := must(c.DoFiles(add, map[string]string{"title": "x.jpg", "dry_run": "1"}, map[string]string{"original": path}))
	if res, _ := e.Result.(map[string]any); !strings.Contains(strings.Join(e.Class, ","), "dry-run") || res == nil || res["id"] != nil {
		t.Errorf("a dry run: %+v %+v", e.Class, e.Result)
	}
	if list := must(c.Get("/files")); len(list.Entities) != 0 {
		t.Error("a dry run kept a file")
	}
	capture := find(must(c.Get("/actions")), "capture")
	if _, err := c.Do(capture, map[string]string{"day": "2026-10-04", "text": "Dawn: ![[Dry.jpg]]"}); err != nil {
		t.Fatal(err)
	}
	path, _ = jpegFile(t, "dry.jpg", 40, 30)
	e = must(c.DoFiles(add, map[string]string{"title": "dry.jpg", "day": "2026-10-04", "dry_run": "1"}, map[string]string{"original": path}))
	if res, _ := e.Result.(map[string]any); res["promoted"] != true || res["id"] != nil {
		t.Errorf("a dry-run promotion report: %+v", e.Result)
	}
	ghost := must(c.Get("/pages?title=Dry.jpg"))
	if props(ghost)["entity_type"] != "page" || props(ghost)["day"] != nil || props(ghost)["file"] != nil {
		t.Errorf("a dry-run promotion wrote to the ghost: %+v", props(ghost))
	}
	if list := must(c.Get("/files")); len(list.Entities) != 0 {
		t.Error("a dry-run promotion kept a file")
	}
}

func TestPhotoGPSReferences(t *testing.T) {
	for _, malformed := range []bool{true, false} {
		t.Run(fmt.Sprint(malformed), func(t *testing.T) {
			c, _ := fresh(t)
			root := must(c.Get("/"))
			place := must(c.Do(find(root, "create-place"), map[string]string{"title": "Southwest"}))
			if !malformed {
				must(c.Do(find(place, "locate"), map[string]string{"lat": "-12", "lon": "-34", "radius_m": "1000"}))
			}
			b := phototest.JPEG(32, 24, photo.Meta{Taken: "2026-10-05 12:34:56", Lat: -12, Lon: -34, HasGPS: true, Orientation: 6}, true)
			if malformed {
				b = phototest.GPSReference(b, 1, 2, 2, "Sx", false)
			}
			p := filepath.Join(t.TempDir(), "synthetic.jpg")
			if err := os.WriteFile(p, b, 0o644); err != nil {
				t.Fatal(err)
			}
			fields := map[string]string{"title": "synthetic.jpg", "body": "kept text"}
			e := must(c.DoFiles(find(root, "add-file"), fields, map[string]string{"original": p}))
			res, _ := e.Result.(map[string]any)
			file, _ := props(e)["file"].(map[string]any)
			if props(e)["body"] != "kept text" || file["preview"] != true || res["day"] != "2026-10-05" || res["embedded"] != true {
				t.Fatalf("file/day lost: %+v %+v", props(e), res)
			}
			if malformed {
				if res["linked"] == true || res["point_set"] == true || res["unmatched"] != nil {
					t.Errorf("guessed GPS persisted: %+v", res)
				}
				place = must(c.Get("/pages?title=Southwest"))
				if props(place)["point"] != nil {
					t.Errorf("guessed point: %+v", props(place))
				}
				day := must(c.Get("/days/2026-10-05"))
				if strings.Contains(fmt.Sprint(day.Properties), "Southwest") {
					t.Errorf("guessed at link: %+v", day.Properties)
				}
			} else if res["place"] != "Southwest" || res["linked"] != true {
				t.Errorf("valid southwest lost: %+v", res)
			}
		})
	}
}

func TestBrowserMutationFeedbackPhoto(t *testing.T) {
	_, h := fresh(t)
	for _, dry := range []bool{true, false} {
		var b bytes.Buffer
		mw := multipart.NewWriter(&b)
		mw.WriteField("title", "Unmatched synthetic.jpg")
		if dry {
			mw.WriteField("dry_run", "1")
		}
		part, err := mw.CreateFormFile("original", "synthetic.jpg")
		if err != nil {
			t.Fatal(err)
		}
		part.Write(phototest.JPEG(64, 48, photo.Meta{Taken: "2019-06-04 10:00:00", Lat: 41.1496, Lon: -8.6110, HasGPS: true}, true))
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
		for _, want := range []string{"Result", "unmatched", "41.1496"} {
			if !strings.Contains(body, want) {
				t.Errorf("photo feedback missing %q", want)
			}
		}
	}
}

func TestAutomaticEmbedAPIRefusal(t *testing.T) {
	c, _ := fresh(t)
	add := find(must(c.Get("/")), "add-file")
	path, _ := jpegFile(t, "Lake [1].jpg", 40, 30)
	for _, dry := range []string{"1", "0"} {
		_, err := c.DoFiles(add, map[string]string{"title": "Lake [1].jpg", "day": "2026-10-04", "dry_run": dry}, map[string]string{"original": path})
		if err == nil || !strings.Contains(err.Error(), "explicit") {
			t.Fatalf("dry=%s: %v", dry, err)
		}
	}
}
