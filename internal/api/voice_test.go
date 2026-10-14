package api_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"lifelog/internal/api"
	"lifelog/internal/client"
	"lifelog/internal/core"
	"lifelog/internal/db"
)

// A voice note sent to capture (docs/plans/109-a-voice-note.md): transcribed by the configured transcriber, its words
// appended to the day page, the recording never stored.

// fakeTranscriber answers with fixed words or a fixed error, and records what it was sent.
type fakeTranscriber struct {
	words string
	err   error
	name  string
	got   []byte
	calls int
}

func (f *fakeTranscriber) Transcribe(_ context.Context, name string, audio io.Reader) (string, error) {
	f.calls++
	f.name = name
	f.got, _ = io.ReadAll(audio)
	return f.words, f.err
}

const voiceDay = "2026-10-14"

// voiceSetup is a fresh life.db, its API with the given transcriber and the day fixed, an owner's client and the store.
func voiceSetup(t *testing.T, tr api.Transcriber) (*client.Client, http.Handler, *core.Store) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "life.db")
	if err := db.Init(t.Context(), p); err != nil {
		t.Fatal(err)
	}
	d, err := db.Open(t.Context(), p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	s := &core.Store{DB: d}
	h := api.NewOn(s, func() string { return voiceDay }, time.UTC, nil, tr)
	return client.InProcess(h, "cli"), h, s
}

