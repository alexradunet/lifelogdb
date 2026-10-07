package importer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"lifelog/internal/core"
	"lifelog/internal/photo"
	"lifelog/internal/preview"
)

const selectedPhotoFile = "selected-photo.md"

// PhotoChoices are owner-reviewed at the existing stamp boundary, not permission flags.
type PhotoChoices struct {
	Capture string `json:"capture"`
	GPS     string `json:"gps"`
	Offset  string `json:"offset,omitempty"`
	Day     string `json:"day,omitempty"`
	At      string `json:"at,omitempty"`
	Radius  int    `json:"radius,omitempty"`
}
type SelectedPhoto struct {
	Source         string        `json:"source"`
	Version        int           `json:"version"`
	File           string        `json:"file"`
	Sidecar        string        `json:"sidecar,omitempty"`
	OriginalSHA256 string        `json:"original_sha256"`
	SidecarSHA256  string        `json:"sidecar_sha256,omitempty"`
	Title          string        `json:"title"`
	Body           string        `json:"body,omitempty"`
	Choices        PhotoChoices  `json:"choices"`
	Evidence       PhotoEvidence `json:"evidence"`
}
type PhotoEvidence struct {
	EXIFLocal      string         `json:"exif_local,omitempty"`
	SourceCapture  string         `json:"source_claimed_capture_utc,omitempty"`
	SourceCreation string         `json:"source_creation_claim_unestablished_meaning,omitempty"`
	SidecarTitle   string         `json:"sidecar_title,omitempty"`
	Association    string         `json:"association"`
	Comparison     string         `json:"capture_comparison"`
	GPSComparison  string         `json:"gps_comparison"`
	EXIFGPS        bool           `json:"exif_gps"`
	SidecarGPS     bool           `json:"sidecar_gps"`
	EXIFPosition   *core.Position `json:"exif_position,omitempty"`
	SourcePosition *core.Position `json:"source_position,omitempty"`
	Excluded       []string       `json:"excluded,omitempty"`
}
type sidePhoto struct {
	title, capture, creation string
	lat, lon                 float64
	gps                      bool
}

