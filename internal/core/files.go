package core

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"mime"
	"strings"

	"lifelog/internal/preview"
	"lifelog/internal/text"
)

// FileIn is a file to keep (docs/cookbook/keep-a-file.md, D9): the hash and type of the original, which is never
// stored; the title and text of its page; its preview, a JPEG preview.Make made (nil for none). Its day is Day when
// the caller gives one, else Taken, else today for ordinary files. Unknown-capture mode follows
// docs/cookbook/keep-a-file.md and withholds incidental place writes. Its position (HasGPS) is
// matched to a place and never stored; At names the place instead, which takes the position as its point if it has
// none, with Radius (cookbook/place-of-a-photo.md).
type FileIn struct {
	UnknownCaptureDay bool // Explicit selected-photo mode: no inferred attribution or incidental place writes.
	Title             string
	SHA256            string
	MIME              string
	Body              string
	Day               string
	Taken             string
	Preview           []byte
	Lat, Lon          float64
	HasGPS            bool
	At                string
	Radius            int
}

// Kept is what keeping a file did.
type Kept struct {
	StoredDay    string    `json:"stored_day,omitempty"`    // Existing attribution, not new capture evidence.
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

// AddFile keeps a file: the page the original's hash names already, or a new file page, or a plain page of the title
// promoted to the file (docs/cookbook/keep-a-file.md); then its place and its day (docs/cookbook/place-of-a-photo.md).
func (t *Tx) AddFile(f FileIn) (k Kept, err error) {
	// the hash and the type are normalized here and held by files_sha256 and files_mime; the days by entities_day
	f.SHA256 = strings.ToLower(strings.TrimSpace(f.SHA256))
	if f.MIME = NormalMIME(f.MIME); f.MIME == "" {
		return k, Invalid("mime: a type/subtype such as audio/mp4 or image/heic")
	}
	if f.Preview != nil {
		if err := preview.Check(f.Preview); err != nil {
			return k, Invalid("%v", err)
		}
	}
	if f.Radius == 0 {
		f.Radius = DefaultRadius
	}
	// a position is matched, never stored, so the places CHECKs never see it: the same bounds, and 0°, 0° is how
	// photo metadata says "no location" (places_lat, places_lon, places_not_null_island)
	if f.HasGPS && (f.Lat == 0 && f.Lon == 0 || math.IsNaN(f.Lat) || math.IsNaN(f.Lon) || f.Lat < -90 || f.Lat > 90 || f.Lon < -180 || f.Lon > 180) {
		return k, Invalid("a position is a latitude -90..90 and a longitude -180..180, and 0°, 0° is no position")
	}
	if f.UnknownCaptureDay && (f.Day != "" || f.Taken != "" || f.At != "") {
		return k, Invalid("unknown capture day requires no day attribution or selected place")
	}
	day, own := f.Day, true
	switch {
	case f.UnknownCaptureDay:
		own = false
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
	var storedDay sql.NullString
	err = t.tx.QueryRowContext(t.ctx, `SELECT f.id, f.preview IS NOT NULL, p.deleted_at, p_name.title,p.day FROM files f JOIN entities p ON p.id = f.id JOIN entity_names p_name ON p_name.entity_id = p.id AND p_name.name_key = p.preferred_name_key  WHERE f.sha256 = ?`, f.SHA256).
		Scan(&k.ID, &hasPreview, &deleted, &title, &storedDay)
	switch {
	case err == nil:
		k.Existing, k.Deleted = true, deleted.Valid
		if f.UnknownCaptureDay {
			k.StoredDay = storedDay.String
		}
		if k.Deleted {
			return k, nil
		}
		if own && (hasPreview || f.Preview != nil) {
			if err := t.preflightEmbed(day, title); err != nil {
				return k, err
			}
		}
		if !hasPreview && f.Preview != nil {
			if _, err := t.tx.ExecContext(t.ctx, `UPDATE files SET preview = ? WHERE id = ? AND preview IS NULL`, f.Preview, k.ID); err != nil {
				return k, err
			}
			k.PreviewAdded = true
		}
		return k, t.placeAndDay(&k, f, title, day, own, hasPreview || k.PreviewAdded)
	case !errors.Is(err, sql.ErrNoRows):
		return k, err
	}
	// 1) the title: free, or a plain page to promote
	if err := checkTitle("title", f.Title); err != nil {
		return k, err
	}
	p, err := t.lookup(f.Title)
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
		if k.ID, _, err = t.insertPage("file", f.Title, text.TitleKey(f.Title), nullIfEmpty(day), f.Body, ""); err != nil {
			return k, err
		}
		title = f.Title
	case p.Type == "page" && !p.DayPage:
		if p.Body != "" && f.Body != "" && p.Body != f.Body {
			return k, Conflict("%s already has text of its own: give the file another title (two texts are never merged)", p.Title)
		}
		res, err := t.tx.ExecContext(t.ctx, `UPDATE entities SET entity_type = 'file', deleted_at = NULL WHERE id = ? AND entity_type = 'page'`, p.ID)
		if err != nil {
			return k, err
		}
		if n, _ := res.RowsAffected(); n != 1 {
			return k, Conflict("page %d could not become the file", p.ID)
		}
		if p.Body == "" && f.Body != "" {
			if _, err := t.tx.ExecContext(t.ctx, `UPDATE entities SET body = ? WHERE id = ?`, f.Body, p.ID); err != nil {
				return k, err
			}
		} else {
			body = p.Body
		}
		if _, err := t.tx.ExecContext(t.ctx, `UPDATE entities SET day = ? WHERE id = ? AND day IS NULL`, nullIfEmpty(day), p.ID); err != nil {
			return k, err
		}
		k.ID, k.Promoted, title = p.ID, true, p.Title
		if f.UnknownCaptureDay {
			if err := t.tx.QueryRowContext(t.ctx, "SELECT coalesce(day,'') FROM entities WHERE id=?", p.ID).Scan(&k.StoredDay); err != nil {
				return k, err
			}
		}
	default:
		return k, &ExistsError{p.ID, p.Title}
	}
	// 2) the files row, then the links the text names
	if _, err := t.tx.ExecContext(t.ctx, `INSERT INTO files(id, sha256, mime, preview) VALUES (?, ?, ?, ?)`, k.ID, f.SHA256, f.MIME, f.Preview); err != nil {
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
	if f.UnknownCaptureDay {
		return nil
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
		if err := t.tx.QueryRowContext(t.ctx, `SELECT p_name.title, pl.link_days FROM entities p JOIN entity_names p_name ON p_name.entity_id = p.id AND p_name.name_key = p.preferred_name_key  LEFT JOIN places pl ON pl.id = p.id WHERE p.id = ?`, id).
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
	return s.preview(ctx, `SELECT f.preview FROM files f JOIN entities p ON p.id = f.id JOIN entity_names p_name ON p_name.entity_id = p.id AND p_name.name_key = p.preferred_name_key AND p.deleted_at IS NULL WHERE p.id = (SELECT entity_id FROM entity_names WHERE name_key = ?)`, text.TitleKey(title))
}

func (s *Store) preview(ctx context.Context, q string, arg any) ([]byte, error) {
	var b []byte
	err := s.DB.R.QueryRowContext(ctx, q, arg).Scan(&b)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return b, err
}

// NormalMIME is a type as files.mime keeps it: lowercase type/subtype, parameters dropped; "" when it does not parse.
// Its grammar is files_mime.
func NormalMIME(s string) string {
	t, _, err := mime.ParseMediaType(s)
	if err != nil {
		return ""
	}
	return t
}

// TryAddFile is AddFile in a transaction that is rolled back: what keeping the file would do, writing nothing. A page
// it would create has no id.
func (s *Store) TryAddFile(ctx context.Context, source string, f FileIn) (k Kept, err error) {
	err = s.dryRun(ctx, source, func(t *Tx) (e error) { k, e = t.AddFile(f); return })
	if !k.Existing {
		k.ID = 0
	}
	return
}

// embed shows a file in a day's page once: `![[title]]` appended after a blank line unless the page shows it
// already, then the page's links synced (cookbook/place-of-a-photo.md).
func (t *Tx) embed(dayID int64, title string) (bool, *Sync, error) {
	var body string
	if err := t.tx.QueryRowContext(t.ctx, `SELECT body FROM entities WHERE id = ?`, dayID).Scan(&body); err != nil {
		return false, nil, err
	}
	shown, err := t.hasEmbed(body, title)
	if err != nil {
		return false, nil, err
	}
	if shown {
		return false, nil, nil
	}
	body += func() string {
		if body == "" {
			return ""
		}
		return "\n\n"
	}() + "![[" + title + "]]"
	keys, _, _ := text.Targets(body, "")
	named := false
	for _, key := range keys {
		if key == text.TitleKey(title) {
			named = true
		}
	}
	if !text.HasEmbed(body, title) || !named {
		return false, nil, Invalid("automatic photo embed cannot render and link here; edit the day's Markdown before keeping the photo")
	}
	_, err = t.tx.ExecContext(t.ctx, `UPDATE entities SET body = ? WHERE id = ?`, body, dayID)
	if err != nil {
		return false, nil, err
	}

	sync, err := t.syncWikilinks(dayID, body)
	return true, &sync, err
}

func automaticEmbedTitle(title string) error {
	mark := "![[" + title + "]]"
	keys, _, _ := text.Targets(mark, "")
	if !text.HasEmbed(mark, title) || len(keys) != 1 || keys[0] != text.TitleKey(title) {
		return Invalid("cannot automatically embed %q: give a new file an explicit representable title; an existing file's handle is not renamed", title)
	}
	return nil
}

func (t *Tx) preflightEmbed(day, title string) error {
	var body string
	err := t.tx.QueryRowContext(t.ctx, `SELECT body FROM entities WHERE preferred_name_key = ?`, day).Scan(&body)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err == nil {
		shown, err := t.hasEmbed(body, title)
		if err != nil {
			return err
		}
		if shown {
			return nil
		}
	}
	return automaticEmbedTitle(title)
}

func (t *Tx) hasEmbed(body, title string) (bool, error) {
	owner, err := t.lookup(title)
	if err != nil {
		return false, err
	}
	for _, target := range text.EmbeddedTitles(body) {
		if owner == nil {
			if text.TitleKey(target) == text.TitleKey(title) {
				return true, nil
			}
			continue
		}
		p, err := t.lookup(target)
		if err != nil {
			return false, err
		}
		if p != nil && p.ID == owner.ID {
			return true, nil
		}
	}
	return false, nil
}
