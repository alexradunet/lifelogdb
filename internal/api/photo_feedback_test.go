package api_test

import (
	"bytes"
	"mime/multipart"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestPhotoDaySyncJSONAndBrowserReceipt(t *testing.T) {
	for _, browser := range []bool{false, true} {
		t.Run(map[bool]string{false: "JSON", true: "browser"}[browser], func(t *testing.T) {
			c, h := fresh(t)
			must(c.Do(find(must(c.Get("/")), "create-page"), map[string]string{"title": "2026-10-04", "body": "[[Old synthetic target]]"}))
			target := must(c.Get("/pages?title=Old+synthetic+target"))
			must(c.Do(find(target, "tombstone"), nil))
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
			for _, want := range []string{"day_sync", "revived", "Old synthetic target", "File synthetic target"} {
				if !strings.Contains(body, want) {
					t.Errorf("missing %s: %s", want, body)
				}
			}
		})
	}
}