func sourceEpoch(v json.RawMessage) (string, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(v, &obj); err != nil {
		return "", refuse("invalid selected timestamp")
	}
	var raw string
	if err := json.Unmarshal(obj["timestamp"], &raw); err != nil {
		return "", refuse("selected timestamp must be integral seconds text")
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || strconv.FormatInt(n, 10) != raw {
		return "", refuse("unsupported integral epoch")
	}
	t := time.Unix(n, 0).UTC()
	if t.Year() < 0 || t.Year() > 9999 {
		return "", refuse("epoch outside supported calendar")
	}
	return t.Format("2006-01-02T15:04:05.000Z"), nil
}
func decodeSidePhoto(data []byte) (sidePhoto, error) {
	var out sidePhoto
	if len(data) > maxProfileBytes || !utf8.Valid(data) || validateSourceJSON(data) != nil {
		return out, refuse("unsupported bounded selected sidecar")
	}
	obj, err := decodeObject(data)
	if err != nil {
		return out, refuse("selected sidecar must be an object")
	}
	for k, v := range obj {
		switch k {
		case "title":
			if json.Unmarshal(v, &out.title) != nil {
				return out, refuse("invalid sidecar title")
			}
		case "photoTakenTime":
			out.capture, err = sourceEpoch(v)
		case "creationTime":
			out.creation, err = sourceEpoch(v)
		case "geoDataExif", "geoData":
			var fields map[string]json.RawMessage
			if json.Unmarshal(v, &fields) != nil || fields["latitude"] == nil || fields["longitude"] == nil || string(fields["latitude"]) == "null" || string(fields["longitude"]) == "null" {
				return out, refuse("incomplete selected position")
			}
			var g struct {
				Latitude  float64 `json:"latitude"`
				Longitude float64 `json:"longitude"`
				Altitude  float64 `json:"altitude"`
			}
			if json.Unmarshal(v, &g) != nil || math.IsNaN(g.Latitude) || math.IsNaN(g.Longitude) || math.Abs(g.Latitude) > 90 || math.Abs(g.Longitude) > 180 {
				return out, refuse("invalid sidecar position")
			}
			if g.Latitude != 0 || g.Longitude != 0 {
				if out.gps && (g.Latitude != out.lat || g.Longitude != out.lon) {
					return out, refuse("sidecar position claims conflict")
				}
				out.lat, out.lon, out.gps = g.Latitude, g.Longitude, true
			}
		case "description", "url", "imageViews", "googlePhotosOrigin", "photoLastModifiedTime", "date", "people", "favorited", "archived", "trashed":
		default:
			return out, refuse("unsupported selected sidecar field")
		}
		if err != nil {
			return out, err
		}
	}
	return out, nil
}
func (w *Workspace) selectedBytes(ctx context.Context, file string, limit int64) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(w.Source)
	if err != nil {
		return nil, refuse("selected source unavailable")
	}
	defer root.Close()
	path, err := w.sourcePath(root, file)
	if err != nil {
		return nil, refuse("selected source is not confined")
	}
	f, err := root.Open(path)
	if err != nil {
		return nil, refuse("selected source unavailable")
	}
	data, e := io.ReadAll(io.LimitReader(f, limit+1))
	closeErr := f.Close()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if e != nil || closeErr != nil || int64(len(data)) > limit {
		return nil, refuse("selected source exceeds byte limit or cannot be read")
	}
	return data, nil
}
func digestBytes(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
func (w *Workspace) deriveSelectedPhoto(ctx context.Context, p *SelectedPhoto) (core.FileIn, error) {
	var in core.FileIn
	if offset := p.Choices.Offset; offset != "" {
		if len(offset) != 6 || (offset[0] != '+' && offset[0] != '-') || offset[3] != ':' || offset == "-00:00" {
			return in, refuse("invalid supplied capture offset")
		}
		hours, e := strconv.Atoi(offset[1:3])
		minutes, e2 := strconv.Atoi(offset[4:6])
		if e != nil || e2 != nil || hours < 0 || hours > 23 || minutes < 0 || minutes > 59 || !sourceDigits.MatchString(offset[1:3]) || !sourceDigits.MatchString(offset[4:6]) {
			return in, refuse("invalid supplied capture offset")
		}
	}
	if p.Version != 1 {
		return in, refuse("unsupported selected photo version")
	}
	data, err := w.selectedBytes(ctx, p.File, 64<<20)
	if err != nil {
		return in, err
	}
	mime := core.MimeOf(p.File, data)
	if !strings.HasPrefix(mime, "image/") {
		return in, refuse("selected source is not a photo")
	}
	meta := photo.Read(data[:min(len(data), maxProfileBytes)])
	var side sidePhoto
	sideHash := ""
	if p.Sidecar != "" {
		if p.Sidecar == p.File {
			return in, refuse("original and sidecar must be distinct")
		}
		raw, e := w.selectedBytes(ctx, p.Sidecar, maxProfileBytes)
		if e != nil {
			return in, e
		}
		side, e = decodeSidePhoto(raw)
		if e != nil {
			return in, e
		}
		sideHash = digestBytes(raw)
	}
	evidence := PhotoEvidence{EXIFLocal: meta.Taken, SourceCapture: side.capture, SourceCreation: side.creation, SidecarTitle: side.title, Association: "explicitly selected pair; title is not identity proof", Comparison: "unresolved", GPSComparison: "no competing position claims", EXIFGPS: meta.HasGPS, SidecarGPS: side.gps, Excluded: []string{"URLs and unselected summaries are not followed or stored"}}
	if meta.HasGPS {
		evidence.EXIFPosition = &core.Position{Lat: meta.Lat, Lon: meta.Lon}
	}
	if side.gps {
		evidence.SourcePosition = &core.Position{Lat: side.lat, Lon: side.lon}
	}
	if p.Sidecar == "" {
		evidence.Association = "explicitly selected original; no sidecar"
	}
	exifUTC := ""
	if meta.Taken != "" && p.Choices.Offset != "" {
		at, _, _, e := normalizeSourceClock(strings.Replace(meta.Taken, " ", "T", 1) + p.Choices.Offset)
		if e != nil {
			return in, refuse("invalid supplied capture offset")
		}
		exifUTC = at
	}
	if exifUTC != "" && side.capture != "" {
		evidence.Comparison = "comparable disagreement"
		if exifUTC == side.capture {
			evidence.Comparison = "comparable agreement"
		}
	}
	if meta.HasGPS && side.gps {
		evidence.GPSComparison = "disagreement"
		if meta.Lat == side.lat && meta.Lon == side.lon {
			evidence.GPSComparison = "exact agreement"
		}
	}
	p.OriginalSHA256 = digestBytes(data)
	p.SidecarSHA256 = sideHash
	p.Evidence = evidence
	in = core.FileIn{Title: p.Title, Body: p.Body, SHA256: p.OriginalSHA256, MIME: mime, UnknownCaptureDay: true, At: p.Choices.At, Radius: p.Choices.Radius}
	switch p.Choices.Capture {
	case "none":
	case "exif":
		if meta.Day() == "" || !core.IsDay(meta.Day()) {
			return in, refuse("EXIF local capture day unavailable")
		}
		in.Day = meta.Day()
	case "sidecar":
		if side.capture == "" {
			return in, refuse("source capture claim unavailable")
		}
		if p.Choices.Offset != "" {
			at, err := time.Parse("2006-01-02T15:04:05.000Z", side.capture)
			if err != nil {
				return in, err
			}
			_, _, offset, e := normalizeSourceClock("2000-01-01T00:00:00" + p.Choices.Offset)
			if e != nil {
				return in, refuse("invalid supplied offset")
			}
			hours, _ := strconv.Atoi(offset[1:3])
			mins, _ := strconv.Atoi(offset[4:6])
			seconds := (hours*60 + mins) * 60
			if offset[0] == '-' {
				seconds = -seconds
			}
			local := at.Add(time.Duration(seconds) * time.Second)
			if local.Year() < 0 || local.Year() > 9999 {
				return in, refuse("capture-local day outside supported calendar")
			}
			in.Day = local.Format(time.DateOnly)
		}
	case "owner":
		if p.Choices.Day == "" {
			return in, refuse("owner day attribution required")
		}
	default:
		return in, refuse("explicit reviewed capture choice required")
	}
	if p.Choices.Day != "" {
		if !core.IsDay(p.Choices.Day) {
			return in, refuse("invalid owner day attribution")
		}
		in.Day = p.Choices.Day
	}
	in.UnknownCaptureDay = in.Day == ""
	switch p.Choices.GPS {
	case "none":
	case "exif":
		if !meta.HasGPS {
			return in, refuse("EXIF position unavailable")
		}
		in.HasGPS, in.Lat, in.Lon = true, meta.Lat, meta.Lon
	case "sidecar":
		if !side.gps {
			return in, refuse("sidecar position unavailable")
		}
		in.HasGPS, in.Lat, in.Lon = true, side.lat, side.lon
	default:
		return in, refuse("explicit reviewed position choice required")
	}
	if in.UnknownCaptureDay && in.At != "" {
		return in, refuse("explicit place requires capture-local day")
	}
	if preview.Readable(mime) {
		in.Preview, err = preview.Make(data)
		if err != nil {
			return in, refuse("selected photo preview cannot be prepared")
		}
	}
	return in, nil
}
func (w *Workspace) DraftSelectedPhoto(ctx context.Context, file, sidecar, title, body string, choices PhotoChoices) (*SelectedPhoto, error) {
	w.selectionMu.RLock()
	defer w.selectionMu.RUnlock()
	w.mu.Lock()
	defer w.mu.Unlock()
	rules, err := w.snapshotRules()
	if err != nil {
		return nil, err
	}
	p := &SelectedPhoto{Source: rules.Source, Version: 1, File: file, Sidecar: sidecar, Title: title, Body: body, Choices: choices}
	if _, err := w.deriveSelectedPhoto(ctx, p); err != nil {
		return nil, err
	}
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return nil, err
	}
	if err = writeAtomic(w.file(selectedPhotoFile), append([]byte("status: draft\n"), append(data, 10)...)); err != nil {
		return nil, err
	}
	return p, nil
}

