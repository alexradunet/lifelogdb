package google

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type staticToken string

func (s staticToken) AccessToken(context.Context) (string, error) { return string(s), nil }

func TestListItemsReadsEveryPageAndSkipsVideos(t *testing.T) {
	var sawAuth, sawSession bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/mediaItems" {
			http.NotFound(w, r)
			return
		}
		sawAuth = r.Header.Get("Authorization") == "Bearer tok"
		sawSession = r.URL.Query().Get("sessionId") == "s1" && r.URL.Query().Get("pageSize") == "100"
		switch r.URL.Query().Get("pageToken") {
		case "":
			fmt.Fprint(w, `{"mediaItems":[
			 {"id":"a","createTime":"2031-05-01T10:00:00Z","type":"PHOTO","mediaFile":{"baseUrl":"http://x/a","mimeType":"image/jpeg","filename":"a.jpg"}},
			 {"id":"v","createTime":"2031-05-01T11:00:00Z","type":"VIDEO","mediaFile":{"baseUrl":"http://x/v","mimeType":"video/mp4","filename":"v.mp4"}}],
			 "nextPageToken":"p2"}`)
		case "p2":
			fmt.Fprint(w, `{"mediaItems":[
			 {"id":"u","createTime":"2031-05-02T10:00:00Z","type":"TYPE_UNSPECIFIED","mediaFile":{"baseUrl":"http://x/u","mimeType":"image/jpeg"}},
			 {"id":"b","createTime":"2031-05-02T10:00:00.5Z","type":"PHOTO","mediaFile":{"baseUrl":"http://x/b","mimeType":"image/heic","filename":"b.heic"}}]}`)
		default:
			http.Error(w, "bad token", http.StatusBadRequest)
		}
	}))
	defer srv.Close()
	p := &Picker{Base: srv.URL, HTTP: srv.Client(), Tokens: staticToken("tok")}
	items, err := p.ListItems(context.Background(), "s1")
	if err != nil {
		t.Fatal(err)
	}
	if !sawAuth || !sawSession {
		t.Errorf("request: bearer token seen %v, session and page size seen %v", sawAuth, sawSession)
	}
	var ids []string
	for _, it := range items {
		ids = append(ids, it.ID)
	}
	if got := strings.Join(ids, ","); got != "a,b" {
		t.Errorf("items = %s, want a,b (the video and the item with no type are skipped)", got)
	}
	if items[1].MIME != "image/heic" || items[1].BaseURL != "http://x/b" {
		t.Errorf("item b = %+v", items[1])
	}
}

func TestItemDayUsesTheZone(t *testing.T) {
	bucharest, err := time.LoadLocation("Europe/Bucharest")
	if err != nil {
		t.Skipf("no tz database: %v", err)
	}
	// 21:30 UTC on 31 May is 00:30 on 1 June in Bucharest (UTC+3 in summer).
	it := Item{CreateTime: time.Date(2031, 5, 31, 21, 30, 0, 0, time.UTC)}
	if got := it.Day(bucharest); got != "2031-06-01" {
		t.Errorf("Bucharest day = %s, want 2031-06-01", got)
	}
	if got := it.Day(time.UTC); got != "2031-05-31" {
		t.Errorf("UTC day = %s, want 2031-05-31", got)
	}
}

func TestFetchSendsTheTokenAndRefusesALargeBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			http.Error(w, "no", http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/m=d":
			w.Header().Set("Content-Type", "image/jpeg; charset=x")
			fmt.Fprint(w, "bytes")
		case "/gone=d":
			http.Error(w, "gone", http.StatusNotFound)
		}
	}))
	defer srv.Close()
	p := &Picker{Base: srv.URL, HTTP: srv.Client(), Tokens: staticToken("tok")}
	b, typ, err := p.Fetch(context.Background(), srv.URL+"/m", "=d")
	if err != nil || string(b) != "bytes" || typ != "image/jpeg" {
		t.Errorf("Fetch = %q, %q, %v", b, typ, err)
	}
	_, _, err = p.Fetch(context.Background(), srv.URL+"/gone", "=d")
	se, ok := err.(*StatusError)
	if !ok || se.Status != http.StatusNotFound {
		t.Errorf("error = %v, want a StatusError 404", err)
	}
}

func TestSessionLifecycle(t *testing.T) {
	var deleted bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "POST /sessions":
			fmt.Fprint(w, `{"id":"s1","pickerUri":"https://photos.google.com/picker/s1","pollingConfig":{"pollInterval":"3.5s","timeoutIn":"600s"}}`)
		case "GET /sessions/s1":
			fmt.Fprint(w, `{"id":"s1","mediaItemsSet":true,"pollingConfig":{"pollInterval":"5s"}}`)
		case "DELETE /sessions/s1":
			deleted = true
			fmt.Fprint(w, `{}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	p := &Picker{Base: srv.URL, HTTP: srv.Client(), Tokens: staticToken("tok")}
	ctx := context.Background()
	s, err := p.CreateSession(ctx)
	if err != nil || s.ID != "s1" || s.PollInterval != 3500*time.Millisecond || s.MediaItemsSet {
		t.Fatalf("CreateSession = %+v, %v", s, err)
	}
	if s, err = p.GetSession(ctx, "s1"); err != nil || !s.MediaItemsSet {
		t.Fatalf("GetSession = %+v, %v", s, err)
	}
	if err := p.DeleteSession(ctx, "s1"); err != nil || !deleted {
		t.Fatalf("DeleteSession = %v, deleted %v", err, deleted)
	}
}
