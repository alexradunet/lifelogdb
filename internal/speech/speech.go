// Package speech turns a voice note into words through an endpoint of the OpenAI audio-transcriptions protocol
// (docs/plans/109-a-voice-note.md). The words go into the journal; the recording is never stored (D9).
package speech

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path"
	"strings"
)

// Transcriber is an endpoint of the OpenAI audio-transcriptions protocol: a base URL that ends in /v1, a model id
// and an optional key. whisper.cpp's server, Speaches, LM Studio and the hosted providers speak it.
type Transcriber struct {
	URL, Name, Key string
	HTTP           *http.Client // nil: http.DefaultClient
}

// FromEnv is the transcriber of LIFELOG_TRANSCRIBE_URL, LIFELOG_TRANSCRIBE_MODEL and LIFELOG_TRANSCRIBE_KEY; nil
// when the first two are not both set, and then a capture with a voice note is refused.
func FromEnv() *Transcriber {
	url, name := os.Getenv("LIFELOG_TRANSCRIBE_URL"), os.Getenv("LIFELOG_TRANSCRIBE_MODEL")
	if url == "" || name == "" {
		return nil
	}
	return &Transcriber{URL: strings.TrimRight(url, "/"), Name: name, Key: os.Getenv("LIFELOG_TRANSCRIBE_KEY")}
}

// maxAnswer bounds what is read of one answer: the words of a voice note, never a whole page.
const maxAnswer = 1 << 20

// Transcribe sends the recording and returns its words, trimmed. name is the recording's file name: its extension
// tells the endpoint the format. The recording is read whole into the request, so the caller bounds it.
func (t *Transcriber) Transcribe(ctx context.Context, name string, audio io.Reader) (string, error) {
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	if err := mw.WriteField("model", t.Name); err != nil {
		return "", err
	}
	if err := mw.WriteField("response_format", "json"); err != nil {
		return "", err
	}
	name = path.Base(strings.ReplaceAll(name, "\\", "/"))
	if name == "." || name == "/" || name == "" {
		name = "voice-note"
	}
	part, err := mw.CreateFormFile("file", name)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(part, audio); err != nil {
		return "", err
	}
	if err := mw.Close(); err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(t.URL, "/")+"/audio/transcriptions", &body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if t.Key != "" {
		req.Header.Set("Authorization", "Bearer "+t.Key)
	}
	hc := t.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	res, err := hc.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, maxAnswer+1))
	if err != nil {
		return "", err
	}
	if len(raw) > maxAnswer {
		return "", errors.New("the transcriber's answer is larger than 1 MiB")
	}
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("the transcriber answered %s: %s", res.Status, firstLine(string(raw)))
	}
	var out struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("the transcriber's answer is not transcription JSON: %w", err)
	}
	return strings.TrimSpace(out.Text), nil
}

// firstLine is the start of an error body: enough to say what went wrong, never a whole page.
func firstLine(s string) string {
	s, _, _ = strings.Cut(strings.TrimSpace(s), "\n")
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	return s
}
