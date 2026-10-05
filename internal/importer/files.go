package importer

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/text/unicode/norm"

	ltext "lifelog/internal/text"

	"lifelog/internal/core"
)

// ---- metrics.md: a table found by its header (the guide's "metrics.md")

// Metric is one row of metrics.md.
type Metric struct {
	Status string `json:"status"`
	Name   string `json:"name"`
	Unit   string `json:"unit"`
	Note   string `json:"note,omitempty"`
	From   string `json:"from,omitempty"`
	Doubts string `json:"doubts,omitempty"`
	Since  string `json:"since,omitempty"`
	Until  string `json:"until,omitempty"`
	// Category is the path of the category pages the metric is filed in (D26): "Biomarkers/Iron"; "" files nothing.
	Category string `json:"category,omitempty"`
}

var metricCols = []string{"status", "name", "unit", "note", "from", "doubts", "since", "until", "category"}

const metricsHeader = "| status | name | unit | note | from | doubts | since | until | category |\n|---|---|---|---|---|---|---|---|---|\n"

// tableCells splits a markdown table row on the | that are not escaped.
func tableCells(row string) []string {
	row = strings.TrimSpace(row)
	row = strings.TrimSuffix(strings.TrimPrefix(row, "|"), "|")
	var out []string
	var cur strings.Builder
	for i := 0; i < len(row); i++ {
		if row[i] == '\\' && i+1 < len(row) && row[i+1] == '|' {
			cur.WriteByte('|')
			i++
			continue
		}
		if row[i] == '|' {
			out = append(out, strings.TrimSpace(cur.String()))
			cur.Reset()
			continue
		}
		cur.WriteByte(row[i])
	}
	return append(out, strings.TrimSpace(cur.String()))
}

func cell(s string) string { return strings.ReplaceAll(strings.ReplaceAll(s, "|", `\|`), "\n", " ") }

// Metrics reads metrics.md's rows.
func (w *Workspace) Metrics() ([]Metric, error) {
	text, ok, err := w.read("metrics.md")
	if err != nil || !ok {
		return nil, err
	}
	var cols []string
	var out []Metric
	for _, line := range strings.Split(text, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "|") {
			cols = nil
			continue
		}
		cells := tableCells(line)
		if cols == nil {
			if contains(cells, "status") && contains(cells, "name") && contains(cells, "unit") {
				cols = cells
			}
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(cells[0]), "---") {
			continue
		}
		m := Metric{}
		for i, c := range cols {
			if i >= len(cells) {
				break
			}
			v := cells[i]
			switch c {
			case "status":
				m.Status = v
			case "name":
				m.Name = v
			case "unit":
				m.Unit = v
			case "note":
				m.Note = v
			case "from":
				m.From = v
			case "doubts":
				m.Doubts = v
			case "since":
				m.Since = v
			case "until":
				m.Until = v
			case "category":
				m.Category = v
			}
		}
		out = append(out, m)
	}
	return out, nil
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

// ApprovedMetrics are the rows of a stamped metrics.md the owner approved; nil while the gate is closed.
func (w *Workspace) ApprovedMetrics() (map[string]Metric, error) {
	if g, err := w.Gate("metrics.md"); err != nil || g != "approved" {
		return nil, err
	}
	ms, err := w.Metrics()
	if err != nil {
		return nil, err
	}
	out := map[string]Metric{}
	for _, m := range ms {
		if m.Status == "approved" {
			out[ltext.TitleKey(m.Name)] = m
		}
	}
	return out, nil
}

