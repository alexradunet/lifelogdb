package speech

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fake is an endpoint of the protocol on synthetic words: it records what it was sent.
type fake struct {
	t                        *testing.T
	status                   int
	answer                   string
	model, name, auth, bytes string
}

func (f *fake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || r.URL.Path != "/v1/audio/transcriptions" {
		f.t.Errorf("the request went to %s %s", r.Method, r.URL.Path)
	}
	f.auth = r.Header.Get("Authorization")
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		f.t.Errorf("not a multipart form: %v", err)
		return
	}
	f.model = r.FormValue("model")
	file, head, err := r.FormFile("file")
	if err != nil {
		f.t.Errorf("no file part: %v", err)
		return
	}
	defer file.Close()
	b, _ := io.ReadAll(file)
	f.name, f.bytes = head.Filename, string(b)
	w.WriteHeader(f.status)
	io.WriteString(w, f.answer)
}

func TestTranscribeSendsTheRecordingAndReadsTheWords(t *testing.T) {
	f := &fake{t: t, status: 200, answer: `{"text": "  Walked to the market with Sam. \n"}`}
	srv := httptest.NewServer(f)
	defer srv.Close()
	tr := &Transcriber{URL: srv.URL + "/v1", Name: "synthetic-whisper", Key: "synthetic-key"}
	words, err := tr.Transcribe(t.Context(), `C:\notes\memo 1.m4a`, strings.NewReader("RIFF synthetic"))
	if err != nil {
		t.Fatal(err)
	}
	if words != "Walked to the market with Sam." {
		t.Errorf("the words: %q", words)
	}
	if f.model != "synthetic-whisper" || f.auth != "Bearer synthetic-key" || f.name != "memo 1.m4a" || f.bytes != "RIFF synthetic" {
		t.Errorf("the request: model %q auth %q name %q bytes %q", f.model, f.auth, f.name, f.bytes)
	}
}

func TestTranscribeReportsARefusalAndNotJSON(t *testing.T) {
	for _, tc := range []struct {
		status       int
		answer, want string
	}{
		{400, "{\"error\": \"no audio\"}\nsecond line", `400 Bad Request: {"error": "no audio"}`},
		{200, "<html>", "not transcription JSON"},
	} {
		f := &fake{t: t, status: tc.status, answer: tc.answer}
		srv := httptest.NewServer(f)
		tr := &Transcriber{URL: srv.URL + "/v1/", Name: "synthetic-whisper"}
		_, err := tr.Transcribe(t.Context(), "", strings.NewReader("x"))
		srv.Close()
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("status %d: %v, want %q", tc.status, err, tc.want)
		}
		if f.name != "voice-note" || f.auth != "" {
			t.Errorf("status %d: name %q auth %q", tc.status, f.name, f.auth)
		}
	}
}

func TestFromEnvNeedsTheURLAndTheModel(t *testing.T) {
	t.Setenv("LIFELOG_TRANSCRIBE_URL", "http://127.0.0.1:1/v1/")
	t.Setenv("LIFELOG_TRANSCRIBE_MODEL", "")
	t.Setenv("LIFELOG_TRANSCRIBE_KEY", "k")
	if FromEnv() != nil {
		t.Error("a URL alone configures a transcriber")
	}
	t.Setenv("LIFELOG_TRANSCRIBE_MODEL", "whisper")
	tr := FromEnv()
	if tr == nil || tr.URL != "http://127.0.0.1:1/v1" || tr.Name != "whisper" || tr.Key != "k" {
		t.Errorf("FromEnv: %+v", tr)
	}
}
