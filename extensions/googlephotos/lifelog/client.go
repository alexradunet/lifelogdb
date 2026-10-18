// Package lifelog is a client of the lifelog HTTP API (API.md). It reads Siren JSON and writes through add-file.
package lifelog

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"strings"
)

// Source is the provenance of every write of the extension.
const Source = "import:google-photos"

const (
	maxJSON   = 8 << 20 // the largest answer read from lifelog
	maxTitles = 5       // titles tried for one day: "<day>.jpg", "<day> 2.jpg" ... "<day> 5.jpg"
)

// Client talks to one lifelog server.
type Client struct {
	Base  string       // for example http://127.0.0.1:7777
	Token string       // the bearer token; "" sends none
	HTTP  *http.Client // with a Timeout
}

// APIError is an error entity of lifelog.
type APIError struct {
	Status  int
	Code    string
	Message string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("lifelog answered %d %s: %s", e.Status, e.Code, e.Message)
}

// MonthsWithoutCover gives, for each month (YYYY-MM), the days that show no picture. A day with no page is one.
func (c *Client) MonthsWithoutCover(ctx context.Context, months []string) (map[string]bool, error) {
	out := map[string]bool{}
	for _, m := range months {
		var e struct {
			Properties struct {
				Days []struct {
					Day     string `json:"day"`
					Picture string `json:"picture"`
				} `json:"days"`
			} `json:"properties"`
		}
		if err := c.get(ctx, "/months/"+url.PathEscape(m), &e); err != nil {
			return nil, fmt.Errorf("read month %s: %w", m, err)
		}
		for _, d := range e.Properties.Days {
			if d.Picture == "" {
				out[d.Day] = true
			}
		}
	}
	return out, nil
}

// DayHasCover reads the day now: has its page a cover?
func (c *Client) DayHasCover(ctx context.Context, day string) (bool, error) {
	var e struct {
		Properties struct {
			Cover *struct {
				Title string `json:"title"`
			} `json:"cover"`
		} `json:"properties"`
	}
	if err := c.get(ctx, "/days/"+url.PathEscape(day), &e); err != nil {
		return false, fmt.Errorf("read day %s: %w", day, err)
	}
	return e.Properties.Cover != nil, nil
}

// AddCover keeps the original with the day (add-file). The title is "<day>.<ext>". After a 409 "exists" (another file
// holds the title) it tries "<day> 2.<ext>" and so on, up to a small bound. preview may be nil. It gives the title kept.
// The same original sent again writes nothing: lifelog keeps one file for one hash.
func (c *Client) AddCover(ctx context.Context, day, mimeType string, original, preview []byte) (string, error) {
	ext := Ext(mimeType)
	for n := 1; n <= maxTitles; n++ {
		title := day + "." + ext
		if n > 1 {
			title = fmt.Sprintf("%s %d.%s", day, n, ext)
		}
		err := c.addFile(ctx, day, title, mimeType, original, preview)
		var ae *APIError
		switch {
		case err == nil:
			return title, nil
		case errors.As(err, &ae) && ae.Status == http.StatusConflict && ae.Code == "exists":
			continue
		default:
			return "", fmt.Errorf("add file %q: %w", title, err)
		}
	}
	return "", fmt.Errorf("add file for %s: the titles %s.%s to %s %d.%s are all in use", day, day, ext, day, maxTitles, ext)
}

// Ext is the file extension for a media type.
func Ext(mimeType string) string {
	switch strings.ToLower(mimeType) {
	case "image/jpeg", "image/jpg":
		return "jpg"
	case "image/png":
		return "png"
	case "image/gif":
		return "gif"
	case "image/heic":
		return "heic"
	case "image/heif":
		return "heif"
	case "image/webp":
		return "webp"
	case "image/avif":
		return "avif"
	case "image/tiff":
		return "tiff"
	case "image/bmp":
		return "bmp"
	}
	return "jpg"
}

func (c *Client) addFile(ctx context.Context, day, title, mimeType string, original, preview []byte) error {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for _, f := range [][2]string{{"day", day}, {"title", title}} {
		if err := w.WriteField(f[0], f[1]); err != nil {
			return err
		}
	}
	if err := filePart(w, "original", title, mimeType, original); err != nil {
		return err
	}
	if preview != nil {
		if err := filePart(w, "preview", "preview.jpg", "image/jpeg", preview); err != nil {
			return err
		}
	}
	if err := w.Close(); err != nil {
		return err
	}
	req, err := c.request(ctx, http.MethodPost, "/files", &buf)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	return c.do(req, nil)
}

func filePart(w *multipart.Writer, field, name, mimeType string, b []byte) error {
	h := textproto.MIMEHeader{}
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name=%q; filename=%q`, field, name))
	h.Set("Content-Type", mimeType)
	p, err := w.CreatePart(h)
	if err != nil {
		return err
	}
	_, err = p.Write(b)
	return err
}

func (c *Client) get(ctx context.Context, path string, out any) error {
	req, err := c.request(ctx, http.MethodGet, path, nil)
	if err != nil {
		return err
	}
	return c.do(req, out)
}

func (c *Client) request(ctx context.Context, method, path string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.Base, "/")+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.siren+json")
	req.Header.Set("Lifelog-Source", Source)
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	return req, nil
}

// do sends the request. A status of 300 or more is an *APIError, read from the error entity.
func (c *Client) do(req *http.Request, out any) error {
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxJSON+1))
	if err != nil {
		return err
	}
	if len(b) > maxJSON {
		return fmt.Errorf("answer is larger than %d bytes", maxJSON)
	}
	if resp.StatusCode >= 300 {
		ae := &APIError{Status: resp.StatusCode, Code: http.StatusText(resp.StatusCode)}
		var e struct {
			Properties struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"properties"`
		}
		if json.Unmarshal(b, &e) == nil {
			ae.Code, ae.Message = e.Properties.Code, e.Properties.Message
		}
		return ae
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(b, out); err != nil {
		return fmt.Errorf("read answer: %w", err)
	}
	return nil
}
