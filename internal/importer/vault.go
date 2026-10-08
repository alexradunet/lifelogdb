package importer

import (
	"context"
	"fmt"
	"os"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	gtext "github.com/yuin/goldmark/text"
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

// key is the import key of the note's page: its path in the vault (importKey hashes one too long to store).
func (n Note) key() string { return importKey(n.Path) }

func (w *Workspace) planPath() string { return w.file("plan.json") }

func (w *Workspace) LoadPlan() (*Plan, bool, error) {
	if !exists(w.planPath()) {
		return nil, false, nil
	}
	p := &Plan{}
	return p, true, readJSON(w.planPath(), p)
}

// NotesCount is what a source holds before it is planned: the Markdown files among all its files. Whether they
// are notes to keep as pages is the rules' word, never a guess from one application's marker (the guide's "A
// folder of notes").
type NotesCount struct {
	Markdown int `json:"markdown"`
	Files    int `json:"files"`
}

func (w *Workspace) notesCount() (*NotesCount, error) {
	files, err := w.SourceFiles()
	if err != nil {
		return nil, err
	}
	n := &NotesCount{Files: len(files)}
	for _, f := range files {
		if isNote(f) {
			n.Markdown++
		}
	}
	return n, nil
}

// isNote says whether a source file is a Markdown note; .canvas and other view files and attachments are not.
func isNote(f string) bool { return strings.HasSuffix(strings.ToLower(f), ".md") }

// PlanVault drafts plan.json against the database: every note becomes one page titled by its file name, a
// YYYY-MM-DD note is that day's page, and every problem is listed for the model to fix. Run again, it appends the
// notes added to the source since and leaves every existing entry as it is (a title or day the model fixed stays).
func (w *Workspace) PlanVault(ctx context.Context, s *core.Store) (*Plan, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	p, _, err := w.LoadPlan()
	if err != nil {
		return nil, err
	}
	if p == nil {
		p = &Plan{Notes: []Note{}}
	}
	planned := make(map[string]bool, len(p.Notes))
	for _, n := range p.Notes {
		planned[n.Path] = true
	}
	files, err := w.SourceFiles()
	if err != nil {
		return nil, err
	}
	for _, f := range files {
		if !isNote(f) || planned[f] {
			continue
		}
		title := norm.NFC.String(strings.TrimSuffix(path.Base(f), path.Ext(f)))
		n := Note{Path: f, Title: title, Action: "create"}
		if core.IsDay(title) {
			n.Day = title
		} else if src, err := w.ReadSource(f); err == nil {
			n.Day = fileDay(f, src)
			if n.Day == "" {
				if st, err := w.statSource(f); err == nil {
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
			if err := w.checkVaultIdentity(t, n); err != nil {
				return err
			}
			if n.Appended {
				continue
			}
			mine, err := t.ByImportKey(n.key())
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

func (w *Workspace) checkVaultIdentity(t *core.Tx, n *Note) error {
	r, err := w.loadVaultReceipt()
	if err != nil {
		return err
	}
	return checkVaultReceiptIdentity(t, n, r, false)
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

// applyPlan writes a checked plan; record persists the trial's plan/identity/completion
// evidence. Replay consumes verified evidence and never replaces it from target state.
func (w *Workspace) applyPlan(ctx context.Context, s *core.Store, p *Plan, record bool) (*VaultResult, error) {
	return w.applyPlanPublishing(ctx, s, p, record, publishVaultReceipt)
}

// The publisher is the narrow filesystem boundary for deterministic receipt failures.
func (w *Workspace) applyPlanPublishing(ctx context.Context, s *core.Store, p *Plan, record bool, publish func(string, *vaultIdentityReceipt) error) (*VaultResult, error) {
	src, err := w.Name()
	if err != nil {
		return nil, err
	}
	// Read every source before the first write, so a refused source leaves persistence unchanged.
	root, err := os.OpenRoot(w.Source)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	// Each note's text is final before the first write: its links are rewritten from the plan alone.
	links := linkIndex(p)
	bodies := make([]string, len(p.Notes))
	for i := range p.Notes {
		raw, err := w.readSource(root, p.Notes[i].Path)
		if err != nil {
			return nil, err
		}
		bodies[i] = rewriteLinks(raw, &p.Notes[i], links)
	}
	res := &VaultResult{}
	// 1. every page with its note's text, in one transaction: creation is one write, so an imported page is at
	// revision 1 and no edit (lifelog_meta.edit_revisions)
	var created []bool
	err = s.Do(ctx, src, func(t *core.Tx) error {
		res.Created = 0
		created = make([]bool, len(p.Notes))
		if record {
			if err := w.prepareVaultReceiptPublishing(t, p, publish); err != nil {
				return err
			}
		}
		for i := range p.Notes {
			if err := w.checkVaultIdentity(t, &p.Notes[i]); err != nil {
				return err
			}
		}
		for i, n := range p.Notes {
			if n.Action != "create" {
				continue
			}
			var day any
			if n.Day != "" {
				day = n.Day
			}
			_, existing, err := t.CreateImported(n.Title, day, bodies[i], n.key())
			if err != nil {
				return refuse("%s: %v", n.Path, err)
			}
			if !existing {
				res.Created++
				created[i] = true
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	// 2. each note's links, in its own transaction, now that every page exists, so a link between notes lands on the
	// note. SetBody writes the text only where it differs from the page's (a note changed since) and syncs the
	// links either way, so a run interrupted after step 1 is completed by the next.
	for i := range p.Notes {
		n := &p.Notes[i]
		body := bodies[i]
		err = s.Do(ctx, src, func(t *core.Tx) error {
			if n.Action == "append" {
				return appendOnce(t, n, body, res)
			}
			id, err := t.ByImportKey(n.key())
			if err != nil {
				return err
			}
			old, err := t.Body(id)
			if err != nil {
				return err
			}
			sync, err := t.SetBody(id, body)
			if err != nil {
				return err
			}
			if written := old != body || created[i] && body != ""; !written {
				res.Same++
				return nil
			}
			res.Saved++
			res.Skipped = append(res.Skipped, sync.Skipped...)
			return nil
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
	if record {
		if err := writeJSON(w.planPath(), p); err != nil {
			return res, err
		}
		if err := s.DryRun(ctx, src, func(t *core.Tx) error { return w.completeVaultReceipt(t, p, publish) }); err != nil {
			return res, err
		}
	}
	return res, nil
}

// appendOnce appends a daily note to its day page, as capture appends, unless it was appended before: by its
// record, or, should the record have been lost, by an exact body or complete trailing capture block.
// Independently identical trailing text remains indistinguishable from a lost record.
func appendOnce(t *core.Tx, n *Note, body string, res *VaultResult) error {
	if strings.TrimSpace(body) == "" {
		res.Same++
		return nil
	}
	if p, err := t.Lookup(n.Title); err != nil {
		return err
	} else if p != nil && (n.Appended || p.Body == body || strings.HasSuffix(p.Body, "\n\n"+body)) {
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
	wikiTokenRE = regexp.MustCompile(`!?\[\[([^\[\]\n]*)\]\]`)
	attachExt   = regexp.MustCompile(`(?i)\.(png|jpe?g|gif|webp|svg|bmp|pdf|mp3|wav|m4a|ogg|mp4|mov|webm|zip|docx?|xlsx?|pptx?|canvas)$`)
)

// rewriteLinks rewrites the Obsidian link forms of a note outside code: a folder, a heading, a block reference,
// a .md suffix, or a renamed target become [[Title|what was written]]; an attachment becomes a code span; a link
// to a heading of the same note becomes a code span too, so it makes no row. Every other byte is kept.
func rewriteLinks(src string, self *Note, idx map[string]*Note) string {
	// Recover code source segments only; never render or normalize Markdown.
	doc := goldmark.New().Parser().Parse(gtext.NewReader([]byte(src)))
	type span struct{ start, end int }
	var protected []span
	add := func(start, end int) {
		if end > start {
			protected = append(protected, span{start, end})
		}
	}
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch c := n.(type) {
		case *ast.CodeSpan:
			// Cover newlines and container prefixes between the inline segments too.
			if first, last := c.FirstChild(), c.LastChild(); first != nil {
				add(first.(*ast.Text).Segment.Start, last.(*ast.Text).Segment.Stop)
			}
			return ast.WalkSkipChildren, nil
		case *ast.FencedCodeBlock:
			if c.Info != nil {
				add(c.Info.Segment.Start, c.Info.Segment.Stop)
			}
			for i := 0; i < c.Lines().Len(); i++ {
				line := c.Lines().At(i)
				add(line.Start, line.Stop)
			}
			return ast.WalkSkipChildren, nil
		case *ast.CodeBlock:
			for i := 0; i < c.Lines().Len(); i++ {
				line := c.Lines().At(i)
				add(line.Start, line.Stop)
			}
			return ast.WalkSkipChildren, nil
		}
		return ast.WalkContinue, nil
	})
	sort.Slice(protected, func(i, j int) bool { return protected[i].start < protected[j].start })
	var out strings.Builder
	offset := 0
	for _, r := range protected {
		if r.start > offset {
			out.WriteString(rewriteText(src[offset:r.start], self, idx))
		}
		if r.end > offset {
			start := r.start
			if start < offset {
				start = offset
			}
			out.WriteString(src[start:r.end])
			offset = r.end
		}
	}
	out.WriteString(rewriteText(src[offset:], self, idx))
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
