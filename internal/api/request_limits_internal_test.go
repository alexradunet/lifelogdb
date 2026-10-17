package api

import (
	"bytes"
	"fmt"
	"io"
	"mime/multipart"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"lifelog/internal/core"
)

// TestMultipartUnknownNameProjection: parseFileForm keeps the value of a name it knows, and of any other name it keeps
// neither the name nor the value. Four parts have a name of 64 KiB and no value: modest headers, each one legal, not
// a stress fixture.
func TestMultipartUnknownNameProjection(t *testing.T) {
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	field := func(name, value string) {
		w, err := mw.CreateFormField(name)
		if err == nil {
			_, err = io.WriteString(w, value)
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	sum := strings.Repeat("ab", 32)
	for _, f := range [][2]string{{"title", "Scan"}, {"sha256", sum}, {"mime", "application/pdf"}, {"body", "the text"},
		{"day", "2031-05-14"}, {"category", "Letters"}, {"dry_run", "1"}, {"back", "/browse?kind=document"}} {
		field(f[0], f[1])
	}
	// After the names it knows, so that a name read as one of them changes its value. "original" and "preview" with no
	// file name are file inputs left empty.
	for _, name := range []string{"", "unknown", "Title", "title-extra", "original", "preview"} {
		field(name, "NOT KEPT")
	}
	for i := range 4 {
		field(fmt.Sprint("unknown", i, strings.Repeat("x", 64<<10)), "")
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", "/files", &body)
	r.Header.Set("Content-Type", mw.FormDataContentType())

	f, err := parseFileForm(r, specOf("add-file"))
	want := core.FileIn{Title: "Scan", Category: "Letters", SHA256: sum, MIME: "application/pdf", Body: "the text", Day: "2031-05-14"}
	if err != nil || !f.Sent || !f.Dry || !reflect.DeepEqual(f.In, want) {
		t.Errorf("parseFileForm = %+v, %v; want %+v, dry and sent", f, err, want)
	}
	// Only the body and the page to return to are kept for the browser.
	if got := fmt.Sprint(r.PostForm); got != "map[back:[/browse?kind=document] body:[the text]]" {
		t.Errorf("the form kept for the browser: %s", got)
	}
}
