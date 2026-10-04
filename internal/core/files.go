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
// stored; the title, text and day of its page; its preview, a JPEG preview.Make made (nil for none).
type FileIn struct {
	Title   string
	SHA256  string
	MIME    string
	Body    string
	Day     string
	Preview []byte
}

// Kept is what keeping a file did.
type Kept struct {
	ID           int64 `json:"id"`
	Existing     bool  `json:"existing,omitempty"`      // the original was kept already: that page, nothing else written
	Deleted      bool  `json:"deleted,omitempty"`       // ...and it is tombstoned, so not even a preview was added
	PreviewAdded bool  `json:"preview_added,omitempty"` // a file kept without a preview got this one
	Promoted     bool  `json:"promoted,omitempty"`      // a plain page of the title (a ghost an embed made) became the file
	Sync         *Sync `json:"sync,omitempty"`          // the links its text names; none when nothing was written
}

var (
	shaRE  = regexp.MustCompile(`^[0-9a-f]{64}$`)
	mimeRE = regexp.MustCompile(`^[a-z][a-z0-9.+-]*/[a-z0-9][a-z0-9.+-]*$`) // files_mime
)

// AddFile keeps a file: the page the original's hash names already, or a new file page, or a plain page of the title
// promoted to the file (docs/cookbook/keep-a-file.md).
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
	// 0) the original kept already: that page, and nothing else but a missing preview
	var hasPreview bool
	var deleted sql.NullString
	err = t.tx.QueryRow(`SELECT f.id, f.preview IS NOT NULL, e.deleted_at FROM files f JOIN entities e ON e.id = f.id
	                      WHERE f.sha256 = ?`, f.SHA256).Scan(&k.ID, &hasPreview, &deleted)
	switch {
	case err == nil:
		k.Existing, k.Deleted = true, deleted.Valid
		if !k.Deleted && !hasPreview && f.Preview != nil {
			if _, err := t.tx.Exec(`UPDATE files SET preview = ? WHERE id = ? AND preview IS NULL`, f.Preview, k.ID); err != nil {
				return k, err
			}
			k.PreviewAdded = true
		}
		return k, nil
	case !errors.Is(err, sql.ErrNoRows):
		return k, err
	}
	// 1) the title: free, or a plain page to promote
	if !text.ValidTitle(f.Title) {
		return k, invalid("title %q is not a valid title (docs/contract/titles-and-wikilinks.md)", f.Title)
	}
	switch {
	case f.Day == "":
		f.Day = Today()
	case !IsDay(f.Day):
		return k, invalid("day %q is not YYYY-MM-DD", f.Day)
	}
	p, err := t.Lookup(f.Title)
	if err != nil {
		return k, err
	}
	body := f.Body
	switch {
	case p == nil:
		if k.ID, _, err = t.insertPage("file", f.Title, text.TitleKey(f.Title), f.Day, f.Body, ""); err != nil {
			return k, err
		}
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
		k.ID, k.Promoted = p.ID, true
	default:
		return k, &ExistsError{p.ID, p.Title}
	}
	// 2) the files row, then the links the text names
	if _, err := t.tx.Exec(`INSERT INTO files(id, sha256, mime, preview) VALUES (?, ?, ?, ?)`, k.ID, f.SHA256, f.MIME, f.Preview); err != nil {
		return k, err
	}
	sync, err := t.syncWikilinks(k.ID, body)
	k.Sync = &sync
	return k, err
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
