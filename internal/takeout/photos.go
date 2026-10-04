// Package takeout reads a Google Takeout export (docs/plans/032-takeout-photos.md): the Google Photos folder, every
// media file paired with its JSON sidecar. It reads files and never writes; what it reports about an export is counts
// and shapes, never a name, a date, a place or a caption.
package takeout

import (
	"encoding/json"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"lifelog/internal/photo"
)

// mediaExt are the extensions of the photos and videos Google Photos exports.
var mediaExt = map[string]bool{".jpg": true, ".jpeg": true, ".png": true, ".gif": true, ".webp": true, ".heic": true,
	".heif": true, ".avif": true, ".tif": true, ".tiff": true, ".bmp": true, ".dng": true, ".mp4": true, ".mov": true,
	".m4v": true, ".3gp": true, ".avi": true, ".mkv": true, ".webm": true, ".mpg": true, ".mts": true}

// IsMedia reports a photo or a video by its extension.
func IsMedia(name string) bool { return mediaExt[strings.ToLower(path.Ext(name))] }

// The ways a media file finds its sidecar, in the order they are tried.
const (
	FormName         = "name.json"
	FormSupplemental = "name.supplemental-metadata.json"
	FormTruncated    = "a truncated name"
	FormDuplicate    = "a duplicate's (n)"
	FormEdited       = "an edited copy's original"
	FormTitle        = "its title field"
	FormNone         = "none"
)

// Sidecar is what a Google Photos JSON sidecar says, as far as the photos step reads it.
type Sidecar struct {
	Title          string `json:"title"`
	Description    string `json:"description"`
	PhotoTakenTime *struct {
		Timestamp string `json:"timestamp"`
	} `json:"photoTakenTime"`
	GeoData     *Geo `json:"geoData"`
	GeoDataExif *Geo `json:"geoDataExif"`
	People      []struct {
		Name string `json:"name"`
	} `json:"people"`
}

// Geo is a sidecar's position; 0, 0 is how it says "none".
type Geo struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

// Known reports a position.
func (g *Geo) Known() bool { return g != nil && (g.Latitude != 0 || g.Longitude != 0) }

// Media is one media file of the export and the sidecar found for it.
type Media struct {
	Path    string // relative to the export's folder, with /
	Dir     string // its folder, relative
	Sidecar string // the sidecar's path, relative; "" when none
	Form    string // how it was found
}

// json is a JSON file of a folder: its name, its keys, and its sidecar when it is a photo's (it has photoTakenTime).
type jsonFile struct {
	name    string
	keys    []string
	sidecar *Sidecar
}

// Scan walks the export's folder: every media file with its sidecar, by folder and name; and every sidecar.
func Scan(root string) ([]Media, map[string]*Sidecar, map[string][]string, error) {
	dirs := map[string][]string{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p != root && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		dirs[path.Dir(rel)] = append(dirs[path.Dir(rel)], path.Base(rel))
		return nil
	})
	if err != nil {
		return nil, nil, nil, err
	}
	var media []Media
	sidecars := map[string]*Sidecar{}
	keys := map[string][]string{}
	names := make([]string, 0, len(dirs))
	for d := range dirs {
		names = append(names, d)
	}
	sort.Strings(names)
	for _, dir := range names {
		files := dirs[dir]
		sort.Strings(files)
		var jsons []jsonFile
		for _, f := range files {
			if strings.EqualFold(path.Ext(f), ".json") {
				jsons = append(jsons, readJSON(filepath.Join(root, filepath.FromSlash(path.Join(dir, f))), f))
			}
		}
		pair := pairer(jsons)
		for _, f := range files {
			if !IsMedia(f) {
				continue
			}
			m := Media{Path: path.Join(dir, f), Dir: dir, Form: FormNone}
			if j, form := pair(f); j != "" {
				m.Sidecar, m.Form = path.Join(dir, j), form
			}
			media = append(media, m)
		}
		for _, j := range jsons {
			p := path.Join(dir, j.name)
			keys[p] = j.keys
			if j.sidecar != nil {
				sidecars[p] = j.sidecar
			}
		}
	}
	return media, sidecars, keys, nil
}

