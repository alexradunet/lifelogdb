// Package inventory surveys an ingest folder without reading a content: which folders hold how many files of what
// kind, what the archives hold, and where the sources are — a folder of notes, a Takeout extraction. It is the
// one step of an import done by a party that may see paths and counts but never a value (the owner at a terminal,
// a hosted model): docs/guides/importing.md, "An ingest folder". File names are reported only as digit-masked
// patterns (IMG_N_N.jpg), never one by one; an unreadable file is counted, not named.
package inventory

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// The bounds keep the report readable and the run finite; what they cut is said in Report.Truncated.
const (
	maxFolders        = 4096
	maxPatterns       = 8
	maxArchives       = 256
	maxArchiveEntries = 65536
	maxArchiveFolders = 32
)

var errUnreadable = errors.New("inventory could not read the folder")

// Report is the survey, printable as JSON.
type Report struct {
	Files     int       `json:"files"`
	Bytes     int64     `json:"bytes"`
	Folders   []Folder  `json:"folders"`
	Archives  []Archive `json:"archives,omitempty"`
	Sources   []Source  `json:"sources,omitempty"`
	Hidden    int       `json:"hidden_folders,omitempty"` // left out, not entered
	Unread    int       `json:"unreadable,omitempty"`
	Truncated []string  `json:"truncated,omitempty"`
}

// Folder is one folder's own files (not its subfolders'), with FilesBelow the whole subtree's.
type Folder struct {
	Path       string  `json:"path"` // /-separated, "." for the root
	Files      int     `json:"files"`
	FilesBelow int     `json:"files_below"`
	Bytes      int64   `json:"bytes"`
	Extensions []Count `json:"extensions,omitempty"`
	Patterns   []Count `json:"patterns,omitempty"` // file names with every run of digits as N
}

type Count struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// Archive is a zip, tar, tar.gz or tgz, listed without extracting: its entries by their first two path
// segments, digit-masked.
type Archive struct {
	Path    string  `json:"path"`
	Bytes   int64   `json:"bytes"`
	Entries int     `json:"entries"`
	Unread  bool    `json:"unreadable,omitempty"`
	Folders []Count `json:"folders,omitempty"`
	Clipped bool    `json:"clipped,omitempty"` // more entries than the bound: counts stop there
}

// Source is a folder the survey recognises: "notes", a folder whose files are mostly Markdown (an Obsidian vault
// is one), or "takeout", a Google Takeout extraction with its product folders.
type Source struct {
	Path       string  `json:"path"`
	Kind       string  `json:"kind"`
	Files      int     `json:"files"`
	Markdown   int     `json:"markdown,omitempty"`
	DailyNotes int     `json:"daily_notes,omitempty"` // YYYY-MM-DD.md
	Products   []Count `json:"products,omitempty"`    // a Takeout's product folders, with their files
}

// Run surveys folder. The error names no file.
func Run(ctx context.Context, dir string) (*Report, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	base, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return nil, errUnreadable
	}
	if st, err := os.Stat(base); err != nil || !st.IsDir() {
		return nil, errUnreadable
	}
	s := &survey{ctx: ctx, base: base, folders: map[string]*folder{}}
	if err := s.walk(); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		return nil, errUnreadable
	}
	return s.report(), nil
}

type survey struct {
	ctx      context.Context
	base     string
	folders  map[string]*folder
	order    []string
	archives []Archive
	rep      Report
}

type folder struct {
	Folder
	exts     map[string]int
	patterns map[string]int
	md       int // Markdown files below
	daily    int
	obsidian bool
	children []string
}

func (s *survey) folder(p string) *folder {
	f := s.folders[p]
	if f == nil {
		f = &folder{Folder: Folder{Path: p}, exts: map[string]int{}, patterns: map[string]int{}}
		s.folders[p] = f
		s.order = append(s.order, p)
		if p != "." {
			parent := path.Dir(p)
			s.folder(parent).children = append(s.folder(parent).children, p)
		}
	}
	return f
}

