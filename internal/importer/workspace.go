// Package importer is the writer's side of docs/guides/importing.md: the workspace files, the checks of a
// facts file, apply, the Obsidian vault plan, status and replay. The guide is the contract; this package
// implements it and cites it.
package importer

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/text/unicode/norm"

	"lifelog/internal/core"
	"lifelog/internal/db"
)

// Workspace is one source's import workspace: the folder <source>.lifelog beside the source. It is fixed when
// the process starts; no request names a path outside it or the source.
type Workspace struct {
	Dir         string       // the workspace, …/Notebook.lifelog
	Source      string       // the source, …/Notebook
	selectionMu sync.RWMutex // Replay excludes workspace selection changes through its full rehearsal/application.
	mu          sync.Mutex
}

// Error is a refusal with a status, like core.Error.
func refuse(format string, a ...any) error {
	return &core.Error{Status: 422, Msg: fmt.Sprintf(format, a...)}
}

// Open opens (and creates, if missing) the workspace of a source.
func Open(dir string) (*Workspace, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	if !strings.HasSuffix(dir, ".lifelog") {
		return nil, fmt.Errorf("a workspace is named after its source: <source>.lifelog, not %s", filepath.Base(dir))
	}
	src := strings.TrimSuffix(dir, ".lifelog")
	if st, err := os.Stat(src); err != nil || !st.IsDir() {
		return nil, fmt.Errorf("no source folder %s beside the workspace", src)
	}
	if err := os.MkdirAll(filepath.Join(dir, "facts"), 0o700); err != nil {
		return nil, err
	}
	return &Workspace{Dir: dir, Source: src}, nil
}

// TrialDB is the trial database's path.
func (w *Workspace) TrialDB() string { return filepath.Join(w.Dir, "trial.db") }

func (w *Workspace) file(name string) string { return filepath.Join(w.Dir, name) }

// SourcePath resolves a source-relative path ("Journal/2031-04-12.md") and refuses one that leaves the source or
// enters a hidden folder. The import's logical paths are NFC, but the physical file system may spell names in
// another canonically equivalent form; an exact full physical path wins, otherwise every component must resolve to
// exactly one complete physical path.
func (w *Workspace) SourcePath(rel string) (string, error) {
	root, err := os.OpenRoot(w.Source)
	if err != nil {
		return "", err
	}
	defer root.Close()
	p, err := w.sourcePath(root, rel)
	if err != nil {
		return "", err
	}
	return filepath.Join(w.Source, p), nil
}

// sourcePath returns a root-relative spelling, never a capability for an unrestricted open.
func (w *Workspace) sourcePath(root *os.Root, rel string) (string, error) {
	rel = filepath.ToSlash(rel)
	if rel == "" || strings.HasPrefix(rel, "/") || filepath.IsAbs(rel) || strings.Contains(rel, ":") {
		return "", refuse("%q is not a path relative to the source", rel)
	}
	for _, part := range strings.Split(rel, "/") {
		if part == ".." || part == "." || part == "" {
			return "", refuse("%q is not a plain path inside the source", rel)
		}
		if strings.HasPrefix(part, ".") {
			return "", refuse("%q is in a hidden folder, which is never imported", rel)
		}
	}
	exact := filepath.FromSlash(rel)
	if _, err := root.Stat(exact); err == nil {
		return exact, nil
	} else if !sourcePathMissing(err) {
		return "", err
	}
	resolved, ok, err := w.resolveSourcePath(root, rel)
	if err != nil || ok {
		return resolved, err
	}
	return exact, nil
}

func (w *Workspace) resolveSourcePath(root *os.Root, rel string) (string, bool, error) {
	candidates := []string{"."}
	parts := strings.Split(rel, "/")
	for i, part := range parts {
		want := norm.NFC.String(part)
		var next []string
		for _, base := range candidates {
			entries, err := fs.ReadDir(root.FS(), filepath.ToSlash(base))
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil {
				return "", false, err
			}
			for _, e := range entries {
				name := e.Name()
				if strings.HasPrefix(name, ".") || norm.NFC.String(name) != want {
					continue
				}
				candidate := filepath.Join(base, name)
				if i < len(parts)-1 {
					isDir, err := sourceDirCandidate(root, candidate)
					if err != nil {
						return "", false, err
					}
					if !isDir {
						continue
					}
				}
				if _, err := root.Stat(candidate); err != nil {
					return "", false, err
				}
				next = append(next, candidate)
			}
		}
		if len(next) == 0 {
			return "", false, nil
		}
		candidates = next
	}
	if len(candidates) == 1 {
		return candidates[0], true, nil
	}
	sort.Strings(candidates)
	return "", false, refuse("%q is ambiguous: multiple physical source paths have the same logical spelling (%s)", rel, strings.Join(sourceRelPaths(".", candidates), ", "))
}

