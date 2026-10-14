package api

// A voice note (docs/plans/109-a-voice-note.md): capture takes a recording beside its text, a transcriber the owner
// configures turns it into words, and the words go into the day page. The recording is never stored (D9).

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"

	"lifelog/internal/core"
)

// Transcriber turns a voice note into words; nil when none is configured, and then a capture with a note is refused.
type Transcriber interface {
	Transcribe(ctx context.Context, name string, audio io.Reader) (string, error)
}

// NoTranscriber is the refusal of a capture with a voice note when no transcriber is configured.
var NoTranscriber = &core.Error{Status: http.StatusServiceUnavailable, Code: "no_transcriber",
	Msg: "no transcriber is configured: set LIFELOG_TRANSCRIBE_URL and LIFELOG_TRANSCRIBE_MODEL where lifelog runs"}

// maxAudio bounds a voice note: the common cap of a transcription endpoint.
const maxAudio = 25 << 20

// voiceNote is the recording a capture carries: its bytes, held whole and bounded, and the name it was sent under,
// whose extension tells the transcriber the format.
type voiceNote struct {
	name string
	data []byte
}

// captureForm reads a capture request: the fields as form does, or a multipart form (a browser's capture box, the
// CLI's --audio, a phone's share sheet) whose audio part is the voice note. The text fields go to r.PostForm too: a
// refused browser form is drawn back from them, and back is read from them.
func captureForm(r *http.Request) (url.Values, *voiceNote, error) {
	if ct, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); ct != "multipart/form-data" {
		v, err := form(r)
		return v, nil, err
	}
	defer r.Body.Close()
	mr, err := r.MultipartReader()
	if err != nil {
		return nil, nil, core.Invalid("the form: %s", err)
	}
	v := url.Values{}
	r.PostForm = v
	var note *voiceNote
	parts, textBytes := 0, 0
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, nil, core.Invalid("the form: %s", err)
		}
		parts++
		if parts > maxParts {
			return nil, nil, requestLimit("multipart request has more than 64 parts")
		}
		name := part.FormName()
		switch {
		case name == "audio" && part.FileName() == "": // a file input left empty
			_, err = io.Copy(io.Discard, part)
		case name == "audio":
			if note != nil {
				part.Close()
				return nil, nil, requestLimit("duplicate audio part")
			}
			var b []byte
			b, err = io.ReadAll(io.LimitReader(part, maxAudio+1))
			if err == nil && len(b) > maxAudio {
				part.Close()
				return nil, nil, requestLimit("the voice note is larger than 25 MiB")
			}
			note = &voiceNote{name: part.FileName(), data: b}
		default:
			limited := io.LimitReader(part, int64(min(maxField, maxMultipartText-textBytes)+1))
			var n int
			if name == "text" || name == "mood" || name == "back" {
				var b []byte
				b, err = io.ReadAll(limited)
				n = len(b)
				if err == nil && n <= maxField && textBytes+n <= maxMultipartText {
					v.Set(name, string(b))
				}
			} else { // an unknown value consumes the budget; neither its name nor its value is retained
				var c int64
				c, err = io.Copy(io.Discard, limited)
				n = int(c)
			}
			textBytes += n
			if err == nil && (n > maxField || textBytes > maxMultipartText) {
				part.Close()
				return nil, nil, requestLimit("multipart text is larger than 16 MiB")
			}
		}
		part.Close()
		if err != nil {
			return nil, nil, core.Invalid("the form: %s", err)
		}
	}
	return v, note, nil
}

// entryOf is the entry a capture writes: the text, then the words of the voice note after a blank line. The note is
// transcribed before the write transaction opens, so the network is never held inside it.
func (h *server) entryOf(ctx context.Context, text string, note *voiceNote) (string, error) {
	if note == nil {
		return text, nil
	}
	if h.transcriber == nil {
		return "", NoTranscriber
	}
	words, err := h.transcriber.Transcribe(ctx, note.name, bytes.NewReader(note.data))
	if err != nil {
		return "", transcriberError(err)
	}
	words = strings.TrimSpace(words)
	if words == "" {
		return "", core.Invalid("the voice note has no words")
	}
	if strings.TrimSpace(text) == "" {
		return words, nil
	}
	return strings.TrimRight(text, " \t\r\n") + "\n\n" + words, nil
}

// transcriberError is a failure of the transcriber as the API answers it: 502, except a context that ran out or was
// cancelled, which the API answers as it answers any other.
func transcriberError(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return &core.Error{Status: http.StatusBadGateway, Code: "transcriber", Msg: err.Error(), Err: err}
}
