package client

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"lifelog-whatsapp/siren"
)

func TestRemoteSendsTheWriterTheTokenAndAFileByItsPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.png")
	if err := os.WriteFile(path, []byte("synthetic"), 0o644); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(siren.SourceHeader) != "agent:t" || r.Header.Get("Authorization") != "Bearer secret" {
			t.Errorf("headers: %v", r.Header)
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Errorf("not multipart: %v", err)
		} else if _, h, err := r.FormFile("original"); err != nil || h.Filename != "a.png" || r.URL.Path != "/files/2031-05-20" {
			t.Errorf("file %v, %v, path %s", h, err, r.URL.Path)
		}
		w.Write([]byte(`{"title":"a.png"}`))
	}))
	defer srv.Close()
	c := Remote(srv.URL+"/", "agent:t", "secret")
	act := siren.Action{Name: "add-file", Method: "POST", Href: "/files/{day}", Fields: []siren.Field{
		{Name: "day", Type: "date", In: "path"}, {Name: "original", Type: "file"}}}
	e, err := c.Do(t.Context(), act, map[string]string{"day": "2031-05-20", "original": path})
	if err != nil || e.Title != "a.png" {
		t.Fatalf("%v %v", e, err)
	}
	if _, err := c.Do(t.Context(), act, map[string]string{"nope": "x"}); err == nil {
		t.Error("a field that the action does not list was sent")
	}
}

func TestAnErrorEntityIsATypedErrorWithItsCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		w.Write([]byte(`{"properties":{"status":409,"code":"stale_version","message":"old"}}`))
	}))
	defer srv.Close()
	e, err := Remote(srv.URL, "", "").Get(t.Context(), "/x")
	var ce *Error
	if !errors.As(err, &ce) || ce.Status != 409 || ce.Code() != "stale_version" || e == nil {
		t.Fatalf("%v %v", e, err)
	}
}

func TestAnAnswerLargerThanTheBoundIsAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"title":"` + strings.Repeat("a", maxResponse) + `"}`))
	}))
	defer srv.Close()
	if e, err := Remote(srv.URL, "", "").Get(t.Context(), "/x"); err == nil || e != nil {
		t.Fatalf("%v %v, want an error and no entity", e, err)
	}
}