func sourcePathMissing(err error) bool {
	return errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ENOTDIR)
}

func sourceDirCandidate(root *os.Root, p string) (bool, error) {
	st, err := root.Stat(p)
	if sourcePathMissing(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return st.IsDir(), nil
}

func sourceRelPaths(root string, paths []string) []string {
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		rel, err := filepath.Rel(root, p)
		if err != nil {
			out = append(out, p)
			continue
		}
		out = append(out, filepath.ToSlash(rel))
	}
	return out
}

// ReadSource reads a source file, NFC-normalised (a file system may store names and text in NFD).
func (w *Workspace) ReadSource(rel string) (string, error) {
	root, err := os.OpenRoot(w.Source)
	if err != nil {
		return "", err
	}
	defer root.Close()
	return w.readSource(root, rel)
}

// readRawSource preserves syntax for evidence parsers; public/vault reads stay NFC.
func (w *Workspace) readRawSource(rel string) (string, error) {
	root, err := os.OpenRoot(w.Source)
	if err != nil {
		return "", err
	}
	defer root.Close()
	return w.readRawSourceAt(root, rel)
}

func (w *Workspace) readSource(root *os.Root, rel string) (string, error) {
	source, err := w.readRawSourceAt(root, rel)
	return norm.NFC.String(source), err
}

func (w *Workspace) readRawSourceAt(root *os.Root, rel string) (string, error) {
	p, err := w.sourcePath(root, rel)
	if err != nil {
		return "", err
	}
	b, err := root.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return "", &core.Error{Status: 404, Msg: "no source file " + rel}
	}
	return string(b), err
}

// writeAtomic replaces a workspace file in one step: a temporary file, then a rename.
func writeAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func (w *Workspace) read(name string) (string, bool, error) {
	if name == preparedFile || name == selectedPhotoFile {
		b, err := w.readBoundedWorkspace(name, 4*maxProfileBytes)
		if errors.Is(err, os.ErrNotExist) {
			return "", false, nil
		}
		return strings.ReplaceAll(string(b), string([]byte{13, 10}), string(rune(10))), err == nil, err
	}
	b, err := os.ReadFile(w.file(name))
	if errors.Is(err, os.ErrNotExist) {
		return "", false, nil
	}
	return strings.ReplaceAll(string(b), "\r\n", "\n"), err == nil, err
}

// ---- gates: a status line the owner stamps (the guide's "Gates")

var statusLine = regexp.MustCompile(`^status:\s*(\S.*)$`)

// stampRE is the owner's stamp. The hash covers the rest of the file, so an edit after approval closes the gate.
var stampRE = regexp.MustCompile(`^status: approved (\d{4}-\d{2}-\d{2}) \(owner\) sha256:([0-9a-f]{64})$`)

func bodyHash(rest string) string {
	sum := sha256.Sum256([]byte(rest))
	return hex.EncodeToString(sum[:])
}

func splitStatus(text string) (status, rest string) {
	first, rest, _ := strings.Cut(text, "\n")
	return strings.TrimSpace(first), rest
}

// Gate says whether a stamped file (rules.md, metrics.md) is approved: "missing", "draft", "approved", or
// "stale" (approved, then edited).
func (w *Workspace) Gate(name string) (string, error) {
	text, ok, err := w.read(name)
	if err != nil || !ok {
		return "missing", err
	}
	status, rest := splitStatus(text)
	m := stampRE.FindStringSubmatch(status)
	switch {
	case m != nil && m[2] == bodyHash(rest):
		return "approved", nil
	case m != nil || strings.HasPrefix(status, "status: approved"):
		return "stale", nil
	case statusLine.MatchString(status):
		return "draft", nil
	}
	return "missing", nil
}

