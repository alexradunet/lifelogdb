package api_test

import (
	"database/sql"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"lifelog/internal/api"
	"lifelog/internal/core"
	"lifelog/internal/db"
)

type repeatedByte struct{}

func (repeatedByte) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'x'
	}
	return len(p), nil
}
func generated(n int64) io.Reader { return io.LimitReader(repeatedByte{}, n) }

func limitsHandler(t *testing.T) (http.Handler, func()) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "life.db")
	if err := db.Init(path); err != nil {
		t.Fatal(err)
	}
	d, err := db.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	counts := func() string {
		var out string
		for _, table := range []string{"entities", "pages", "measurements", "files"} {
			var n int
			if err := d.R.QueryRow("SELECT count(*) FROM " + table).Scan(&n); err != nil && err != sql.ErrNoRows {
				t.Fatal(err)
			}
			out += fmt.Sprint(n, ",")
		}
		return out
	}
	before := counts()
	return api.New(&core.Store{DB: d}, nil), func() {
		t.Helper()
		if after := counts(); after != before {
			t.Fatalf("writes on refusal: %s -> %s", before, after)
		}
	}
}

func TestRequestBufferLimits(t *testing.T) {
	h, unchanged := limitsHandler(t)
	framing := int64(len(`{"title":"Long","body":""}`))
	for _, delta := range []int64{-1, 0, 1} {
		boundaryHandler, boundaryUnchanged := limitsHandler(t)
		n := int64(20<<20) - framing + delta
		body := io.MultiReader(strings.NewReader(`{"title":"Long","body":"`), generated(n), strings.NewReader(`"}`))
		r := httptest.NewRequest("POST", "/pages", body)
		r.Header.Set("Content-Type", "application/json")
		r.TransferEncoding = []string{"chunked"}
		w := httptest.NewRecorder()
		boundaryHandler.ServeHTTP(w, r)
		want := http.StatusOK
		if delta > 0 {
			want = 413
		}
		if delta > 0 {
			boundaryUnchanged()
		}
		if w.Code != want {
			t.Fatalf("size %d: %d want %d: %.200s", n+framing, w.Code, want, w.Body.String())
		}
	}
	defer unchanged()
	for _, ct := range []string{"application/json", "application/x-www-form-urlencoded"} {
		for _, html := range []bool{false, true} {
			r := httptest.NewRequest("POST", "/pages", io.MultiReader(strings.NewReader(`{"title":"Overflow","body":"`), generated(21<<20), strings.NewReader(`"}`)))
			r.ContentLength = 1
			r.Header.Set("Content-Type", ct)
			if html {
				r.Header.Set("Accept", "text/html")
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != 413 {
				t.Fatalf("%s html=%v: %d %.200s", ct, html, w.Code, w.Body.String())
			}
		}
	}
	r := httptest.NewRequest("POST", "/pages", strings.NewReader(`{"title":"Suffix"} {}`))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 400 {
		t.Fatalf("suffix: %d", w.Code)
	}
}

func TestMultipartAggregateLimits(t *testing.T) {
	for _, kind := range []string{"aggregate", "unique", "text-at", "text-over", "parts", "original", "preview", "large-original"} {
		t.Run(kind, func(t *testing.T) {
			h, unchanged := limitsHandler(t)
			pr, pw := io.Pipe()
			mw := multipart.NewWriter(pw)
			go func() {
				defer pw.Close()
				defer mw.Close()
				mw.WriteField("title", "Synthetic")
				if kind != "large-original" {
					mw.WriteField("sha256", strings.Repeat("a", 64))
				}
				mw.WriteField("mime", "application/octet-stream")
				switch kind {
				case "text-at", "text-over":
					p, e := mw.CreateFormField("body")
					if e != nil {
						return
					}
					n := int64(16<<20) - int64(len("Synthetic")+64+len("application/octet-stream"))
					if kind == "text-over" {
						n++
					}
					if _, e = io.Copy(p, generated(n)); e != nil {
						return
					}
				case "aggregate", "unique":
					for i := 0; i < 17; i++ {
						p, e := mw.CreateFormField(func() string {
							if kind == "unique" {
								return fmt.Sprint("unknown", i)
							}
							return "unknown"
						}())
						if e != nil {
							return
						}
						if _, e = io.Copy(p, generated(1<<20)); e != nil {
							return
						}
					}
				case "parts":
					for i := 0; i < 65; i++ {
						if mw.WriteField("unknown", "") != nil {
							return
						}
					}
				case "original", "preview":
					for i := 0; i < 2; i++ {
						p, e := mw.CreateFormFile(kind, "synthetic.bin")
						if e != nil {
							return
						}
						if _, e = p.Write([]byte("x")); e != nil {
							return
						}
					}
				case "large-original":
					p, e := mw.CreateFormFile("original", "synthetic.bin")
					if e != nil {
						return
					}
					io.Copy(p, generated(65<<20))
				}
			}()
			defer pr.Close()
			r := httptest.NewRequest("POST", "/files", pr)
			r.Header.Set("Content-Type", mw.FormDataContentType())
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			want := 413
			if kind == "large-original" || kind == "text-at" {
				want = 200
			} // an original larger than the preview and JSON budgets still succeeds
			if want == 413 {
				unchanged()
			}
			if w.Code != want {
				t.Fatalf("%d want %d: %.200s", w.Code, want, w.Body.String())
			}
		})
	}
}
