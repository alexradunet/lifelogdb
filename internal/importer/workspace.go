// Package importer is the writer's side of docs/guides/importing.md: the workspace files, the checks of a
// facts file, apply, the Obsidian vault plan, status and replay. The guide is the contract; this package
// implements it and cites it.
package importer

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"golang.org/x/text/unicode/norm"

	"lifelog/internal/core"
	"lifelog/internal/db"
)

// Workspace is one source's import workspace: the folder <source>.lifelog beside the source. It is fixed when
// the process starts; no request names a path outside it or the source.
type Workspace struct {
	Dir    string // the workspace, …/Notebook.lifelog
	Source string // the source, …/Notebook
	mu     sync.Mutex
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
// enters a hidden folder.
func (w *Workspace) SourcePath(rel string) (string, error) {
	rel = filepath.ToSlash(strings.TrimSpace(rel))
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
	p := filepath.Join(w.Source, filepath.FromSlash(rel))
	if !strings.HasPrefix(p, w.Source+string(filepath.Separator)) {
		return "", refuse("%q leaves the source", rel)
	}
	return p, nil
}

// ReadSource reads a source file, NFC-normalised (a file system may store names and text in NFD).
func (w *Workspace) ReadSource(rel string) (string, error) {
	p, err := w.SourcePath(rel)
	if err != nil {
		return "", err
	}
	b, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return "", &core.Error{Status: 404, Msg: "no source file " + rel}
	}
	return norm.NFC.String(string(b)), err
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
func (w *Workspace) Approve(name string, today time.Time) error {
	if name != "rules.md" && name != "metrics.md" {
		return fmt.Errorf("approve rules.md or metrics.md, not %s", name)
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	text, ok, err := w.read(name)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("no %s to approve", name)
	}
	_, rest := splitStatus(text)
	if name == "metrics.md" {
		rest = approveRows(rest)
	}
	stamp := fmt.Sprintf("status: approved %s (owner) sha256:%s", today.Format(time.DateOnly), bodyHash(rest))
	return writeAtomic(w.file(name), []byte(stamp+"\n"+rest))
}

// ---- rules.md

// Rules is what the writer reads from rules.md: the source name, aliases and distinct pairs.
type Rules struct {
	Source   string
	Aliases  map[string]string // normalised name -> title
	Distinct map[[2]string]bool
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
	r := &Rules{Aliases: map[string]string{}, Distinct: map[[2]string]bool{}}
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
		case "Aliases":
			if m := aliasLine.FindStringSubmatch(line); m != nil {
				r.Aliases[nameKey(m[1])] = m[2]
			}
		case "Distinct":
			if m := distinctLine.FindStringSubmatch(line); m != nil {
				a, b := nameKey(m[1]), nameKey(m[2])
				r.Distinct[[2]string{a, b}], r.Distinct[[2]string{b, a}] = true, true
			}
		}
	}
	return r, nil
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