// Approve stamps rules.md or metrics.md as the owner's. Only the CLI calls it, at an interactive terminal; it
// is not in the API, so no model can reach it. In metrics.md every row still proposed becomes approved first.
// shown is the Hash of the Review the owner read: a body that changed since is refused, never stamped. The
// approved file is then copied for the next review (approval.go).
func (w *Workspace) Approve(name string, today time.Time, shown string) error {
	w.selectionMu.RLock()
	defer w.selectionMu.RUnlock()
	w.mu.Lock()
	defer w.mu.Unlock()
	rest, err := w.toStamp(name)
	if err != nil {
		return err
	}
	if bodyHash(rest) != shown {
		return fmt.Errorf("%s changed after it was shown: nothing stamped; approve it again", name)
	}
	stamped := []byte(fmt.Sprintf("status: approved %s (owner) sha256:%s", today.Format(time.DateOnly), bodyHash(rest)) + "\n" + rest)
	if err := writeAtomic(w.file(name), stamped); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(w.Dir, approvedDir), 0o700); err != nil {
		return fmt.Errorf("%s approved, but no copy kept for the next review: %w", name, err)
	}
	if err := writeAtomic(w.approvedPath(name), stamped); err != nil {
		return fmt.Errorf("%s approved, but no copy kept for the next review: %w", name, err)
	}
	return nil
}

// ---- rules.md

// Rules is what the writer reads from rules.md, the source name, and what a write of this workspace may use: the
// owner's name decisions of entities.md, and the aliases among them (normalised name -> title). rules.md holds no
// name decision: its old `## Aliases` and `## Distinct` lines are Legacy, for propose-entities to move.
type Rules struct {
	Source  string
	Aliases map[string]string
	Names   names
	Legacy  legacyNames
	Folders []folderRule
}

// folderRule is one `## Folders` line of rules.md: its patterns, in the glob language of *skip*, and the mark its
// words give a file that *ledger* adds: "-" for words that start with `skip:`, ">" for `later:`, "" for a rule that
// the model follows (the guide's "rules.md").
type folderRule struct {
	patterns []string
	mark     string
	note     string
}

// matches says whether a ledger file falls under the rule.
func (r folderRule) matches(file string) bool {
	for _, p := range r.patterns {
		if matchGlob(p, file) {
			return true
		}
	}
	return false
}

// backtickedPatterns is the start of a `## Folders` line whose patterns are in backticks: a pattern in backticks
// may hold a dash or a comma, so they are read before the words.
var backtickedPatterns = regexp.MustCompile("^((?:`[^`]+`\\s*,?\\s*)+)(.*)$")

// parseFolderLine reads a `## Folders` line: `- `, one or more patterns (in backticks, or bare and separated by
// commas), a dash, and the words.
func parseFolderLine(line string) (folderRule, bool) {
	if !strings.HasPrefix(line, "-") {
		return folderRule{}, false
	}
	body := strings.TrimSpace(line[1:])
	var r folderRule
	var words string
	if m := backtickedPatterns.FindStringSubmatch(body); m != nil {
		for _, p := range strings.Split(m[1], "`") {
			if p = strings.TrimSpace(p); p != "" && p != "," {
				r.patterns = append(r.patterns, p)
			}
		}
		words = m[2]
	} else {
		pats := body
		for _, sep := range []string{" — ", " – ", " - "} {
			if i := strings.Index(body, sep); i >= 0 {
				pats, words = body[:i], body[i+len(sep):]
				break
			}
		}
		for _, p := range splitPatterns(pats) {
			if p = strings.TrimSpace(p); p != "" {
				r.patterns = append(r.patterns, p)
			}
		}
	}
	if len(r.patterns) == 0 {
		return folderRule{}, false
	}
	words = oneLine(strings.TrimLeft(words, " \t—–-:"))
	// the note reads as a *skip* or *defer* mark does: its word in lower case, then the rule's words
	switch lower := strings.ToLower(words); {
	case strings.HasPrefix(lower, "skip:"):
		r.mark, r.note = "-", "skip:"+words[len("skip:"):]
	case strings.HasPrefix(lower, "later:"):
		r.mark, r.note = ">", "later:"+words[len("later:"):]
	}
	return r, true
}

// splitPatterns splits bare patterns at the commas outside `{a,b}` groups.
func splitPatterns(s string) []string {
	var out []string
	depth, start := 0, 0
	for i, c := range s {
		switch c {
		case '{':
			depth++
		case '}':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				out = append(out, s[start:i])
				start = i + 1
			}
		}
	}
	return append(out, s[start:])
}

