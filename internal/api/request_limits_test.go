package api_test

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"lifelog/internal/api"
	"lifelog/internal/core"
	"lifelog/internal/db/dbtest"
)

type repeatedByte byte

func (b repeatedByte) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = byte(b)
	}
	return len(p), nil
}
func generated(n int64) io.Reader { return io.LimitReader(repeatedByte('x'), n) }

// blanks gives n spaces: JSON reads them as the white space between two tokens.
func blanks(n int64) io.Reader { return io.LimitReader(repeatedByte(' '), n) }

func limitsHandler(t *testing.T) (http.Handler, func()) {
	t.Helper()
	d := dbtest.New(t)
	counts := func() string {
		var out string
		for _, table := range []string{"entities", "entity_names", "measurements", "files"} {
			var n int
			if err := d.R.QueryRow("SELECT count(*) FROM " + table).Scan(&n); err != nil && err != sql.ErrNoRows {
				t.Fatal(err)
			}
			out += fmt.Sprint(n, ",")
		}
		return out
	}
	before := counts()
	return api.New(&core.Store{DB: d}), func() {
		t.Helper()
		if after := counts(); after != before {
			t.Fatalf("writes on refusal: %s -> %s", before, after)
		}
	}
}

// keptBody is the body of the page in the answer w.
func keptBody(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var page struct {
		Properties struct{ Body string } `json:"properties"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
		t.Fatalf("the answer: %v", err)
	}
	return page.Properties.Body
}

// TestRequestBufferLimits: a JSON or URL-encoded request has at most 20 MiB. The limit is of the request, not of a
// field. One byte under and one byte over the limit, blanks between two tokens fill the request. At the limit, the
// body of the page fills it, and the page keeps the whole body. The framing is the same 26 bytes.
func TestRequestBufferLimits(t *testing.T) {
	h, unchanged := limitsHandler(t)
	head, tail := `{"title":"Long",`, `"body":""}`
	framing := int64(len(head) + len(tail))
	for _, delta := range []int64{-1, 0, 1} {
		boundaryHandler, boundaryUnchanged := limitsHandler(t)
		n := int64(20<<20) - framing + delta
		body := io.MultiReader(strings.NewReader(head), blanks(n), strings.NewReader(tail))
		if delta == 0 {
			body = io.MultiReader(strings.NewReader(`{"title":"Long","body":"`), generated(n), strings.NewReader(`"}`))
		}
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
		if delta == 0 {
			if got := keptBody(t, w); got != strings.Repeat("x", int(n)) {
				t.Fatalf("the body at the limit: %d bytes, want %d bytes of x", len(got), n)
			}
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
	// fields is the length of the text of the three fields that come first: title, sha256 and mime.
	fields := int64(len("Synthetic") + 64 + len("application/octet-stream"))
	for _, kind := range []string{"aggregate", "unique", "body-at", "text-at", "text-over", "parts", "original", "preview", "large-original"} {
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
				case "body-at":
					// The body fills the text of the request to the limit, and the page keeps the whole body.
					p, e := mw.CreateFormField("body")
					if e != nil {
						return
					}
					if _, e = io.Copy(p, generated(16<<20-fields)); e != nil {
						return
					}
				case "text-at", "text-over":
					// An unknown field fills the text of the request to the limit, less a short body. The body comes
					// last: it is kept only when the text of all the fields is at most 16 MiB.
					p, e := mw.CreateFormField("unknown")
					if e != nil {
						return
					}
					n := 16<<20 - fields - int64(len("the text"))
					if kind == "text-over" {
						n++
					}
					if _, e = io.Copy(p, generated(n)); e != nil {
						return
					}
					if mw.WriteField("body", "the text") != nil {
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
			if kind == "large-original" || kind == "body-at" || kind == "text-at" {
				want = 200
			} // an original larger than the preview and JSON budgets still succeeds
			if want == 413 {
				unchanged()
			}
			if w.Code != want {
				t.Fatalf("%d want %d: %.200s", w.Code, want, w.Body.String())
			}
			switch kind {
			case "body-at":
				if got := keptBody(t, w); got != strings.Repeat("x", int(16<<20-fields)) {
					t.Fatalf("the body that fills the text: %d bytes, want %d bytes of x", len(got), 16<<20-fields)
				}
			case "text-at":
				if got := keptBody(t, w); got != "the text" {
					t.Fatalf("the body at the limit: %q; want it kept", got)
				}
			}
		})
	}
}
