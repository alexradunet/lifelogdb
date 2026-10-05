package core

import (
	"context"
	"database/sql"
	"errors"
	"mime"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"

	"lifelog/internal/preview"
	"lifelog/internal/text"
)

// FileIn is a file to keep (docs/cookbook/keep-a-file.md, D9): the hash and type of the original, which is never
// stored; the title and text of its page; its preview, a JPEG preview.Make made (nil for none). Its day is Day when
// the caller gives one, else Taken, the day its metadata says it was made, else today. Its position (HasGPS) is
// matched to a place and never stored; At names the place instead, which takes the position as its point if it has
// none, with Radius (cookbook/place-of-a-photo.md).
type FileIn struct {
	Title    string
	SHA256   string
	MIME     string
	Body     string
	Day      string
	Taken    string
	Preview  []byte
	Lat, Lon float64
	HasGPS   bool
	At       string
	Radius   int
}

// Kept is what keeping a file did.
type Kept struct {
	ID           int64     `json:"id,omitempty"`            // none in a dry run that would create the page
	Existing     bool      `json:"existing,omitempty"`      // the original was kept already: that page, nothing else written
	Deleted      bool      `json:"deleted,omitempty"`       // ...and it is tombstoned, so not even a preview was added
	PreviewAdded bool      `json:"preview_added,omitempty"` // a file kept without a preview got this one
	Promoted     bool      `json:"promoted,omitempty"`      // a plain page of the title (a ghost an embed made) became the file
	Sync         *Sync     `json:"sync,omitempty"`          // the links its text names; none when nothing was written
	DaySync      *Sync     `json:"day_sync,omitempty"`      // the links synchronized when appending to its day
	Day          string    `json:"day,omitempty"`           // its own day: the caller's, or the one its metadata gives
	Place        string    `json:"place,omitempty"`         // the place its position is in, or the one named for it
	Linked       bool      `json:"linked,omitempty"`        // its day has an at link to that place
	PointSet     bool      `json:"point_set,omitempty"`     // the place named took its position as its point
	Unmatched    *Position `json:"unmatched,omitempty"`     // its position, in no place's circle: name the place with at
	Embedded     bool      `json:"embedded,omitempty"`      // it was appended to its day's page
}

// Position is a photo's position, reported (never stored) when no place holds it.
type Position struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
	Map string  `json:"map"`
}

var (
	shaRE  = regexp.MustCompile(`^[0-9a-f]{64}$`)
	mimeRE = regexp.MustCompile(`^[a-z][a-z0-9.+-]*/[a-z0-9][a-z0-9.+-]*$`) // files_mime
)

