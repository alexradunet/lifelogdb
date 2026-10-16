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
	"net/url"
	"slices"
	"strconv"
	"strings"

	"lifelog/internal/core"
	"lifelog/internal/photo"
	"lifelog/internal/preview"
)

const (
	maxPicture       = 64 << 20 // the largest original a picture is made from, and the largest picture sent
	maxParts         = 64
	maxMultipartText = 16 << 20
	maxField         = 16 << 20 // a text field: a long transcript
	maxMeta          = 1 << 20  // the head of an original read for its metadata
)

// addFile keeps a file (docs/cookbook/keep-a-file.md, D9) and answers with its page; Result says what was written.
func (h *server) addFile(r *http.Request, src string) (*Entity, error) {
	in, dry, sent, err := parseFileForm(r, "add-file")
	if err != nil {
		return nil, err
	}
	if !sent {
		return nil, core.Invalid("send the original, or its sha256 and mime")
	}
	if dry {
		k, err := h.s.TryAddFile(r.Context(), src, in)
		if err != nil {
			return nil, err
		}
		return &Entity{Class: []string{"result", "dry-run"}, Title: "Dry run: nothing written",
			Properties: map[string]any{"title": in.Title, "sha256": in.SHA256, "mime": in.MIME, "picture": in.Preview != nil},
			Result:     k, Links: []Link{link("index", "/", "Home")}}, nil
	}
	k, err := h.s.AddFile(r.Context(), src, in)
	if err != nil {
		return nil, err
	}
	return withResult(h.pageEntity(r.Context(), k.ID))(k)
}

// parseFileForm reads a request of the action name (add-file or set-picture): its fields, refused as fieldsOf refuses
// them, and sent, which says whether a file was sent. Multipart (a browser, the CLI): the original is streamed through
// SHA-256 and never stored; a picture the writer reads is held, up to maxPicture, to make the preview from. A multipart
// form with the file input left empty and no sha256 and no mime sends no file, and is not refused here: the caller
// decides. Url-encoded or JSON (an agent that hashed the file itself): sha256 and mime, and no picture.
func parseFileForm(r *http.Request, name string) (in core.FileIn, dry, sent bool, err error) {
	bad := func(msg string) (core.FileIn, bool, bool, error) {
		return core.FileIn{}, false, false, core.Invalid("%s", msg)
	}
	if ct, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); ct != "multipart/form-data" {
		v, err := fieldsOf(r, name)
		if err != nil {
			return core.FileIn{}, false, false, err
		}
		if v.Get("sha256") == "" || v.Get("mime") == "" {
			return bad("send the original, or its sha256 and mime")
		}
		dry, err := flag(v, "dry_run")
		return core.FileIn{UnknownCaptureDay: strings.HasPrefix(core.NormalMIME(v.Get("mime")), "image/") && v.Get("day") == "", Title: v.Get("title"), Category: v.Get("category"), SHA256: v.Get("sha256"), MIME: v.Get("mime"), Body: v.Get("body"), Day: v.Get("day")}, dry, true, err
	}
	defer r.Body.Close()
	mr, err := r.MultipartReader()
	if err != nil {
		return bad("the form: " + err.Error())
	}
	vals := url.Values{}
	// Only accepted, bounded text is retained: the transcript for browser error recovery, and the page to return to.
	r.PostForm = make(url.Values)
	var sum, typ string
	var picture, given []byte
	var meta photo.Meta
	original := false
	parts, textBytes := 0, 0
	seen := map[string]bool{}
	overflow := func(msg string) (core.FileIn, bool, bool, error) {
		return core.FileIn{}, false, false, requestLimit(msg)
	}
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return bad("the form: " + err.Error())
		}
		parts++
		if parts > maxParts {
			return overflow("multipart request has more than 64 parts")
		}
		name := part.FormName()
		if name == "original" || name == "preview" {
			if seen[name] {
				return overflow("duplicate " + name + " part")
			}
			seen[name] = true
		}
		switch {
		case (name == "original" || name == "preview") && part.FileName() == "": // a file input left empty
			_, err = io.Copy(io.Discard, part)
		case name == "original":
			original = true
			sum, typ, picture, meta, err = readOriginal(part)
		case name == "preview":
			given, err = io.ReadAll(io.LimitReader(part, maxPicture+1))
			if err == nil && len(given) > maxPicture {
				return overflow("the picture sent is larger than 64 MiB")
			}
		default:
			limited := io.LimitReader(part, int64(min(maxField, maxMultipartText-textBytes)+1))
			var n int64
			if slices.Contains(fileTextFields, name) {
				key := name
				var b []byte
				b, err = io.ReadAll(limited)
				n = int64(len(b))
				if err == nil && n <= maxField && textBytes+len(b) <= maxMultipartText {
					vals.Set(key, string(b))
					if key == "body" || key == "back" { // the browser's recovery and the page it returns to
						r.PostForm.Set(key, vals.Get(key))
					}
				}
			} else {
				// Unknown values still consume the budget, but neither names nor values are retained.
				n, err = io.Copy(io.Discard, limited)
			}
			textBytes += int(n)
			if err == nil && (n > maxField || textBytes > maxMultipartText) {
				return overflow("multipart text is larger than 16 MiB")
			}
		}
		part.Close()
		if err != nil {
			return bad("the form: " + err.Error())
		}
	}
	if err := required(vals, specOf(name)); err != nil {
		return core.FileIn{}, false, false, err
	}
	in = core.FileIn{Title: vals.Get("title"), Category: vals.Get("category"), SHA256: vals.Get("sha256"), MIME: vals.Get("mime"), Body: vals.Get("body"), Day: vals.Get("day"), Taken: meta.Day()}
	switch {
	case original && in.SHA256 != "" && !strings.EqualFold(strings.TrimSpace(in.SHA256), sum):
		return bad("the sha256 sent is not the original's (" + sum + ")")
	case original:
		in.SHA256 = sum
		if in.MIME == "" {
			in.MIME = typ
		}
	case in.SHA256 == "" && in.MIME == "": // no file sent: the dry_run field is not read
		return in, false, false, nil
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
	in.UnknownCaptureDay = strings.HasPrefix(core.NormalMIME(in.MIME), "image/") && in.Day == "" && in.Taken == ""
	dry, err = flagValue("dry_run", vals.Get("dry_run"))
	return in, dry, true, err
}

