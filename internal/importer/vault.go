package importer

import (
	"context"
	"fmt"
	"os"
	"path"
	"regexp"
	"strings"
	"time"

	"golang.org/x/text/unicode/norm"

	"lifelog/internal/core"
	"lifelog/internal/text"
)

// Plan is plan.json: every note of a vault, its page's title and day, and what applying it does
// (the guide's "An Obsidian vault"). The writer drafts it; the model may change only titles and days.
type Plan struct {
	Notes []Note `json:"notes"`
}

// Note is one note. Action is "create" (its own page) or "append" (a daily note whose day page already exists:
// its text is appended to it once). Appended records the append, written by the writer.
type Note struct {
	Path     string   `json:"path"`
	Title    string   `json:"title"`
	Day      string   `json:"day,omitempty"`
	Action   string   `json:"action"`
	Appended bool     `json:"appended,omitempty"`
	Problems []string `json:"problems,omitempty"`
}

func (w *Workspace) planPath() string { return w.file("plan.json") }

func (w *Workspace) LoadPlan() (*Plan, bool, error) {
	if !exists(w.planPath()) {
		return nil, false, nil
	}
	p := &Plan{}
	return p, true, readJSON(w.planPath(), p)
}

// IsVault says whether the source is a vault: it has an .obsidian folder, or a plan was made.
func (w *Workspace) IsVault() bool {
	return exists(w.Source+"/.obsidian") || exists(w.planPath())
}

// PlanVault drafts plan.json against the database: every note becomes one page titled by its file name, a
// YYYY-MM-DD note is that day's page, and every problem is listed for the model to fix.
func (w *Workspace) PlanVault(ctx context.Context, s *core.Store) (*Plan, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if exists(w.planPath()) {
		return nil, &core.Error{Status: 409, Msg: "plan.json exists: fix it with fix-plan, it is never redrafted"}
	}
	files, err := w.SourceFiles()
	if err != nil {
		return nil, err
	}
	p := &Plan{Notes: []Note{}}
	for _, f := range files {
		if !strings.HasSuffix(strings.ToLower(f), ".md") {
			continue // .canvas and other view files, attachments: not notes
		}
		title := norm.NFC.String(strings.TrimSuffix(path.Base(f), path.Ext(f)))
		n := Note{Path: f, Title: title, Action: "create"}
		if core.IsDay(title) {
			n.Day = title
		} else if src, err := w.ReadSource(f); err == nil {
			n.Day = fileDay(f, src)
			if n.Day == "" {
				if st, err := os.Stat(w.Source + "/" + f); err == nil {
					n.Day = st.ModTime().Format(time.DateOnly)
				}
			}
		}
		p.Notes = append(p.Notes, n)
	}
	if err := w.validatePlan(ctx, s, p); err != nil {
		return nil, err
	}
	return p, writeJSON(w.planPath(), p)
}

// validatePlan recomputes every note's action and problems.
func (w *Workspace) validatePlan(ctx context.Context, s *core.Store, p *Plan) error {
	src, err := w.Name()
	if err != nil {
		return err
	}
	byKey := map[string][]int{}
	for i := range p.Notes {
		byKey[text.TitleKey(p.Notes[i].Title)] = append(byKey[text.TitleKey(p.Notes[i].Title)], i)
	}
	return s.DryRun(ctx, src, func(t *core.Tx) error {
		for i := range p.Notes {
			n := &p.Notes[i]
			n.Problems = nil
			if !text.ValidTitle(n.Title) {
				n.Problems = append(n.Problems, "the title is not valid (docs/contract/titles-and-wikilinks.md): change it")
			}
			if n.Day != "" && !core.IsDay(n.Day) {
				n.Problems = append(n.Problems, "the day is not YYYY-MM-DD")
			}
			if core.IsDay(n.Title) && n.Day != n.Title {
				n.Problems = append(n.Problems, "a title that is a day is that day's page: its day is its title")
			}
			if len(byKey[text.TitleKey(n.Title)]) > 1 {
				n.Problems = append(n.Problems, "another note has the same title: change one")
			}
			if n.Appended {
				continue
			}
			mine, err := t.ByImportKey(n.Path)
			if err != nil {
				return err
			}
			held, err := t.Lookup(n.Title)
			if err != nil {
				return err
			}
			switch {
			case mine != 0:
				n.Action = "create" // created by this import before
			case held != nil && held.DayPage && core.IsDay(n.Title):
				n.Action = "append"
			case held != nil:
				n.Action = "create"
				n.Problems = append(n.Problems, fmt.Sprintf("the database already has a %s titled %q: change the title, or ask the owner", held.Type, held.Title))
			default:
				n.Action = "create"
			}
		}
		return nil
	})
}