func readJSON(p, name string) jsonFile {
	j := jsonFile{name: name}
	b, err := os.ReadFile(p)
	if err != nil {
		return j
	}
	var raw map[string]json.RawMessage
	if json.Unmarshal(b, &raw) != nil {
		return j
	}
	for k := range raw {
		j.keys = append(j.keys, k)
	}
	sort.Strings(j.keys)
	if _, ok := raw["photoTakenTime"]; ok {
		s := &Sidecar{}
		if json.Unmarshal(b, s) == nil {
			j.sidecar = s
		}
	}
	return j
}

var (
	dupRE    = regexp.MustCompile(`^(.*)\((\d+)\)(\.[^.]+)$`) // IMG_1(1).jpg
	editedRE = regexp.MustCompile(`^(.*)-[^-.]+(\.[^.]+)$`)   // IMG_1-edited.jpg, IMG_1-bearbeitet.jpg
)

const supplemental = ".supplemental-metadata"

// pairer finds a media file's sidecar among a folder's JSON files: by the names Google writes, then by the title the
// sidecar records. Names compare without case.
func pairer(jsons []jsonFile) func(media string) (string, string) {
	byName := map[string]string{}
	var lower []string // the names in order, so a truncated name is matched the same way every time
	byTitle := map[string][]string{}
	for _, j := range jsons {
		if j.sidecar == nil {
			continue
		}
		byName[strings.ToLower(j.name)] = j.name
		lower = append(lower, strings.ToLower(j.name))
		if t := strings.ToLower(j.sidecar.Title); t != "" {
			byTitle[t] = append(byTitle[t], j.name)
		}
	}
	sort.Strings(lower)
	named := func(m, n string) (string, string) {
		lm := strings.ToLower(m)
		if j, ok := byName[lm+n+".json"]; ok {
			return j, FormName
		}
		if j, ok := byName[lm+supplemental+n+".json"]; ok {
			return j, FormSupplemental
		}
		for _, lj := range lower { // the base truncated, but past the whole media name: IMG_1.jpg.supplemental-me.json
			j := byName[lj]
			base := strings.TrimSuffix(lj, n+".json")
			if base != lj && len(base) > len(lm) && strings.HasPrefix(lm+supplemental, base) {
				return j, FormTruncated
			}
		}
		return "", ""
	}
	return func(m string) (string, string) {
		if j, form := named(m, ""); j != "" {
			return j, form
		}
		if d := dupRE.FindStringSubmatch(m); d != nil {
			if j, _ := named(d[1]+d[3], "("+d[2]+")"); j != "" {
				return j, FormDuplicate
			}
		}
		if e := editedRE.FindStringSubmatch(m); e != nil {
			if j, _ := named(e[1]+e[2], ""); j != "" {
				return j, FormEdited
			}
		}
		if js := byTitle[strings.ToLower(m)]; len(js) == 1 {
			return js[0], FormTitle
		}
		return "", ""
	}
}

// ExifCount is what the metadata of one format's files says.
type ExifCount struct {
	Files int `json:"files"`
	Date  int `json:"with_a_date"`
	GPS   int `json:"with_a_position"`
}