func (w *Workspace) readSelectedPhoto(ctx context.Context, approved bool) (*SelectedPhoto, core.FileIn, error) {
	body, err := w.artifactBody(selectedPhotoFile, approved)
	if err != nil {
		return nil, core.FileIn{}, err
	}
	return w.parseSelectedPhoto(ctx, body)
}
func (w *Workspace) parseSelectedPhoto(ctx context.Context, body string) (*SelectedPhoto, core.FileIn, error) {
	if len(body) > maxProfileBytes || !utf8.ValidString(body) || validateSourceJSON([]byte(body)) != nil {
		return nil, core.FileIn{}, refuse("unsupported selected artifact")
	}
	var p SelectedPhoto
	dec := json.NewDecoder(strings.NewReader(body))
	dec.DisallowUnknownFields()
	if dec.Decode(&p) != nil {
		return nil, core.FileIn{}, refuse("unsupported selected artifact")
	}
	rules, err := w.snapshotRules()
	if err != nil {
		return nil, core.FileIn{}, err
	}
	if p.Source != rules.Source {
		return nil, core.FileIn{}, refuse("selected namespace changed since review")
	}
	derived := p
	in, err := w.deriveSelectedPhoto(ctx, &derived)
	if err != nil {
		return nil, in, err
	}
	if !reflect.DeepEqual(p, derived) {
		return nil, in, refuse("selected original, sidecar, or interpretation changed since review")
	}
	return &p, in, nil
}
func (w *Workspace) SelectedPhoto(ctx context.Context, s *core.Store, dry bool) (core.Kept, error) {
	w.selectionMu.RLock()
	defer w.selectionMu.RUnlock()
	return w.selectedPhoto(ctx, s, dry, nil)
}
func (w *Workspace) selectedPhoto(ctx context.Context, s *core.Store, dry bool, afterCommit func() error) (core.Kept, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	p, in, err := w.readSelectedPhoto(ctx, true)
	if err != nil {
		return core.Kept{}, err
	}
	if err = w.admitSource(p.File); err != nil {
		return core.Kept{}, err
	}
	state, e := w.ledgerState(p.File)
	if e != nil {
		return core.Kept{}, e
	}
	if state == "x" {
		if _, e = w.readBoundedWorkspace(selectedBindingName(p), maxProfileBytes); e != nil {
			return core.Kept{}, refuse("done selection is missing its immutable reservation")
		}
	}
	if !dry {
		if _, err = keepSelectedPhoto(ctx, s, p.Source, in, true); err != nil {
			return core.Kept{}, err
		}
	}
	if err = w.bindSelectedPhoto(p, !dry); err != nil {
		return core.Kept{}, err
	}
	kept, err := keepSelectedPhoto(ctx, s, p.Source, in, dry)
	if err != nil || dry {
		return kept, err
	}
	if afterCommit != nil {
		if err = afterCommit(); err != nil {
			return kept, err
		}
	}
	if err = w.completeSelectedPhoto(p); err != nil {
		return kept, err
	}
	err = w.markLocked(p.File, func(l *Line) error {
		l.State = "x"
		l.Note = "selected photo; fingerprint-bound owner choices"
		return nil
	})
	return kept, err
}
func keepSelectedPhoto(ctx context.Context, s *core.Store, source string, in core.FileIn, dry bool) (core.Kept, error) {
	in.RequireCaptureDayAgreement = true
	if dry {
		return s.TryAddFile(ctx, source, in)
	}
	return s.AddFile(ctx, source, in)
}