// AddFile keeps a file: the page the original's hash names already, or a new file page, or a plain page of the title
// promoted to the file (docs/cookbook/keep-a-file.md); then its place and its day (docs/cookbook/place-of-a-photo.md).
func (t *Tx) AddFile(f FileIn) (k Kept, err error) {
	f.SHA256 = strings.ToLower(strings.TrimSpace(f.SHA256))
	if !shaRE.MatchString(f.SHA256) {
		return k, invalid("sha256 %q is not the 64 hex digits of a SHA-256", f.SHA256)
	}
	if f.MIME = NormalMIME(f.MIME); f.MIME == "" {
		return k, invalid("mime: a type/subtype such as audio/mp4 or image/heic")
	}
	if f.Preview != nil {
		if err := preview.Check(f.Preview); err != nil {
			return k, invalid("%v", err)
		}
	}
	for _, d := range []string{f.Day, f.Taken} {
		if d != "" && !IsDay(d) {
			return k, invalid("day %q is not YYYY-MM-DD", d)
		}
	}
	if f.Radius == 0 {
		f.Radius = DefaultRadius
	}
	if f.HasGPS {
		if err := (Point{f.Lat, f.Lon, f.Radius, true}).check(); err != nil {
			return k, err
		}
	}
	day, own := f.Day, true
	switch {
	case day != "":
	case f.Taken != "":
		day = f.Taken
	default:
		day, own = Today(), false
	}
	// 0) the original kept already: that page, nothing written but a missing preview, its place and its day
	var hasPreview bool
	var deleted sql.NullString
	var title string
	err = t.tx.QueryRow(`SELECT f.id, f.preview IS NOT NULL, e.deleted_at, p.title FROM files f
	                       JOIN entities e ON e.id = f.id JOIN pages p ON p.id = f.id WHERE f.sha256 = ?`, f.SHA256).
		Scan(&k.ID, &hasPreview, &deleted, &title)
	switch {
	case err == nil:
		k.Existing, k.Deleted = true, deleted.Valid
		if k.Deleted {
			return k, nil
		}
		if own && (hasPreview || f.Preview != nil) {
			if err := t.preflightEmbed(day, title); err != nil {
				return k, err
			}
		}
		if !hasPreview && f.Preview != nil {
			if _, err := t.tx.Exec(`UPDATE files SET preview = ? WHERE id = ? AND preview IS NULL`, f.Preview, k.ID); err != nil {
				return k, err
			}
			k.PreviewAdded = true
		}
		return k, t.placeAndDay(&k, f, title, day, own, hasPreview || k.PreviewAdded)
	case !errors.Is(err, sql.ErrNoRows):
		return k, err
	}
	// 1) the title: free, or a plain page to promote
	if !text.ValidTitle(f.Title) {
		return k, invalid("title %q is not a valid title (docs/contract/titles-and-wikilinks.md)", f.Title)
	}
	p, err := t.Lookup(f.Title)
	if err != nil {
		return k, err
	}
	title = f.Title
	if p != nil {
		title = p.Title
	}
	if own && f.Preview != nil {
		if err := t.preflightEmbed(day, title); err != nil {
			return k, err
		}
	}
	body := f.Body
	switch {
	case p == nil:
		if k.ID, _, err = t.insertPage("file", f.Title, text.TitleKey(f.Title), day, f.Body, ""); err != nil {
			return k, err
		}
		title = f.Title
	case p.Type == "page" && !p.DayPage && !p.Stub:
		if p.Body != "" && f.Body != "" && p.Body != f.Body {
			return k, conflict("%s already has text of its own: give the file another title (two texts are never merged)", p.Title)
		}
		res, err := t.tx.Exec(`UPDATE entities SET entity_type = 'file', deleted_at = NULL WHERE id = ? AND entity_type = 'page'`, p.ID)
		if err != nil {
			return k, err
		}
		if n, _ := res.RowsAffected(); n != 1 {
			return k, conflict("page %d could not become the file", p.ID)
		}
		if p.Body == "" && f.Body != "" {
			if _, err := t.tx.Exec(`UPDATE pages SET body = ? WHERE id = ?`, f.Body, p.ID); err != nil {
				return k, err
			}
		} else {
			body = p.Body
		}
		if _, err := t.tx.Exec(`UPDATE pages SET day = ? WHERE id = ? AND day IS NULL`, day, p.ID); err != nil {
			return k, err
		}
		k.ID, k.Promoted, title = p.ID, true, p.Title
	default:
		return k, &ExistsError{p.ID, p.Title}
	}
	// 2) the files row, then the links the text names
	if _, err := t.tx.Exec(`INSERT INTO files(id, sha256, mime, preview) VALUES (?, ?, ?, ?)`, k.ID, f.SHA256, f.MIME, f.Preview); err != nil {
		return k, err
	}
	sync, err := t.syncWikilinks(k.ID, body)
	if err != nil {
		return k, err
	}
	k.Sync = &sync
	// 3) its place and its day
	return k, t.placeAndDay(&k, f, title, day, own, f.Preview != nil)
}

// placeAndDay is cookbook/place-of-a-photo.md for a file kept: the place named (its point set from the position if it
// has none) or the place the position is in; then, when the file has a day of its own, that day's at link and the
// file shown in that day's page. No day of its own, no link: a photo links the day it was taken.
func (t *Tx) placeAndDay(k *Kept, f FileIn, title, day string, own, picture bool) error {
	if own {
		k.Day = day
	}
	var place int64
	link := false
	switch {
	case f.At != "":
		id, err := t.placeFor(f.At)
		if err != nil {
			return err
		}
		if f.HasGPS {
			if k.PointSet, err = t.pointIfNone(id, Point{f.Lat, f.Lon, f.Radius, true}); err != nil {
				return err
			}
		}
		var days sql.NullBool // a place with no point yet is linked: the owner named it
		if err := t.tx.QueryRow(`SELECT p.title, pl.link_days FROM pages p LEFT JOIN places pl ON pl.id = p.id WHERE p.id = ?`, id).
			Scan(&k.Place, &days); err != nil {
			return err
		}
		place, link = id, !days.Valid || days.Bool
	case f.HasGPS:
		m, err := t.MatchPlace(f.Lat, f.Lon)
		if err != nil {
			return err
		}
		if m == nil {
			k.Unmatched = &Position{round6(f.Lat), round6(f.Lon), MapLink(f.Lat, f.Lon)}
			break
		}
		place, link, k.Place = m.ID, m.LinkDays, m.Title
	}
	if !own || !(place != 0 && link || picture) {
		return nil
	}
	dayID, err := t.dayPage(day)
	if err != nil {
		return err
	}
	if place != 0 && link {
		if _, err := t.Link(dayID, place, "at", ""); err != nil {
			return err
		}
		k.Linked = true
	}
	if picture {
		k.Embedded, k.DaySync, err = t.embed(dayID, title)
	}
	return err
}

