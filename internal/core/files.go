package core

import (
	"context"
	"database/sql"
	"errors"
	"mime"
	"strings"

	"lifelog/internal/preview"
	"lifelog/internal/text"
)

// FileIn is a file to keep (docs/cookbook/keep-a-file.md, D9): the hash and type of the original, which is never
// stored; the title and text of its page; its preview, a JPEG preview.Make made (nil for none). Its day is Day when
// the caller gives one, else Taken, else today for ordinary files. Unknown-capture mode follows
// docs/cookbook/keep-a-file.md: a photo with no day of its own has no day, and goes in no day page. keep finds that
// mode for a photo (an image/* type); setPicture gives it to a file of any type (unknownCaptureDay). A photo's
// position is never read (D21). Category is the title of the category to file the page in ("" for none).
type FileIn struct {
	unknownCaptureDay bool // no day of its own, whatever the type: only setPicture sets it
	Title             string
	Category          string
	SHA256            string
	MIME              string
	Body              string
	Day               string
	Taken             string
	Preview           []byte
}

// Kept is what keeping a file did.
type Kept struct {
	StoredDay    string `json:"stored_day,omitempty"`    // Existing attribution, not new capture evidence.
	ID           int64  `json:"id,omitempty"`            // none in a dry run that would create the page
	Existing     bool   `json:"existing,omitempty"`      // the original was kept already: that page, nothing else written
	Deleted      bool   `json:"deleted,omitempty"`       // ...and it is tombstoned, so not even a preview was added
	PreviewAdded bool   `json:"preview_added,omitempty"` // a file kept without a preview got this one
	Promoted     bool   `json:"promoted,omitempty"`      // a plain page of the title (a ghost an embed made) became the file
	Sync         *Sync  `json:"sync,omitempty"`          // the links its text names; none when nothing was written
	DaySync      *Sync  `json:"day_sync,omitempty"`      // the links synchronized when appending to its day
	Day          string `json:"day,omitempty"`           // its own day: the caller's, or the one its metadata gives
	Embedded     bool   `json:"embedded,omitempty"`      // it was appended to its day's page
	Filed        *Filed `json:"filed,omitempty"`         // what filing it in the category named did; none when no category was named
}

// AddFile keeps a file (keep), then, when f names a category, files its page there in the same write, as FilePages
// does (docs/plans/117-one-browse-view.md, rule 6). The page the original's hash names already is filed too. A
// tombstoned page is not filed: for it nothing is written. A day as the category refuses the whole write, as fileNew
// says.
func (t *Tx) AddFile(f FileIn) (Kept, error) {
	k, err := t.keep(f)
	if err != nil || f.Category == "" || k.Deleted {
		return k, err
	}
	filed, err := t.filePages([]int64{k.ID}, f.Category, false)
	if err != nil {
		return k, err
	}
	k.Filed = &filed
	return k, nil
}