func (w *Workspace) DraftSelectedPhotoJSON(ctx context.Context, file, sidecar, title, body, choicesRaw string) (*SelectedPhoto, error) {
	if len(choicesRaw) > 65536 || !utf8.ValidString(choicesRaw) || validateSourceJSON([]byte(choicesRaw)) != nil {
		return nil, refuse("invalid lossless photo choices")
	}
	var choices PhotoChoices
	dec := json.NewDecoder(strings.NewReader(choicesRaw))
	dec.DisallowUnknownFields()
	if dec.Decode(&choices) != nil {
		return nil, refuse("unsupported photo choices")
	}
	return w.DraftSelectedPhoto(ctx, file, sidecar, title, body, choices)
}

func selectedBindingName(p *SelectedPhoto) string {
	return ".selected-binding-" + p.OriginalSHA256 + ".json"
}
func selectedBindingBytes(p *SelectedPhoto) ([]byte, error) {
	copy := *p
	copy.Evidence = PhotoEvidence{}
	return json.Marshal(copy)
}
func (w *Workspace) bindSelectedPhoto(p *SelectedPhoto, publish bool) error {
	data, err := selectedBindingBytes(p)
	if err != nil {
		return err
	}
	entries, e := os.ReadDir(w.Dir)
	if e != nil {
		return e
	}
	if len(entries) > 10000 {
		return refuse("workspace receipt limit exceeded")
	}
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), ".selected-binding-") || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		raw, e := w.readBoundedWorkspace(entry.Name(), maxProfileBytes)
		if e != nil {
			return e
		}
		if validateSourceJSON(raw) != nil {
			return refuse("corrupt selected reservation")
		}
		var have SelectedPhoto
		dec := json.NewDecoder(strings.NewReader(string(raw)))
		dec.DisallowUnknownFields()
		if dec.Decode(&have) != nil || entry.Name() != selectedBindingName(&have) {
			return refuse("corrupt selected reservation identity")
		}
		if have.File == p.File || have.OriginalSHA256 == p.OriginalSHA256 {
			old, e := selectedBindingBytes(&have)
			if e != nil {
				return e
			}
			if string(old) != string(data) {
				return refuse("workspace selected pair binding is immutable")
			}
		}
	}
	raw, err := w.readBoundedWorkspace(selectedBindingName(p), maxProfileBytes)
	if err == nil {
		if string(raw) != string(data) {
			return refuse("selected source namespace, pair and choices are immutable")
		}
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if publish {
		return writeAtomic(w.file(selectedBindingName(p)), data)
	}
	return nil
}
func (w *Workspace) completeSelectedPhoto(p *SelectedPhoto) error {
	data, err := selectedBindingBytes(p)
	if err != nil {
		return err
	}
	return writeAtomic(w.file(selectedBindingName(p)+".complete"), []byte(bodyHash(string(data))))
}
func (w *Workspace) appliedSelectedPhotos(ctx context.Context) ([]*SelectedPhoto, error) {
	rules, err := w.snapshotRules()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(w.Dir)
	if err != nil {
		return nil, err
	}
	if len(entries) > 10000 {
		return nil, refuse("workspace receipt limit exceeded")
	}
	var out []*SelectedPhoto
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), ".selected-binding-") || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		raw, err := w.readBoundedWorkspace(e.Name(), maxProfileBytes)
		if err != nil {
			return nil, err
		}
		if !utf8.Valid(raw) || validateSourceJSON(raw) != nil {
			return nil, refuse("corrupt selected receipt")
		}
		var p SelectedPhoto
		dec := json.NewDecoder(strings.NewReader(string(raw)))
		dec.DisallowUnknownFields()
		if dec.Decode(&p) != nil || e.Name() != selectedBindingName(&p) || p.Source != rules.Source {
			return nil, refuse("selected receipt namespace or identity mismatch")
		}
		marker, e := w.readBoundedWorkspace(e.Name()+".complete", 64)
		if errors.Is(e, os.ErrNotExist) {
			continue
		}
		if e != nil {
			return nil, e
		}
		if string(marker) != bodyHash(string(raw)) {
			return nil, refuse("corrupt selected completion evidence")
		}
		out = append(out, &p)
	}
	return out, nil
}