// FixPlan changes one note's title and day: the only fields the model may edit.
func (w *Workspace) FixPlan(ctx context.Context, s *core.Store, notePath, title, day string) (*Plan, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	p, ok, err := w.LoadPlan()
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, refuse("no plan.json yet: plan the vault first")
	}
	found := false
	for i := range p.Notes {
		if p.Notes[i].Path == notePath {
			if p.Notes[i].Appended {
				return nil, refuse("%s was appended already", notePath)
			}
			if title != "" {
				p.Notes[i].Title = norm.NFC.String(strings.TrimSpace(title))
			}
			if day != "" {
				p.Notes[i].Day = day
			}
			found = true
		}
	}
	if !found {
		return nil, refuse("%s is not a note of the plan", notePath)
	}
	if err := w.validatePlan(ctx, s, p); err != nil {
		return nil, err
	}
	return p, writeJSON(w.planPath(), p)
}

// VaultResult is what applying a vault plan did.
type VaultResult struct {
	Created  int      `json:"pages_created"`
	Saved    int      `json:"bodies_saved"`
	Appended int      `json:"notes_appended"`
	Same     int      `json:"unchanged"`
	Skipped  []string `json:"wikilinks_skipped,omitempty"`
}

// ApplyVault writes the plan: every page first (keyed by its note's path), then each note's text in its own
// transaction through the save contract, so a link between notes lands on the note. An append note is
// appended once to its day page.
func (w *Workspace) ApplyVault(ctx context.Context, s *core.Store) (*VaultResult, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	p, ok, err := w.LoadPlan()
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, refuse("no plan.json yet: plan the vault first")
	}
	if g, _ := w.Gate("rules.md"); g != "approved" {
		return nil, refuse("rules.md is %s: the owner approves it first", g)
	}
	if err := w.validatePlan(ctx, s, p); err != nil {
		return nil, err
	}
	for _, n := range p.Notes {
		if len(n.Problems) > 0 {
			return nil, refuse("the plan has problems (%s: %s): fix them first", n.Path, n.Problems[0])
		}
	}
	return w.applyPlan(ctx, s, p, true)
}

