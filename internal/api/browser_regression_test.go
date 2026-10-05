package api_test

import (
	"bytes"
	"mime/multipart"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

func TestBrowserAllReadingsCoordinates(t *testing.T) {
	c, h := fresh(t)
	root := must(c.Get("/"))
	must(c.Do(find(root, "register-metric"), map[string]string{"name": "synthetic"}))
	for _, day := range []string{"2026-01-01", "2026-09-01"} {
		must(c.Do(find(root, "record"), map[string]string{"metric": "synthetic", "day": day, "value": "1"}))
	}
	body := browse(t, h, "/metrics/synthetic?from=2026-06-03&to=2026-09-01")
	link := regexp.MustCompile(`href="([^"]+)"\>All readings`).FindStringSubmatch(body)
	if len(link) != 2 {
		t.Fatal("missing all readings link")
	}
	all := browse(t, h, strings.ReplaceAll(link[1], "&amp;", "&"))
	xs := regexp.MustCompile(`<circle cx="([^"]+)"`).FindAllStringSubmatch(all, -1)
	if len(xs) != 2 || xs[0][1] == xs[1][1] {
		t.Fatalf("collapsed coordinates: %v", xs)
	}
	if !strings.Contains(body, `cx="626.0"`) {
		t.Error("90 day endpoint")
	}
	if strings.Contains(browse(t, h, "/metrics/synthetic?from=2026-09-01&to=2026-09-01"), `<svg class="chart"`) {
		t.Error("same day exclusive range should be empty")
	}
	if strings.Contains(browse(t, h, "/metrics/synthetic?from=2025-01-01&to=2025-02-01"), `<svg class="chart"`) {
		t.Error("empty chart")
	}
}

func TestBrowserMultipartRecoversAcceptedBody(t *testing.T) {
	for _, kind := range []string{"title", "preview", "radius", "overflow", "conflict"} {
		t.Run(kind, func(t *testing.T) {
			c, h := fresh(t)
			if kind == "conflict" {
				must(c.Do(find(must(c.Get("/")), "create-place"), map[string]string{"title": "Occupied"}))
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
			mw.WriteField("mime", "text/plain")
			switch kind {
			case "preview":
				p, _ := mw.CreateFormFile("preview", "bad.png")
				p.Write([]byte("invalid"))
			case "radius":
				mw.WriteField("radius", "bad")
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
			if len(must(c.Get("/files")).Entities) != 0 {
				t.Fatal("wrote file")
			}
		})
	}
}

func TestBrowserLocateZeroAxes(t *testing.T) {
	c, h := fresh(t)
	p := must(c.Do(find(must(c.Get("/")), "create-place"), map[string]string{"title": "Synthetic point"}))
	for _, coords := range [][2]string{{"0", "12"}, {"12", "0"}, {"12", "34"}} {
		p = must(c.Do(find(p, "locate"), map[string]string{"lat": coords[0], "lon": coords[1], "radius_m": "100"}))
		body := browse(t, h, href(p, "self"))
		for i, name := range []string{"lat", "lon"} {
			if !strings.Contains(body, `name="`+name+`" value="`+coords[i]+`"`) {
				t.Errorf("missing %s=%s", name, coords[i])
			}
		}
	}
}