func (w *Workspace) SelectedPhotoReview(ctx context.Context) (*SelectedPhoto, error) {
	w.selectionMu.RLock()
	defer w.selectionMu.RUnlock()
	w.mu.Lock()
	defer w.mu.Unlock()
	p, _, err := w.readSelectedPhoto(ctx, false)
	return p, err
}

// verifySelectedPhoto checks required persisted identity without publishing recovery evidence.
// Owner tombstones remain valid on the same store, but this profile cannot transport them.
func (w *Workspace) verifySelectedPhoto(ctx context.Context, s *core.Store, p *SelectedPhoto, allowDeleted bool) (core.FileIn, error) {
	if err := w.admitSource(p.File); err != nil {
		return core.FileIn{}, err
	}
	copy := *p
	in, err := w.deriveSelectedPhoto(ctx, &copy)
	if err != nil {
		return in, err
	}
	if copy.OriginalSHA256 != p.OriginalSHA256 || copy.SidecarSHA256 != p.SidecarSHA256 {
		return in, refuse("selected receipt source changed")
	}
	kept, err := keepSelectedPhoto(ctx, s, p.Source, in, true)
	if err != nil {
		return in, err
	}
	if !kept.Existing {
		return in, refuse("required stored selected original is missing")
	}
	if kept.Deleted && !allowDeleted {
		return in, refuse("selected trial owner tombstone cannot be transported by this replay profile")
	}
	return in, nil
}