func (s *survey) walk() error {
	root, err := os.OpenRoot(s.base)
	if err != nil {
		return err
	}
	defer root.Close()
	return fs.WalkDir(root.FS(), ".", func(p string, d fs.DirEntry, err error) error {
		if e := s.ctx.Err(); e != nil {
			return e
		}
		if err != nil {
			s.rep.Unread++
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		name := d.Name()
		if p != "." && strings.HasPrefix(name, ".") {
			if d.IsDir() {
				s.rep.Hidden++
				if name == ".obsidian" {
					s.folder(path.Dir(p)).obsidian = true
				}
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if len(s.folders) >= maxFolders {
				s.truncate("folders beyond " + strconv.Itoa(maxFolders) + " are not listed")
				return fs.SkipDir
			}
			s.folder(p)
			return nil
		}
		if d.Type()&fs.ModeSymlink != 0 || !d.Type().IsRegular() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			s.rep.Unread++
			return nil
		}
		s.addFile(p, name, info.Size())
		if isArchive(name) {
			s.addArchive(root, p, name, info.Size())
		}
		return nil
	})
}

func (s *survey) addFile(p, name string, size int64) {
	dir := s.folder(path.Dir(p))
	dir.Files++
	dir.Bytes += size
	dir.exts[extension(name)]++
	dir.patterns[mask(name)]++
	md := strings.EqualFold(path.Ext(name), ".md")
	daily := md && dailyNote.MatchString(name)
	for f := dir; ; f = s.folders[path.Dir(f.Path)] {
		f.FilesBelow++
		if md {
			f.md++
		}
		if daily {
			f.daily++
		}
		if f.Path == "." {
			break
		}
	}
	s.rep.Files++
	s.rep.Bytes += size
}

var dailyNote = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}\.md$`)
var digits = regexp.MustCompile(`\d+`)

// mask replaces every run of digits with N: IMG_20240102_123456.jpg is IMG_N_N.jpg.
func mask(name string) string { return digits.ReplaceAllString(name, "N") }

func extension(name string) string {
	ext := strings.ToLower(path.Ext(name))
	if ext == "" || ext == name {
		return "(none)"
	}
	return ext
}

func isArchive(name string) bool {
	low := strings.ToLower(name)
	return strings.HasSuffix(low, ".zip") || strings.HasSuffix(low, ".tar") || strings.HasSuffix(low, ".tar.gz") || strings.HasSuffix(low, ".tgz")
}

func (s *survey) addArchive(root *os.Root, p, name string, size int64) {
	if len(s.archives) >= maxArchives {
		s.truncate("archives beyond " + strconv.Itoa(maxArchives) + " are not listed")
		return
	}
	a := Archive{Path: p, Bytes: size}
	folders := map[string]int{}
	add := func(entry string, isDir bool) bool {
		if isDir || strings.HasSuffix(entry, "/") {
			return true
		}
		if a.Entries >= maxArchiveEntries {
			a.Clipped = true
			return false
		}
		a.Entries++
		segs := strings.Split(strings.TrimPrefix(entry, "/"), "/")
		top := mask(segs[0])
		if len(segs) > 1 {
			top += "/" + mask(segs[1])
		}
		folders[top]++
		return true
	}
	low := strings.ToLower(name)
	var err error
	switch {
	case strings.HasSuffix(low, ".zip"):
		err = listZip(root, p, add)
	default:
		err = listTar(root, p, strings.HasSuffix(low, ".gz") || strings.HasSuffix(low, ".tgz"), add)
	}
	if err != nil {
		a.Unread = true
	}
	a.Folders = top(folders, maxArchiveFolders)
	s.archives = append(s.archives, a)
}

func listZip(root *os.Root, p string, add func(string, bool) bool) error {
	f, err := root.Open(filepath.FromSlash(p))
	if err != nil {
		return err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return err
	}
	r, err := zip.NewReader(f, st.Size())
	if err != nil {
		return err
	}
	for _, e := range r.File {
		if !add(e.Name, e.FileInfo().IsDir()) {
			break
		}
	}
	return nil
}

func listTar(root *os.Root, p string, gz bool, add func(string, bool) bool) error {
	f, err := root.Open(filepath.FromSlash(p))
	if err != nil {
		return err
	}
	defer f.Close()
	var r io.Reader = f
	if gz {
		g, err := gzip.NewReader(f)
		if err != nil {
			return err
		}
		defer g.Close()
		r = g
	}
	t := tar.NewReader(r)
	for {
		h, err := t.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if !add(h.Name, h.Typeflag == tar.TypeDir) {
			return nil
		}
	}
}

func (s *survey) truncate(what string) {
	for _, t := range s.rep.Truncated {
		if t == what {
			return
		}
	}
	s.rep.Truncated = append(s.rep.Truncated, what)
}

func (s *survey) report() *Report {
	r := s.rep
	sort.Strings(s.order)
	for _, p := range s.order {
		f := s.folders[p]
		f.Extensions = top(f.exts, 0)
		f.Patterns = top(f.patterns, maxPatterns)
		r.Folders = append(r.Folders, f.Folder)
	}
	r.Archives = s.archives
	r.Sources = s.sources(".")
	return &r
}

// sources finds the sources top-down: a folder recognised as one is not searched inside.
func (s *survey) sources(p string) []Source {
	f := s.folders[p]
	if src, ok := s.recognise(f); ok {
		return []Source{src}
	}
	var out []Source
	sort.Strings(f.children)
	for _, c := range f.children {
		out = append(out, s.sources(c)...)
	}
	return out
}

// googleProducts are the names a Takeout extraction gives its product folders (Google's names, not the owner's).
var googleProducts = map[string]bool{"Calendar": true, "Chrome": true, "Contacts": true, "Drive": true, "Fit": true,
	"Google Health": true, "Google Photos": true, "Keep": true, "Location History": true, "Mail": true, "Maps": true,
	"My Activity": true, "Tasks": true, "Timeline": true, "YouTube and YouTube Music": true}

func (s *survey) recognise(f *folder) (Source, bool) {
	products := 0
	for _, c := range f.children {
		if googleProducts[path.Base(c)] || path.Base(c) == "Takeout" {
			products++
		}
	}
	if products >= 2 || (products == 1 && path.Base(f.Path) == "Takeout") {
		src := Source{Path: f.Path, Kind: "takeout", Files: f.FilesBelow}
		for _, c := range f.children {
			src.Products = append(src.Products, Count{path.Base(c), s.folders[c].FilesBelow})
		}
		return src, true
	}
	if f.obsidian || (f.md > 0 && f.md*2 >= f.FilesBelow) {
		return Source{Path: f.Path, Kind: "notes", Files: f.FilesBelow, Markdown: f.md, DailyNotes: f.daily}, true
	}
	return Source{}, false
}

// top sorts counts by count, then name; n > 0 keeps the n most common and folds the rest into "<other>", and
// first folds every name seen once into "<one of a kind>": a name that repeats is a pattern, a name that does
// not is one file's name, which the report never prints.
func top(m map[string]int, n int) []Count {
	out := make([]Count, 0, len(m))
	once := 0
	for k, v := range m {
		if v == 1 && n > 0 {
			once++
			continue
		}
		out = append(out, Count{k, v})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Name < out[j].Name
	})
	if n > 0 && len(out) > n {
		rest := 0
		for _, c := range out[n:] {
			rest += c.Count
		}
		out = append(out[:n], Count{"<other>", rest})
	}
	if once > 0 {
		out = append(out, Count{"<one of a kind>", once})
	}
	return out
}