// applyPlan writes a checked plan into a database; record says whether appends are recorded in plan.json
// (the trial does, a replay reads them).
func (w *Workspace) applyPlan(ctx context.Context, s *core.Store, p *Plan, record bool) (*VaultResult, error) {
	src, err := w.Name()
	if err != nil {
		return nil, err
	}
	res := &VaultResult{}
	// 1. every page, in one transaction
	err = s.Do(ctx, src, func(t *core.Tx) error {
		res.Created = 0
		for _, n := range p.Notes {
			if n.Action != "create" {
				continue
			}
			var day any
			if n.Day != "" {
				day = n.Day
			}
			_, existing, err := t.CreateImported(n.Title, day, n.Path)
			if err != nil {
				return refuse("%s: %v", n.Path, err)
			}
			if !existing {
				res.Created++
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	links := linkIndex(p)
	// 2. each body, in its own transaction
	for i := range p.Notes {
		n := &p.Notes[i]
		raw, err := w.ReadSource(n.Path)
		if err != nil {
			return res, err
		}
		body := rewriteLinks(raw, n, links)
		err = s.Do(ctx, src, func(t *core.Tx) error {
			if n.Action == "append" {
				return appendOnce(t, n, body, res)
			}
			id, err := t.ByImportKey(n.Path)
			if err != nil {
				return err
			}
			old, err := t.Body(id)
			if err != nil {
				return err
			}
			if old == body {
				res.Same++
				return nil
			}
			sync, err := t.SetBody(id, body)
			res.Saved++
			res.Skipped = append(res.Skipped, sync.Skipped...)
			return err
		})
		if err != nil {
			return res, refuse("%s: %v", n.Path, err)
		}
		if record && n.Action == "append" && !n.Appended {
			n.Appended = true
			if err := writeJSON(w.planPath(), p); err != nil {
				return res, err
			}
		}
	}
	return res, nil
}

// appendOnce appends a daily note to its day page, as capture appends, unless it was appended before: by its
// record, or, should the record have been lost, by the page already ending with the text.
func appendOnce(t *core.Tx, n *Note, body string, res *VaultResult) error {
	if strings.TrimSpace(body) == "" {
		res.Same++
		return nil
	}
	if p, err := t.Lookup(n.Title); err != nil {
		return err
	} else if p != nil && (n.Appended || strings.Contains(p.Body, body)) {
		res.Same++
		return nil
	}
	_, sync, err := t.Capture(n.Title, body, nil)
	res.Appended++
	res.Skipped = append(res.Skipped, sync.Skipped...)
	return err
}

// ---- Obsidian's link forms (the guide: rewritten before the save so a reader sees the same words)

// linkIndex maps what an Obsidian link may name to a note: its path without .md, and its base name, case-folded.
func linkIndex(p *Plan) map[string]*Note {
	idx := map[string]*Note{}
	for i := range p.Notes {
		n := &p.Notes[i]
		noExt := strings.TrimSuffix(n.Path, path.Ext(n.Path))
		idx[text.TitleKey(noExt)] = n
		base := text.TitleKey(path.Base(noExt))
		if _, taken := idx[base]; !taken {
			idx[base] = n // Obsidian resolves a bare name to one note
		}
	}
	return idx
}

var (
	fenceRE     = regexp.MustCompile("^ {0,3}(```+|~~~+)")
	wikiTokenRE = regexp.MustCompile(`!?\[\[([^\[\]\n]*)\]\]`)
	attachExt   = regexp.MustCompile(`(?i)\.(png|jpe?g|gif|webp|svg|bmp|pdf|mp3|wav|m4a|ogg|mp4|mov|webm|zip|docx?|xlsx?|pptx?|canvas)$`)
)

// rewriteLinks rewrites the Obsidian link forms of a note outside code: a folder, a heading, a block reference,
// a .md suffix, or a renamed target become [[Title|what was written]]; an attachment becomes a code span; a link
// to a heading of the same note becomes a code span too, so it makes no row. Every other byte is kept.
func rewriteLinks(src string, self *Note, idx map[string]*Note) string {
	lines := strings.SplitAfter(src, "\n")
	var out strings.Builder
	fence := ""
	for _, line := range lines {
		if m := fenceRE.FindStringSubmatch(line); m != nil {
			switch {
			case fence == "":
				fence = m[1][:3]
			case strings.HasPrefix(m[1], fence):
				fence = ""
			}
			out.WriteString(line)
			continue
		}
		if fence != "" || strings.HasPrefix(line, "    ") || strings.HasPrefix(line, "\t") {
			out.WriteString(line)
			continue
		}
		out.WriteString(rewriteOutsideCodeSpans(line, self, idx))
	}
	return out.String()
}

func rewriteOutsideCodeSpans(line string, self *Note, idx map[string]*Note) string {
	var out strings.Builder
	for len(line) > 0 {
		i := strings.IndexByte(line, '`')
		if i < 0 {
			out.WriteString(rewriteText(line, self, idx))
			break
		}
		out.WriteString(rewriteText(line[:i], self, idx))
		n := 1
		for i+n < len(line) && line[i+n] == '`' {
			n++
		}
		ticks := line[i : i+n]
		end := strings.Index(line[i+n:], ticks)
		if end < 0 {
			out.WriteString(line[i:])
			break
		}
		out.WriteString(line[i : i+n+end+n])
		line = line[i+n+end+n:]
	}
	return out.String()
}

func rewriteText(s string, self *Note, idx map[string]*Note) string {
	return wikiTokenRE.ReplaceAllStringFunc(s, func(tok string) string {
		embed := strings.HasPrefix(tok, "!")
		inner := wikiTokenRE.FindStringSubmatch(tok)[1]
		target, alias, hasAlias := strings.Cut(inner, "|")
		target = strings.TrimSpace(target)
		shown := inner
		if hasAlias {
			shown = alias
		}
		if attachExt.MatchString(target) || embed {
			return "`" + tok + "`" // an attachment is not part of the vault: it is kept on its own as a file (D9)
		}
		name, anchor, hasAnchor := strings.Cut(target, "#")
		if hasAnchor && strings.TrimSpace(name) == "" {
			return "`" + tok + "`" // a heading of this same note: no row
		}
		name = strings.TrimSuffix(name, ".md")
		note := idx[text.TitleKey(strings.TrimSpace(name))]
		if note == nil {
			note = idx[text.TitleKey(path.Base(strings.TrimSpace(name)))]
		}
		title := path.Base(strings.TrimSpace(name))
		if note != nil {
			title = note.Title
		}
		plainForm := !hasAnchor && !strings.Contains(name, "/") && !strings.HasSuffix(target, ".md") && title == strings.TrimSpace(name)
		if plainForm {
			return tok
		}
		_ = anchor
		return "[[" + title + "|" + shown + "]]"
	})
}
