package lifelog

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// titlesServer answers 409 exists for the first taken titles, then 200. It records the titles it was sent.
func titlesServer(t *testing.T, taken int) (*Client, *[]string) {
	var titles []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret" || r.Header.Get("Lifelog-Source") != Source ||
			r.Header.Get("Accept") != "application/vnd.siren+json" {
			t.Errorf("headers: %v", r.Header)
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Errorf("parse: %v", err)
		}
		titles = append(titles, r.FormValue("title"))
		if len(titles) <= taken {
			w.WriteHeader(http.StatusConflict)
			fmt.Fprint(w, `{"class":["error"],"properties":{"status":409,"code":"exists","message":"title in use"}}`)
			return
		}
		fmt.Fprint(w, `{"class":["file"]}`)
	}))
	t.Cleanup(srv.Close)
	return &Client{Base: srv.URL + "/", Token: "secret", HTTP: srv.Client()}, &titles
}

func TestAddCoverTriesNumberedTitlesAfterExists(t *testing.T) {
	c, titles := titlesServer(t, 2)
	got, err := c.AddCover(context.Background(), "2031-05-04", "image/png", []byte("x"), nil)
	if err != nil || got != "2031-05-04 3.png" {
		t.Fatalf("AddCover = %q, %v; want 2031-05-04 3.png", got, err)
	}
	if want := "2031-05-04.png|2031-05-04 2.png|2031-05-04 3.png"; strings.Join(*titles, "|") != want {
		t.Errorf("titles tried = %v, want %s", *titles, want)
	}
}

func TestAddCoverStopsAfterTheBound(t *testing.T) {
	c, titles := titlesServer(t, 100)
	if _, err := c.AddCover(context.Background(), "2031-05-04", "image/jpeg", []byte("x"), nil); err == nil {
		t.Fatal("no error after every title was in use")
	}
	if len(*titles) != maxTitles {
		t.Errorf("tried %d titles, want %d", len(*titles), maxTitles)
	}
}

func TestAnErrorEntityGivesItsCodeAndMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		fmt.Fprint(w, `{"class":["error"],"properties":{"status":422,"code":"invalid","message":"missing field day"}}`)
	}))
	defer srv.Close()
	c := &Client{Base: srv.URL, HTTP: srv.Client()}
	_, err := c.DayHasCover(context.Background(), "2031-05-04")
	var ae *APIError
	if !errors.As(err, &ae) || ae.Status != 422 || ae.Code != "invalid" || ae.Message != "missing field day" {
		t.Errorf("error = %v, want the code and message of the entity", err)
	}
}

func TestExtOfMediaTypes(t *testing.T) {
	for mime, want := range map[string]string{"image/jpeg": "jpg", "image/png": "png", "image/gif": "gif", "image/heic": "heic", "image/webp": "webp"} {
		if got := Ext(mime); got != want {
			t.Errorf("Ext(%s) = %s, want %s", mime, got, want)
		}
	}
}