// Inventory is the counts the owner pastes (plan 032, Phase A): never a name, a date, a place or a caption.
type Inventory struct {
	Folders         int                  `json:"folders"`
	YearFolders     int                  `json:"folders_named_with_a_year"`
	MetadataFolders int                  `json:"folders_with_a_metadata_json"`
	Media           map[string]int       `json:"media_by_extension"`
	JSONFiles       int                  `json:"json_files"`
	Sidecars        int                  `json:"photo_sidecars"`
	FoundBy         map[string]int       `json:"sidecar_found_by"`
	SharedSidecars  int                  `json:"sidecars_shared_by_several_files"`
	Unclaimed       int                  `json:"sidecars_no_file_claims"`
	Keys            map[string]int       `json:"sidecar_keys"`
	GeoData         int                  `json:"position_in_geodata"`
	GeoDataExifOnly int                  `json:"position_in_geodataexif_only"`
	NoPosition      int                  `json:"position_none_or_zero"`
	TakenTime       int                  `json:"photo_taken_time"`
	Descriptions    int                  `json:"descriptions_not_empty"`
	WithPeople      int                  `json:"sidecars_naming_people"`
	SameNameAndSize int                  `json:"files_also_in_another_folder"`
	Exif            map[string]ExifCount `json:"exif_by_format"`
	ExifReadBytes   int                  `json:"exif_read_from_first_bytes"`
}

var yearRE = regexp.MustCompile(`(19|20)\d\d$`)

// head is how much of a file is read for its metadata: a JPEG's EXIF is in its first 64 KB, a HEIC's Exif item near
// its start.
const head = 1 << 20

// Take counts an export's folder.
func Take(root string) (*Inventory, error) {
	media, sidecars, keys, err := Scan(root)
	if err != nil {
		return nil, err
	}
	inv := &Inventory{Media: map[string]int{}, FoundBy: map[string]int{}, Keys: map[string]int{}, Exif: map[string]ExifCount{},
		ExifReadBytes: head}
	folders := map[string]bool{}
	for p, ks := range keys {
		inv.JSONFiles++
		folders[path.Dir(p)] = true
		if strings.EqualFold(path.Base(p), "metadata.json") {
			inv.MetadataFolders++
		}
		if s := sidecars[p]; s != nil {
			inv.Sidecars++
			for _, k := range ks {
				inv.Keys[k]++
			}
			switch {
			case s.GeoData.Known():
				inv.GeoData++
			case s.GeoDataExif.Known():
				inv.GeoDataExifOnly++
			default:
				inv.NoPosition++
			}
			if s.PhotoTakenTime != nil && s.PhotoTakenTime.Timestamp != "" {
				inv.TakenTime++
			}
			if strings.TrimSpace(s.Description) != "" {
				inv.Descriptions++
			}
			if len(s.People) > 0 {
				inv.WithPeople++
			}
		}
	}
	claims := map[string]int{}
	seen := map[[2]any]string{}
	for _, m := range media {
		folders[m.Dir] = true
		ext := strings.ToLower(path.Ext(m.Path))
		inv.Media[ext]++
		inv.FoundBy[m.Form]++
		if m.Sidecar != "" {
			claims[m.Sidecar]++
		}
		full := filepath.Join(root, filepath.FromSlash(m.Path))
		if st, err := os.Stat(full); err == nil {
			k := [2]any{strings.ToLower(path.Base(m.Path)), st.Size()}
			if d, ok := seen[k]; ok && d != m.Dir {
				inv.SameNameAndSize++
			} else if !ok {
				seen[k] = m.Dir
			}
		}
		format := map[string]string{".jpg": "jpeg", ".jpeg": "jpeg", ".heic": "heic", ".heif": "heic"}[ext]
		if format == "" {
			format = "other (not read)"
			c := inv.Exif[format]
			c.Files++
			inv.Exif[format] = c
			continue
		}
		c := inv.Exif[format]
		c.Files++
		if b, err := readHead(full); err == nil {
			meta := photo.Read(b)
			if meta.Taken != "" {
				c.Date++
			}
			if meta.HasGPS {
				c.GPS++
			}
		}
		inv.Exif[format] = c
	}
	for p := range sidecars {
		switch n := claims[p]; {
		case n == 0:
			inv.Unclaimed++
		case n > 1:
			inv.SharedSidecars++
		}
	}
	for f := range folders {
		if f == "." {
			continue
		}
		inv.Folders++
		if yearRE.MatchString(path.Base(f)) {
			inv.YearFolders++
		}
	}
	return inv, nil
}

func readHead(p string) ([]byte, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, head))
}