// audioFile is a small synthetic recording on disk: the transcriber is fake, so the bytes need no format.
func audioFile(t *testing.T, name string, size int) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, bytes.Repeat([]byte("synthetic audio "), size/16+1)[:size], 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func dayBody(t *testing.T, s *core.Store, day string) string {
	t.Helper()
	id, err := s.PageID(t.Context(), day)
	if err != nil || id == 0 { // no page yet
		return ""
	}
	p, err := s.PageByID(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	return p.Body
}

func filesCount(t *testing.T, s *core.Store) int {
	t.Helper()
	var n int
	if err := s.DB.R.QueryRow(`SELECT count(*) FROM files`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestCaptureTranscribesAVoiceNoteAndStoresNoRecording(t *testing.T) {
	tr := &fakeTranscriber{words: "Walked to the market with [[Sam]]."}
	c, _, s := voiceSetup(t, tr)
	capture := find(must(c.Get(t.Context(), "/")), "capture")
	note := audioFile(t, "memo.m4a", 3000)
	// Words alone: the entry is the transcript.
	e := must(c.DoFiles(t.Context(), capture, map[string]string{}, map[string]string{"audio": note}))
	if got := dayBody(t, s, voiceDay); got != "Walked to the market with [[Sam]]." {
		t.Errorf("the day page after a voice note alone: %q", got)
	}
	if tr.name != "memo.m4a" || len(tr.got) != 3000 {
		t.Errorf("the transcriber was sent %q, %d bytes", tr.name, len(tr.got))
	}
	if id, err := s.PageID(t.Context(), "Sam"); err != nil || id == 0 {
		t.Errorf("the person the words name has no page: %v", err)
	}
	// Text and words: the text first, the words after a blank line, one entry.
	tr.words = "  Then coffee. "
	must(c.DoFiles(t.Context(), capture, map[string]string{"text": "Rain all morning.\n", "mood": "4"}, map[string]string{"audio": note}))
	want := "Walked to the market with [[Sam]].\n\nRain all morning.\n\nThen coffee."
	if got := dayBody(t, s, voiceDay); got != want {
		t.Errorf("the day page after text and a voice note:\n%q\nwant\n%q", got, want)
	}
	if n := filesCount(t, s); n != 0 {
		t.Errorf("%d files rows: a voice note is never kept as a file", n)
	}
	if props(e)["day"] != voiceDay {
		t.Errorf("capture answers with %v, want the day page", props(e)["day"])
	}
	// A multipart form with the file input left empty is an ordinary capture: the transcriber is not called.
	calls := tr.calls
	must(c.DoFiles(t.Context(), capture, map[string]string{"text": "Typed."}, nil))
	if tr.calls != calls || !strings.HasSuffix(dayBody(t, s, voiceDay), "\n\nTyped.") {
		t.Errorf("a form with no note: %d calls, body %q", tr.calls-calls, dayBody(t, s, voiceDay))
	}
}

func TestCaptureRefusesAVoiceNoteItCannotTranscribe(t *testing.T) {
	for _, tc := range []struct {
		name   string
		tr     api.Transcriber
		status int
		code   string
	}{
		{"no transcriber", nil, http.StatusServiceUnavailable, "no_transcriber"},
		{"the transcriber fails", &fakeTranscriber{err: errors.New("the transcriber answered 500 Internal Server Error: busy")}, http.StatusBadGateway, "transcriber"},
		{"no words", &fakeTranscriber{words: " \n"}, http.StatusUnprocessableEntity, "invalid"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _, s := voiceSetup(t, tc.tr)
			capture := find(must(c.Get(t.Context(), "/")), "capture")
			_, err := c.DoFiles(t.Context(), capture, map[string]string{"text": "Typed beside it."}, map[string]string{"audio": audioFile(t, "memo.ogg", 100)})
			var ce *client.Error
			if !errors.As(err, &ce) || ce.Status != tc.status || props(ce.Entity)["code"] != tc.code {
				t.Fatalf("refused with %v, want %d %s", err, tc.status, tc.code)
			}
			if got := dayBody(t, s, voiceDay); got != "" {
				t.Errorf("a refused capture wrote %q: the text beside the note is not kept alone", got)
			}
		})
	}
}

// voicePost is a browser's capture form: multipart, Accept text/html, with a back path and a note of size bytes.
func voicePost(t *testing.T, h http.Handler, text string, size int) *httptest.ResponseRecorder {
	t.Helper()
	var b bytes.Buffer
	mw := multipart.NewWriter(&b)
	mw.WriteField("back", "/days/"+voiceDay)
	mw.WriteField("text", text)
	if size > 0 {
		part, err := mw.CreateFormFile("audio", "note.webm")
		if err != nil {
			t.Fatal(err)
		}
		part.Write(bytes.Repeat([]byte("a"), size))
	} else {
		mw.CreateFormFile("audio", "") // the file input left empty
	}
	mw.Close()
	req := httptest.NewRequest("POST", "/days/"+voiceDay+"/capture", &b)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Accept", "text/html")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestTheBrowserCaptureBoxTakesAVoiceNote(t *testing.T) {
	tr := &fakeTranscriber{words: "Spoken."}
	_, h, s := voiceSetup(t, tr)
	for _, path := range []string{"/", "/days/" + voiceDay} {
		body := browse(t, h, path)
		for _, want := range []string{`data-action="capture" enctype="multipart/form-data"`, `type="file" name="audio"`, `accept="audio/*"`} {
			if !strings.Contains(body, want) {
				t.Errorf("%s does not show %s", path, want)
			}
		}
	}
	// Post, then redirect back, as any browser form.
	if rec := voicePost(t, h, "Typed.", 50); rec.Code != http.StatusSeeOther || !strings.HasPrefix(rec.Header().Get("Location"), "/days/"+voiceDay) {
		t.Errorf("the browser form is answered %d, Location %q", rec.Code, rec.Header().Get("Location"))
	}
	if got := dayBody(t, s, voiceDay); got != "Typed.\n\nSpoken." {
		t.Errorf("the day page: %q", got)
	}
	// A note too large is refused before the transcriber sees it.
	calls := tr.calls
	if rec := voicePost(t, h, "", 25<<20+1); rec.Code != http.StatusRequestEntityTooLarge || tr.calls != calls {
		t.Errorf("a note over 25 MiB: %d, %d transcriber calls", rec.Code, tr.calls-calls)
	}
	// Nothing to capture comes back on the page the form came from, with the refusal and the form.
	rec := voicePost(t, h, "", 0)
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "nothing to capture") || !strings.Contains(rec.Body.String(), `data-action="capture"`) {
		t.Errorf("an empty capture from a browser: %d %.200s", rec.Code, rec.Body.String())
	}
}