// keep keeps a file: the page the original's hash names already, or a new file page, or a plain page of the title
// promoted to the file (docs/cookbook/keep-a-file.md); then its own day (ownDay).
func (t *Tx) keep(f FileIn) (k Kept, err error) {
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
	if strings.HasPrefix(f.MIME, "image/") && f.Day == "" && f.Taken == "" { // a photo with no day: never the day it is kept
		f.unknownCaptureDay = true
	}
	day, own := f.Day, true
	switch {
	case f.unknownCaptureDay:
		own = false
	case day != "":
	case f.Taken != "":
		day = f.Taken
	default:
		day, own = t.today(), false
	}
	// 0) the original kept already: that page, nothing written but a missing preview and its embed in its own day
	var hasPreview bool
	var deleted sql.NullString
	var title string
	var storedDay sql.NullString
	err = t.tx.QueryRowContext(t.ctx, `SELECT f.id, f.preview IS NOT NULL, p.deleted_at, p_name.title,p.day FROM files f JOIN entities p ON p.id = f.id JOIN entity_names p_name ON p_name.entity_id = p.id AND p_name.name_key = p.preferred_name_key  WHERE f.sha256 = ?`, f.SHA256).
		Scan(&k.ID, &hasPreview, &deleted, &title, &storedDay)
	switch {
	case err == nil:
		k.Existing, k.Deleted = true, deleted.Valid
		if f.unknownCaptureDay {
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
		return k, t.ownDay(&k, f, title, day, own, hasPreview || k.PreviewAdded)
	case !errors.Is(err, sql.ErrNoRows):
		return k, err
	}
	// 1) the title: free, or a plain page to promote
	if err := checkTitle("title", f.Title); err != nil {
		return k, err
	}
	p, err := t.lookupWithBody(f.Title)
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
		if k.ID, err = t.insertPage("file", f.Title, text.TitleKey(f.Title), nullIfEmpty(day), f.Body); err != nil {
			return k, err
		}
		title = f.Title
	case p.Type == "page": // a day page is never a file: entities_day_page_plain refuses the update
		if p.Body != "" && f.Body != "" && p.Body != f.Body {
			return k, Conflict("%s already has text of its own: give the file another title (two texts are never merged)", p.Title)
		}
		if _, err := t.tx.ExecContext(t.ctx, `UPDATE entities SET entity_type = 'file', deleted_at = NULL WHERE id = ? AND entity_type = 'page'`, p.ID); err != nil {
			return k, err
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
		if f.unknownCaptureDay {
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
	// 3) its own day
	return k, t.ownDay(&k, f, title, day, own, f.Preview != nil)
}

// ownDay reports the own day of a file kept in Kept.Day, when the file has one. When the file also has a picture,
// the page of that day shows it (embed). A file with no day of its own goes in no day page.
func (t *Tx) ownDay(k *Kept, f FileIn, title, day string, own, picture bool) error {
	if own {
		k.Day = day
	}
	if f.unknownCaptureDay || !own || !picture {
		return nil
	}
	dayID, err := t.dayPage(day)
	if err != nil {
		return err
	}
	k.Embedded, k.DaySync, err = t.embed(dayID, title)
	return err
}

func (s *Store) AddFile(ctx context.Context, source string, f FileIn) (Kept, error) {
	return alone(s, ctx, source, func(t *Tx) (Kept, error) { return t.AddFile(f) })
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
	return s.preview(ctx, `SELECT f.preview FROM files f JOIN entities p ON p.id = f.id
		WHERE p.id = (SELECT entity_id FROM entity_names WHERE name_key = ?) AND p.deleted_at IS NULL`, text.TitleKey(title))
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
// it would create has no id, and neither has a category it would create. A refusal gives no result, as alone does.
func (s *Store) TryAddFile(ctx context.Context, source string, f FileIn) (Kept, error) {
	var k Kept
	if err := s.dryRun(ctx, source, func(t *Tx) (err error) { k, err = t.AddFile(f); return err }); err != nil {
		return Kept{}, err
	}
	if !k.Existing {
		k.ID = 0
	}
	if k.Filed != nil && k.Filed.Created {
		k.Filed.Category.ID = 0
	}
	return k, nil
}

// embed shows a file in a day's page once: `![[title]]` appended after a blank line unless the page shows it
// already, then the page's links synced.
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

// dayPageBody is the body of the page whose preferred key is the day: the journal day page of that day (D5). It
// finds the name by the unique index of entity_names.name_key: no index starts with entities.preferred_name_key.
const dayPageBody = `SELECT e.body FROM entity_names n JOIN entities e ON e.id = n.entity_id AND e.preferred_name_key = n.name_key WHERE n.name_key = ?`

func (t *Tx) preflightEmbed(day, title string) error {
	var body string
	err := t.tx.QueryRowContext(t.ctx, dayPageBody, day).Scan(&body)
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