// legacyNames are the alias and distinct lines a rules.md wrote before name decisions moved to entities.md.
type legacyNames struct {
	Aliases  [][2]string // name, title
	Distinct [][2]string
}

var (
	sourceLine   = regexp.MustCompile(`(?m)^source:\s*(import:[a-z0-9_.-]+)\s*$`)
	aliasLine    = regexp.MustCompile(`^-\s*"([^"]+)"\s*→\s*"([^"]+)"\s*$`)
	distinctLine = regexp.MustCompile(`^-\s*"([^"]+)"\s*≠\s*"([^"]+)"\s*$`)
)

// nameKey compares names as the guide says: case-insensitively, runs of spaces collapsed.
func nameKey(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(norm.NFC.String(s))), " ")
}

func (w *Workspace) Rules() (*Rules, error) {
	text, ok, err := w.read("rules.md")
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, refuse("no rules.md yet: survey the source and draft it (guide step 2)")
	}
	return parseRules(text)
}
func parseRules(text string) (*Rules, error) {
	r := &Rules{Aliases: map[string]string{}}
	if m := sourceLine.FindStringSubmatch(text); m != nil {
		r.Source = m[1]
	} else {
		return nil, refuse("rules.md has no `source: import:<name>` line")
	}
	section := ""
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "## ") {
			section = strings.TrimSpace(line[3:])
			continue
		}
		switch section {
		case "Folders":
			if f, ok := parseFolderLine(line); ok {
				r.Folders = append(r.Folders, f)
			}
		case "Aliases":
			if m := aliasLine.FindStringSubmatch(line); m != nil {
				r.Legacy.Aliases = append(r.Legacy.Aliases, [2]string{m[1], m[2]})
			}
		case "Distinct":
			if m := distinctLine.FindStringSubmatch(line); m != nil {
				r.Legacy.Distinct = append(r.Legacy.Distinct, [2]string{m[1], m[2]})
			}
		}
	}
	return r, nil
}

// withoutNameSections is a rules.md text without its `## Aliases` and `## Distinct` sections: what is left when
// propose-entities has moved their lines to entities.md.
func withoutNameSections(text string) string {
	var out []string
	skip := false
	for _, line := range strings.Split(text, "\n") {
		if t := strings.TrimSpace(line); strings.HasPrefix(t, "## ") {
			s := strings.TrimSpace(t[3:])
			skip = s == "Aliases" || s == "Distinct"
		}
		if !skip {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}

// Name is the import's source (lifelog_meta.source), from rules.md.
func (w *Workspace) Name() (string, error) {
	r, err := w.Rules()
	if err != nil {
		return "", err
	}
	return r.Source, nil
}

// DraftRules writes rules.md for the owner to review. It never writes a status line: the file becomes a draft
// again, so a change after approval (an alias from an answer, guide step 8) closes the gate until the owner
// approves it again.
func (w *Workspace) DraftRules(body string) error {
	w.selectionMu.RLock()
	defer w.selectionMu.RUnlock()
	body = strings.ReplaceAll(body, "\r\n", "\n")
	if statusLine.MatchString(strings.TrimSpace(strings.SplitN(body, "\n", 2)[0])) {
		return refuse("write the rules without a status line: the writer keeps it")
	}
	if !sourceLine.MatchString(body) {
		return refuse("the rules need a line `source: import:<name>` (lowercase name)")
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return writeAtomic(w.file("rules.md"), []byte("status: draft\n"+strings.TrimLeft(body, "\n")))
}

// Setup makes the trial database: a copy of the real life.db when one is given, else a new one from the schema.
func (w *Workspace) Setup(from string) (string, error) {
	if exists(w.TrialDB()) {
		return "", &core.Error{Status: 409, Msg: "trial.db exists: it is never re-created (fix mistakes by editing facts and applying again)"}
	}
	if from != "" {
		return "copied " + from, db.Copy(from, w.TrialDB())
	}
	return "created from the schema", db.Init(w.TrialDB())
}

func (w *Workspace) statSource(rel string) (os.FileInfo, error) {
	root, err := os.OpenRoot(w.Source)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	p, err := w.sourcePath(root, rel)
	if err != nil {
		return nil, err
	}
	return root.Stat(p)
}
