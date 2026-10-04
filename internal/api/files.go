package api

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"

	"lifelog/internal/core"
	"lifelog/internal/preview"
)

const (
	maxPicture = 64 << 20 // the largest original a picture is made from, and the largest picture sent
	maxField   = 16 << 20 // a text field: a long transcript
)

// addFile keeps a file (docs/cookbook/keep-a-file.md, D9) and answers with its page; Result says what was written.
func (h *server) addFile(r *http.Request, src string) (*Entity, error) {
	in, err := fileForm(r)
	if err != nil {
		return nil, err
	}
	k, err := h.s.AddFile(r.Context(), src, in)
	if err != nil {
		return nil, err
	}
	return withResult(h.pageEntity(r.Context(), k.ID))(k)
}

// fileForm reads an add-file request. Multipart (a browser, the CLI): the original is streamed through SHA-256 and
// never stored; a picture the writer reads is held, up to maxPicture, to make the preview from. Url-encoded or JSON
// (an agent that hashed the file itself): sha256 and mime, and no picture.
func fileForm(r *http.Request) (core.FileIn, error) {
	bad := func(msg string) (core.FileIn, error) { return core.FileIn{}, &core.Error{Status: 422, Msg: msg} }
	if ct, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); ct != "multipart/form-data" {
		v, err := form(r)
		if err != nil {
			return core.FileIn{}, err
		}
		if err := required(v, "title", "sha256", "mime"); err != nil {
			return core.FileIn{}, err
		}
		return core.FileIn{Title: v.Get("title"), SHA256: v.Get("sha256"), MIME: v.Get("mime"), Body: v.Get("body"), Day: v.Get("day")}, nil
	}
	mr, err := r.MultipartReader()
	if err != nil {
		return bad("the form: " + err.Error())
	}
	vals := map[string]string{}
	var sum, typ string
	var picture, given []byte
	sent := false
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return bad("the form: " + err.Error())
		}
		name := part.FormName()
		switch {
		case (name == "original" || name == "preview") && part.FileName() == "": // a file input left empty
			_, err = io.Copy(io.Discard, part)
		case name == "original":
			sent = true
			sum, typ, picture, err = readOriginal(part)
		case name == "preview":
			given, err = io.ReadAll(io.LimitReader(part, maxPicture+1))
			if err == nil && len(given) > maxPicture {
				return bad("the picture sent is larger than 64 MB")
			}
		default:
			var b []byte
			b, err = io.ReadAll(io.LimitReader(part, maxField+1))
			if err == nil && len(b) > maxField {
				return bad("field " + name + " is larger than 16 MB")
			}
			vals[name] = string(b)
		}
		part.Close()
		if err != nil {
			return bad("the form: " + err.Error())
		}
	}
	in := core.FileIn{Title: vals["title"], SHA256: vals["sha256"], MIME: vals["mime"], Body: vals["body"], Day: vals["day"]}
	switch {
	case sent && in.SHA256 != "" && !strings.EqualFold(strings.TrimSpace(in.SHA256), sum):
		return bad("the sha256 sent is not the original's (" + sum + ")")
	case sent:
		in.SHA256 = sum
		if in.MIME == "" {
			in.MIME = typ
		}
	case in.SHA256 == "" || in.MIME == "":
		return bad("send the original, or its sha256 and mime")
	}
	if given == nil {
		given = picture
	}
	if given != nil {
		p, err := preview.Make(given)
		if err != nil {
			return bad("the picture: " + err.Error())
		}
		in.Preview = p
	}
	return in, nil
}

// readOriginal hashes the original as it streams; a picture the writer can read is held too, unless it passes
// maxPicture (it is then kept without a picture, as a HEIC is).
func readOriginal(part *multipart.Part) (sum, typ string, picture []byte, err error) {
	h := sha256.New()
	head := make([]byte, 512)
	n, err := io.ReadFull(part, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return "", "", nil, err
	}
	head = head[:n]
	h.Write(head)
	typ = core.MimeOf(part.FileName(), head)
	held := &capped{max: maxPicture}
	var w io.Writer = h
	if preview.Readable(typ) {
		held.b.Write(head)
		w = io.MultiWriter(h, held)
	}
	if _, err := io.Copy(w, part); err != nil {
		return "", "", nil, err
	}
	if preview.Readable(typ) && !held.over {
		picture = held.b.Bytes()
	}
	return hex.EncodeToString(h.Sum(nil)), typ, picture, nil
}

// capped holds what is written to it until it passes max, then drops it and holds nothing more.
type capped struct {
	b    bytes.Buffer
	max  int
	over bool
}

func (c *capped) Write(p []byte) (int, error) {
	if !c.over {
		if c.b.Len()+len(p) > c.max {
			c.over, c.b = true, bytes.Buffer{}
		} else {
			c.b.Write(p)
		}
	}
	return len(p), nil
}

// picture serves the JPEG a file page keeps, 404 when there is none.
func picture(get func(*http.Request) ([]byte, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		b, err := get(r)
		var ce *core.Error
		switch {
		case errors.As(err, &ce):
			http.Error(w, ce.Msg, ce.Status)
			return
		case err != nil:
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		case b == nil:
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "image/jpeg")
		w.Header().Set("Content-Length", strconv.Itoa(len(b)))
		w.Write(b)
	}
}