// ProposeMetric adds a proposed row for the owner to review. since and until are the owner's to write.
func (w *Workspace) ProposeMetric(m Metric) error {
	m.Name, m.Unit = strings.TrimSpace(m.Name), strings.TrimSpace(m.Unit)
	if !ltext.ValidTitle(m.Name) {
		return refuse("metric name %q: a metric's name is its page title, and this is not a valid one (D27)", m.Name)
	}
	if m.Since != "" || m.Until != "" {
		return refuse("since and until are the owner's days: leave them empty")
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	rows, err := w.Metrics()
	if err != nil {
		return err
	}
	for _, r := range rows {
		if ltext.TitleKey(r.Name) == ltext.TitleKey(m.Name) {
			return refuse("metrics.md already has a row %s (%s)", m.Name, r.Status)
		}
	}
	text, ok, err := w.read("metrics.md")
	if err != nil {
		return err
	}
	if !ok {
		text = "status: draft\n\n" + metricsHeader
	}
	if !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	cols := headerCols(text)
	if m.Category != "" && !contains(cols, "category") {
		return refuse("metrics.md has no category column: the owner adds it to the header first")
	}
	vals := map[string]string{"status": "proposed", "name": m.Name, "unit": m.Unit, "note": m.Note, "from": m.From,
		"doubts": m.Doubts, "category": strings.TrimSpace(m.Category)}
	row := "|"
	for _, c := range cols {
		if v := vals[c]; v != "" {
			row += " " + cell(v)
		}
		row += " |"
	}
	text += row + "\n"
	return writeAtomic(w.file("metrics.md"), []byte(text))
}

// headerCols are the columns of metrics.md's table, in the order its header gives them.
func headerCols(text string) []string {
	for _, line := range strings.Split(text, "\n") {
		if cells := tableCells(line); strings.HasPrefix(strings.TrimSpace(line), "|") &&
			contains(cells, "status") && contains(cells, "name") && contains(cells, "unit") {
			return cells
		}
	}
	return metricCols
}

// approveRows turns every proposed row into an approved one.
func approveRows(rest string) string {
	lines := strings.Split(rest, "\n")
	status := -1
	for i, l := range lines {
		if !strings.HasPrefix(strings.TrimSpace(l), "|") {
			status = -1
			continue
		}
		c := tableCells(l)
		if status < 0 {
			if contains(c, "status") && contains(c, "name") && contains(c, "unit") {
				for j, col := range c {
					if col == "status" {
						status = j
						break
					}
				}
			}
			continue
		}
		if status >= len(c) || c[status] != "proposed" {
			continue
		}
		// Locate the raw cell using the same escaped-pipe grammar as tableCells,
		// retaining all other bytes, including the cell's surrounding whitespace.
		col, start := -1, 0
		for j := 0; j <= len(l); j++ {
			if j < len(l) && l[j] == '\\' && j+1 < len(l) && l[j+1] == '|' {
				j++
				continue
			}
			if j == len(l) || l[j] == '|' {
				if col == status {
					lines[i] = l[:start] + strings.Replace(l[start:j], "proposed", "approved", 1) + l[j:]
					break
				}
				col++
				start = j + 1
			}
		}
	}
	return strings.Join(lines, "\n")
}

// ---- questions.md

// Question is one doubt for the owner. Status is open, answered, done or parked; a question with an answer is
// answered even while its status line still says open.
type Question struct {
	ID       string `json:"id"`
	Status   string `json:"status"`
	File     string `json:"file,omitempty"`
	About    string `json:"about,omitempty"`
	Question string `json:"question"`
	Answer   string `json:"answer,omitempty"`
}

var questionHead = regexp.MustCompile(`^## (Q\d+) · (open|answered|done|parked)\s*$`)

func (w *Workspace) Questions() ([]Question, error) {
	text, ok, err := w.read("questions.md")
	if err != nil || !ok {
		return nil, err
	}
	var out []Question
	var cur *Question
	inAnswer := false
	for _, line := range strings.Split(text, "\n") {
		if m := questionHead.FindStringSubmatch(line); m != nil {
			out = append(out, Question{ID: m[1], Status: m[2]})
			cur, inAnswer = &out[len(out)-1], false
			continue
		}
		if cur == nil {
			continue
		}
		key, val, _ := strings.Cut(line, ":")
		switch {
		case inAnswer:
			cur.Answer = strings.TrimSpace(cur.Answer + "\n" + line)
		case key == "file":
			cur.File = strings.TrimSpace(val)
		case key == "about":
			cur.About = strings.Trim(strings.TrimSpace(val), `"`)
		case key == "question":
			cur.Question = strings.TrimSpace(val)
		case key == "answer":
			cur.Answer, inAnswer = strings.TrimSpace(val), true
		}
	}
	for i := range out {
		if out[i].Status == "open" && out[i].Answer != "" {
			out[i].Status = "answered"
		}
	}
	return out, nil
}

// Ask appends a question. The model never writes an answer: the answer line is left empty for the owner.
func (w *Workspace) Ask(q Question, found string, options []string) (string, error) {
	if q.Question == "" || len(options) < 2 {
		return "", refuse("a question needs its text and at least two options, each saying the rows it writes")
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	qs, err := w.Questions()
	if err != nil {
		return "", err
	}
	n := 0
	for _, x := range qs {
		if k, _ := strconv.Atoi(strings.TrimPrefix(x.ID, "Q")); k > n {
			n = k
		}
	}
	id := fmt.Sprintf("Q%d", n+1)
	text, _, err := w.read("questions.md")
	if err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString(text)
	if text != "" && !strings.HasSuffix(text, "\n\n") {
		b.WriteString("\n")
	}
	fmt.Fprintf(&b, "## %s · open\n", id)
	if q.File != "" {
		fmt.Fprintf(&b, "file: %s\n", oneLine(q.File))
	}
	if q.About != "" {
		fmt.Fprintf(&b, "about: %q\n", oneLine(q.About))
	}
	if found != "" {
		fmt.Fprintf(&b, "found: %s\n", oneLine(found))
	}
	fmt.Fprintf(&b, "question: %s\noptions:\n", oneLine(q.Question))
	for i, o := range options {
		fmt.Fprintf(&b, "  %c) %s\n", 'a'+i, oneLine(o))
	}
	b.WriteString("answer:\n")
	return id, writeAtomic(w.file("questions.md"), []byte(b.String()))
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// CloseQuestion marks an answered question done, once its answer is applied (guide step 8).
func (w *Workspace) CloseQuestion(id string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	qs, err := w.Questions()
	if err != nil {
		return err
	}
	for _, q := range qs {
		if q.ID != id {
			continue
		}
		if q.Answer == "" {
			return refuse("%s has no answer yet: the owner answers it", id)
		}
		text, _, _ := w.read("questions.md")
		re := regexp.MustCompile(`(?m)^## ` + id + ` · (open|answered)\s*$`)
		return writeAtomic(w.file("questions.md"), []byte(re.ReplaceAllString(text, "## "+id+" · done")))
	}
	return &core.Error{Status: 404, Msg: "no question " + id}
}

// ---- ledger.md: every source file and its state

// Line is one ledger line: State is " " (to do), "x" (done), "?" (waiting) or "-" (skipped).
type Line struct {
	State string `json:"state"`
	File  string `json:"file"`
	Note  string `json:"note,omitempty"`
}

// A ledger line is `- [state] file` with an optional note. A file name that contains the separator is
// written quoted, so the note is always what follows the first " — ".
var ledgerLine = regexp.MustCompile(`^- \[([ x?-])\] (.+)$`)

const ledgerSep = " — "

func (w *Workspace) Ledger() ([]Line, bool, error) {
	text, ok, err := w.read("ledger.md")
	if err != nil || !ok {
		return nil, ok, err
	}
	var out []Line
	for _, l := range strings.Split(text, "\n") {
		if m := ledgerLine.FindStringSubmatch(l); m != nil {
			rest, file, note := m[2], "", ""
			if strings.HasPrefix(rest, `"`) {
				q := strings.Index(rest[1:], `"`)
				if q < 0 {
					continue // a broken line is not a file
				}
				file = rest[1 : 1+q]
				if tail := rest[1+q+1:]; strings.HasPrefix(tail, ledgerSep) {
					note = tail[len(ledgerSep):]
				}
			} else if i := strings.Index(rest, ledgerSep); i >= 0 {
				file, note = rest[:i], rest[i+len(ledgerSep):]
			} else {
				file = rest
			}
			out = append(out, Line{m[1], file, note})
		}
	}
	return out, true, nil
}

func (w *Workspace) writeLedger(lines []Line) error {
	var b strings.Builder
	for _, l := range lines {
		name := l.File
		if strings.Contains(name, ledgerSep) {
			name = `"` + name + `"`
		}
		fmt.Fprintf(&b, "- [%s] %s", l.State, name)
		if l.Note != "" {
			fmt.Fprintf(&b, "%s%s", ledgerSep, l.Note)
		}
		b.WriteString("\n")
	}
	return writeAtomic(w.file("ledger.md"), []byte(b.String()))
}

// MakeLedger writes the ledger once: every file of the source, hidden folders left out, each to do.
func (w *Workspace) MakeLedger() (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if _, ok, _ := w.Ledger(); ok {
		return 0, &core.Error{Status: 409, Msg: "the ledger exists; it is never rebuilt"}
	}
	files, err := w.SourceFiles()
	if err != nil {
		return 0, err
	}
	lines := make([]Line, len(files))
	for i, f := range files {
		lines[i] = Line{" ", f, ""}
	}
	return len(lines), w.writeLedger(lines)
}

// SourceFiles lists the source's files, /-separated and NFC, hidden folders and files left out, in order.
// Two physical files that collapse to the same logical NFC path are refused before a ledger or vault plan is
// written, because that path is the import identity.
func (w *Workspace) SourceFiles() ([]string, error) {
	var out []string
	seen := map[string]string{}
	root, err := os.OpenRoot(w.Source)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	err = fs.WalkDir(root.FS(), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if strings.HasPrefix(d.Name(), ".") && p != "." {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if _, err := root.Stat(filepath.FromSlash(p)); err != nil {
			return err
		}
		rel := p
		physical := filepath.ToSlash(rel)
		logical := norm.NFC.String(physical)
		if prev, ok := seen[logical]; ok && prev != physical {
			return refuse("%s and %s have the same logical source path %s", prev, physical, logical)
		}
		seen[logical] = physical
		out = append(out, logical)
		return nil
	})
	sort.Strings(out)
	return out, err
}

// Skip marks a file the model will not import ([-] with the reason): the one ledger mark the model writes.
func (w *Workspace) Skip(file, reason string) error {
	if strings.TrimSpace(reason) == "" {
		return refuse("a skip needs its reason")
	}
	return w.mark(file, func(l *Line) error {
		if l.State != " " && l.State != "-" {
			return refuse("%s is [%s]: only a file still to do can be skipped", file, l.State)
		}
		l.State, l.Note = "-", "skip: "+oneLine(reason)
		return nil
	})
}

// mark changes one file's ledger line.
func (w *Workspace) mark(file string, f func(*Line) error) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	lines, ok, err := w.Ledger()
	if err != nil {
		return err
	}
	if !ok {
		return refuse("no ledger yet: make it first")
	}
	for i := range lines {
		if lines[i].File == file {
			if err := f(&lines[i]); err != nil {
				return err
			}
			return w.writeLedger(lines)
		}
	}
	return refuse("%s is not in the ledger", file)
}

// ledgerState is a file's state, checked before any database work ("" when it is not in the ledger).
func (w *Workspace) ledgerState(file string) (string, error) {
	lines, ok, err := w.Ledger()
	if err != nil {
		return "", err
	}
	if !ok {
		return "", refuse("no ledger yet: make it first")
	}
	for _, l := range lines {
		if l.File == file {
			return l.State, nil
		}
	}
	return "", nil
}

// ---- corrections.json: the owner's corrections of imported readings, carried to a replay

func (w *Workspace) RecordCorrection(k core.CorrectedKey) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	var all []core.CorrectedKey
	if err := readJSON(w.file("corrections.json"), &all); err != nil {
		return err
	}
	all = append(all, k)
	return writeJSON(w.file("corrections.json"), all)
}

func (w *Workspace) Corrections() ([]core.CorrectedKey, error) {
	var all []core.CorrectedKey
	return all, readJSON(w.file("corrections.json"), &all)
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }
