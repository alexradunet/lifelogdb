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
	in, dry, err := fileForm(r)
	if err != nil {
		return nil, err
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

// fileForm reads an add-file request. Multipart (a browser, the CLI): the original is streamed through SHA-256 and
// never stored; a picture the writer reads is held, up to maxPicture, to make the preview from. Url-encoded or JSON
// (an agent that hashed the file itself): sha256 and mime, and no picture.
func fileForm(r *http.Request) (core.FileIn, bool, error) {
	bad := func(msg string) (core.FileIn, bool, error) {
		return core.FileIn{}, false, &core.Error{Status: 422, Msg: msg}
	}
	if ct, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); ct != "multipart/form-data" {
		v, err := form(r)
		if err != nil {
			return core.FileIn{}, false, err
		}
		if err := required(v, "title", "sha256", "mime"); err != nil {
			return core.FileIn{}, false, err
		}
		r, err := radius(v.Get("radius"))
		return core.FileIn{Title: v.Get("title"), SHA256: v.Get("sha256"), MIME: v.Get("mime"), Body: v.Get("body"), Day: v.Get("day"),
			At: v.Get("at"), Radius: r}, v.Get("dry_run") == "1", err
	}
	defer r.Body.Close()
	mr, err := r.MultipartReader()
	if err != nil {
		return bad("the form: " + err.Error())
	}
	vals := map[string]string{}
	// Only accepted, bounded transcript text is retained for browser error recovery.
	r.PostForm = make(url.Values)
	var sum, typ string
	var picture, given []byte
	var meta photo.Meta
	sent := false
	parts, textBytes := 0, 0
	seen := map[string]bool{}
	overflow := func(msg string) (core.FileIn, bool, error) { return core.FileIn{}, false, requestLimit(msg) }
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
			sent = true
			sum, typ, picture, meta, err = readOriginal(part)
		case name == "preview":
			given, err = io.ReadAll(io.LimitReader(part, maxPicture+1))
			if err == nil && len(given) > maxPicture {
				return overflow("the picture sent is larger than 64 MiB")
			}
		default:
			limited := io.LimitReader(part, int64(min(maxField, maxMultipartText-textBytes)+1))
			var n int64
			if key := fileTextField(name); key != "" {
				var b []byte
				b, err = io.ReadAll(limited)
				n = int64(len(b))
				if err == nil && n <= maxField && textBytes+len(b) <= maxMultipartText {
					vals[key] = string(b)
					if key == "body" {
						r.PostForm.Set("body", vals[key])
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
	in := core.FileIn{Title: vals["title"], SHA256: vals["sha256"], MIME: vals["mime"], Body: vals["body"], Day: vals["day"], At: vals["at"],
		Taken: meta.Day(), Lat: meta.Lat, Lon: meta.Lon, HasGPS: meta.HasGPS}
	if in.Radius, err = radius(vals["radius"]); err != nil {
		return core.FileIn{}, false, err
	}
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
	return in, vals["dry_run"] == "1", nil
}

// fileTextField projects multipart names onto the fields retained by fileForm.
func fileTextField(name string) string {
	switch name {
	case "title":
		return "title"
	case "sha256":
		return "sha256"
	case "mime":
		return "mime"
	case "body":
		return "body"
	case "day":
		return "day"
	case "at":
		return "at"
	case "radius":
		return "radius"
	case "dry_run":
		return "dry_run"
	default:
		return ""
	}
}

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
	typ = core.MimeOf(part.FileName(), head)
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

// radius is the radius field: whole metres, "" for the default.
func radius(s string) (int, error) {
	if s == "" {
		return 0, nil
	}
	r, err := strconv.Atoi(s)
	if err != nil {
		return 0, &core.Error{Status: 422, Msg: "radius " + strconv.Quote(s) + " is not a whole number of metres"}
	}
	return r, nil
}

// locate gives a place its point (docs/cookbook/place-of-a-photo.md): the owner's answer, or a correction.
func (h *server) locate(r *http.Request, src string) (*Entity, error) {
	id, err := idOf(r)
	if err != nil {
		return nil, err
	}
	v, err := form(r)
	if err != nil {
		return nil, err
	}
	if err := required(v, "lat", "lon", "radius_m"); err != nil {
		return nil, err
	}
	var p core.Point
	for name, to := range map[string]*float64{"lat": &p.Lat, "lon": &p.Lon} {
		f, err := strconv.ParseFloat(v.Get(name), 64)
		if err != nil {
			return nil, &core.Error{Status: 422, Msg: name + " " + strconv.Quote(v.Get(name)) + " is not a number"}
		}
		*to = f
	}
	if p.RadiusM, err = radius(v.Get("radius_m")); err != nil {
		return nil, err
	}
	p.LinkDays = v.Get("link_days") != "0"
	if err := h.s.Locate(r.Context(), src, id, p); err != nil {
		return nil, err
	}
	return h.pageEntity(r.Context(), id)
}