func (s *Store) AddFile(ctx context.Context, source string, f FileIn) (k Kept, err error) {
	err = s.Do(ctx, source, func(t *Tx) (e error) { k, e = t.AddFile(f); return })
	if err != nil {
		k = Kept{}
	}
	return
}

// File is what a file page has beyond its page.
type File struct {
	SHA256  string `json:"sha256"`
	MIME    string `json:"mime"`
	Preview bool   `json:"preview"`
}

// Preview is the picture of a file page by its id (tombstoned or not); nil when it has none.
func (s *Store) Preview(ctx context.Context, id int64) ([]byte, error) {
	return s.preview(ctx, `SELECT preview FROM files WHERE id = ?`, id)
}

// PreviewByTitle is the picture of the live file page a title names (an embed); nil when there is none.
func (s *Store) PreviewByTitle(ctx context.Context, title string) ([]byte, error) {
	return s.preview(ctx, `SELECT f.preview FROM files f JOIN pages p ON p.id = f.id
	                         JOIN entities e ON e.id = f.id AND e.deleted_at IS NULL WHERE p.title_key = ?`, text.TitleKey(title))
}

func (s *Store) preview(ctx context.Context, q string, arg any) ([]byte, error) {
	var b []byte
	err := s.DB.R.QueryRowContext(ctx, q, arg).Scan(&b)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return b, err
}

// NormalMIME is a type as files.mime keeps it: lowercase type/subtype, parameters dropped; "" when it is not one.
func NormalMIME(s string) string {
	t, _, err := mime.ParseMediaType(s)
	if err != nil || len(t) > 127 || !mimeRE.MatchString(t) {
		return ""
	}
	return t
}

// types names the files the owner keeps by their extension: fixed here, not the operating system's table, so every
// machine names a file alike.
var types = map[string]string{
	".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".png": "image/png", ".gif": "image/gif", ".webp": "image/webp",
	".heic": "image/heic", ".heif": "image/heif", ".avif": "image/avif", ".tif": "image/tiff", ".tiff": "image/tiff",
	".mp4": "video/mp4", ".m4v": "video/mp4", ".mov": "video/quicktime", ".webm": "video/webm", ".mkv": "video/x-matroska",
	".3gp": "video/3gpp", ".m4a": "audio/mp4", ".mp3": "audio/mpeg", ".wav": "audio/wav", ".ogg": "audio/ogg",
	".opus": "audio/ogg", ".flac": "audio/flac", ".aac": "audio/aac", ".amr": "audio/amr", ".pdf": "application/pdf",
	".txt": "text/plain", ".md": "text/markdown", ".html": "text/html", ".docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
}

// MimeOf names a file's type from its name, else from its first bytes (net/http's sniffing).
func MimeOf(name string, head []byte) string {
	if t, ok := types[strings.ToLower(filepath.Ext(name))]; ok {
		return t
	}
	if t := NormalMIME(http.DetectContentType(head)); t != "" {
		return t
	}
	return "application/octet-stream"
}

// TryAddFile is AddFile in a transaction that is rolled back: what keeping the file would do, writing nothing. A page
// it would create has no id.
func (s *Store) TryAddFile(ctx context.Context, source string, f FileIn) (k Kept, err error) {
	err = s.DryRun(ctx, source, func(t *Tx) (e error) { k, e = t.AddFile(f); return })
	if !k.Existing {
		k.ID = 0
	}
	return
}
