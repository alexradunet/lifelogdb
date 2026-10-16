package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"

	"lifelog/internal/text"
)

// A page's picture is the first live file with a preview that its body embeds (NamedRow.Picture). SetPicture and
// SetPictureOf make a file that picture: they put its embed first in the body. The file is kept as the owner
// picked it, with no day and no place, so a picture of a person is not put in a day page.

// firstPictures gives the picture of each body: the id of the first live file with a preview that the body embeds,
// else 0. One query serves all bodies. A title resolves through every name of a page, as GET /previews resolves it.
// The result has one id for each body, in the order of bodies. q is a *sql.DB or the *sql.Tx of a write.
func firstPictures(ctx context.Context, q interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, bodies []string) ([]int64, error) {
	keys, titles := []string{}, make([][]string, len(bodies))
	for i, body := range bodies {
		for _, title := range text.EmbeddedTitles(body) {
			k := text.TitleKey(title)
			titles[i] = append(titles[i], k)
			keys = append(keys, k)
		}
	}
	out := make([]int64, len(bodies))
	if len(keys) == 0 {
		return out, nil
	}
	arg, err := json.Marshal(keys)
	if err != nil {
		return nil, err
	}
	rows, err := q.QueryContext(ctx, `
		SELECT n.name_key, f.id FROM entity_names n
		  JOIN entities e ON e.id = n.entity_id AND e.deleted_at IS NULL
		  JOIN files f ON f.id = e.id AND f.preview IS NOT NULL
		 WHERE n.name_key IN (SELECT value FROM json_each(?))`, string(arg))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byKey := map[string]int64{}
	for rows.Next() {
		var k string
		var id int64
		if err := rows.Scan(&k, &id); err != nil {
			return nil, err
		}
		byKey[k] = id
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i, ks := range titles {
		for _, k := range ks {
			if id, ok := byKey[k]; ok {
				out[i] = id
				break
			}
		}
	}
	return out, nil
}

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
	f.UnknownCaptureDay, f.Day, f.Taken = true, "", ""
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
	current, err := firstPictures(t.ctx, t.tx, []string{body})
	if err != nil || current[0] == file {
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
