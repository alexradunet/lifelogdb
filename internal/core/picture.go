package core

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
)

// A page's picture is the first live file with a preview that its body embeds (NamedRow.Picture). SetPicture and
// SetPictureOf make a file that picture: they put its embed first in the body. The file is kept as the owner
// picked it, with no day and no place, so a picture of a person is not put in a day page.

// SetPicture keeps a photo as a file and makes it the page's picture. day names a new file when f has no title:
// "<page> <day>.jpg", with a number when that title is taken.
func (s *Store) SetPicture(ctx context.Context, source string, id int64, day string, f FileIn) (k Kept, err error) {
	err = s.Do(ctx, source, func(t *Tx) (e error) { k, e = t.SetPicture(id, day, f); return })
	if err != nil {
		k = Kept{}
	}
	return
}

// SetPictureOf makes a kept file, named by its title, the page's picture.
func (s *Store) SetPictureOf(ctx context.Context, source string, id int64, title string) (r Sync, err error) {
	err = s.Do(ctx, source, func(t *Tx) (e error) { r, e = t.SetPictureOf(id, title); return })
	return
}

// SetPicture is Store.SetPicture in t.
func (t *Tx) SetPicture(id int64, day string, f FileIn) (Kept, error) {
	page, err := t.livePage(id)
	if err != nil {
		return Kept{}, err
	}
	if f.Preview == nil {
		return Kept{}, Invalid("a picture: send a JPEG, PNG or GIF, or a preview of it")
	}
	f.UnknownCaptureDay, f.Day, f.Taken, f.At, f.HasGPS = true, "", "", "", false
	if f.Title == "" {
		if f.Title, err = t.freePictureTitle(page, day, f.MIME); err != nil {
			return Kept{}, err
		}
	}
	k, err := t.AddFile(f)
	if err != nil {
		return Kept{}, err
	}
	var title string
	if err := t.tx.QueryRowContext(t.ctx, `SELECT n.title FROM entities e JOIN entity_names n ON n.entity_id = e.id AND n.name_key = e.preferred_name_key WHERE e.id = ?`, k.ID).Scan(&title); err != nil {
		return Kept{}, err
	}
	sync, err := t.showFirst(id, k.ID, title)
	if sync != nil {
		k.Sync = sync
	}
	return k, err
}

// SetPictureOf is Store.SetPictureOf in t.
func (t *Tx) SetPictureOf(id int64, title string) (Sync, error) {
	if _, err := t.livePage(id); err != nil {
		return Sync{}, err
	}
	p, err := t.lookup(title)
	if err != nil {
		return Sync{}, err
	}
	var preview bool
	if p != nil && p.Type == "file" && !p.Deleted {
		if err := t.tx.QueryRowContext(t.ctx, `SELECT preview IS NOT NULL FROM files WHERE id = ?`, p.ID).Scan(&preview); err != nil {
			return Sync{}, err
		}
	}
	if !preview {
		return Sync{}, Invalid("%q is not a kept picture: keep it first (add-file)", title)
	}
	sync, err := t.showFirst(id, p.ID, p.Title)
	if sync == nil {
		return Sync{}, err
	}
	return *sync, err
}

// livePage is the title of a live page: a missing page is 404, a deleted one 409.
func (t *Tx) livePage(id int64) (string, error) {
	var title string
	var deleted bool
	err := t.tx.QueryRowContext(t.ctx, `SELECT n.title, e.deleted_at IS NOT NULL FROM entities e JOIN entity_names n ON n.entity_id = e.id AND n.name_key = e.preferred_name_key WHERE e.id = ?`, id).
		Scan(&title, &deleted)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return "", NotFound("no page %d", id)
	case err != nil:
		return "", err
	case deleted:
		return "", Conflict("page %d is deleted: revive it first", id)
	}
	return title, nil
}

// freePictureTitle is "<page> <day><ext>", or "<page> <day> 2<ext>" and on when a page has that name.
func (t *Tx) freePictureTitle(page, day, mime string) (string, error) {
	ext := map[string]string{"image/jpeg": ".jpg", "image/png": ".png", "image/gif": ".gif", "image/heic": ".heic", "image/webp": ".webp"}[NormalMIME(mime)]
	for n := 1; ; n++ {
		title := page + " " + day
		if n > 1 {
			title += " " + strconv.Itoa(n)
		}
		title += ext
		p, err := t.lookup(title)
		if err != nil || p == nil {
			return title, err
		}
	}
}

// showFirst puts the embed of a file first in a page's body, unless the file is the page's picture already.
func (t *Tx) showFirst(id, file int64, title string) (*Sync, error) {
	var body string
	if err := t.tx.QueryRowContext(t.ctx, `SELECT body FROM entities WHERE id = ?`, id).Scan(&body); err != nil {
		return nil, err
	}
	current, err := firstPicture(t.ctx, t.tx, body)
	if err != nil || current == file {
		return nil, err
	}
	if err := automaticEmbedTitle(title); err != nil {
		return nil, err
	}
	mark := "![[" + title + "]]"
	if body != "" {
		mark += "\n\n"
	}
	sync, err := t.SetBody(id, mark+body)
	return &sync, err
}