// fileTextFields are the multipart names parseFileForm retains as text; any other name is dropped unread.
var fileTextFields = []string{"title", "sha256", "mime", "body", "day", "category", "dry_run", "back"}

// readOriginal hashes the original as it streams, and keeps its first megabyte for its metadata (photo.Read: a
// JPEG's EXIF is in its first 64 KB, a HEIC's Exif item near its start); a picture the writer can read is held whole,
// unless it passes maxPicture (it is then kept without a picture, as a HEIC is).
func readOriginal(part *multipart.Part) (sum, typ string, picture []byte, meta photo.Meta, err error) {
	h := sha256.New()
	head := make([]byte, 512)
	n, err := io.ReadFull(part, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return "", "", nil, meta, err
	}
	head = head[:n]
	h.Write(head)
	typ = MimeOf(part.FileName(), head)
	held := &capped{max: maxPicture}
	first := &prefix{max: maxMeta}
	first.Write(head)
	w := io.MultiWriter(h, first)
	if preview.Readable(typ) {
		held.b.Write(head)
		w = io.MultiWriter(h, first, held)
	}
	if _, err := io.Copy(w, part); err != nil {
		return "", "", nil, meta, err
	}
	if preview.Readable(typ) && !held.over {
		picture = held.b.Bytes()
	}
	return hex.EncodeToString(h.Sum(nil)), typ, picture, photo.Read(first.b), nil
}

// prefix keeps the first max bytes written to it and ignores the rest.
type prefix struct {
	b   []byte
	max int
}

func (p *prefix) Write(b []byte) (int, error) {
	if room := p.max - len(p.b); room > 0 {
		p.b = append(p.b, b[:min(room, len(b))]...)
	}
	return len(b), nil
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
// A cache that is not empty is sent as Cache-Control.
func picture(get func(*http.Request) ([]byte, error), cache string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		b, err := get(r)
		if err == nil && b == nil {
			err = core.NotFound("this page keeps no picture")
		}
		if err != nil { // the error entity, as every other route answers
			writeError(w, r, err)
			return
		}
		if cache != "" {
			w.Header().Set("Cache-Control", cache)
		}
		w.Header().Set("Content-Type", "image/jpeg")
		w.Header().Set("Content-Length", strconv.Itoa(len(b)))
		w.Write(b)
	}
}

// setPicture makes a photo a page's picture (core.SetPicture): a new photo sent as original, or a kept file named by
// title. It answers with the page.
func (h *server) setPicture(r *http.Request, src string) (*Entity, error) {
	id, err := idOf(r)
	if err != nil {
		return nil, err
	}
	if ct, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); ct == "multipart/form-data" {
		in, _, sent, err := parseFileForm(r, "set-picture")
		if err != nil {
			return nil, err
		}
		if sent {
			k, err := h.s.SetPicture(r.Context(), src, id, h.localDay(), in)
			if err != nil {
				return nil, err
			}
			return withResult(h.pageEntity(r.Context(), id))(k)
		}
		return h.setPictureOf(r, src, id, in.Title)
	}
	v, err := fieldsOf(r, "set-picture")
	if err != nil {
		return nil, err
	}
	return h.setPictureOf(r, src, id, v.Get("title"))
}

func (h *server) setPictureOf(r *http.Request, src string, id int64, title string) (*Entity, error) {
	if title == "" {
		return nil, core.Invalid("title: a kept picture's title, or send a new photo as original")
	}
	sync, err := h.s.SetPictureOf(r.Context(), src, id, title)
	if err != nil {
		return nil, err
	}
	return withResult(h.pageEntity(r.Context(), id))(sync)
}
