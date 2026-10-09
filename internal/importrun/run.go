package importrun

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"lifelog/internal/api"
	"lifelog/internal/client"
	"lifelog/internal/importer"
)

// instructions is the system prompt of the facts pass: what a facts object is, and the fixed rules of decision for
// a small local model (the guide's "The procedure for the model", step 6).
//
//go:embed facts-system.md
var instructions string

// The bounds of the facts pass, as the owner-local loop first used them with a 2-bit model.
const (
	maxText       = 60000 // characters of one file in one call; a .vcf goes in batches of cards
	cardBatch     = 6000  // characters of contact cards in one call
	maxChecks     = 10    // write and check rounds of one file before it is left to do
	statusEvery   = 10    // files the model reads between two status checks: status dry-runs every done file
	defaultTokens = 4000
)

// readable are the kinds of file the model reads as text. Any other stops the run: a CSV goes through a prepared
// profile, a PDF or a picture through the later pass, and none of them reaches the model as text.
var readable = map[string]bool{".md": true, ".txt": true, ".vcf": true}

// RefusedFile, in the workspace, keeps the details of each write the writer refused and of each kept_as_text entry
// dropped: names and quotes, for the owner. The progress on Out never holds them.
const RefusedFile = "run-refused.md"

// Options are the bounds of one run.
type Options struct {
	Max       int       // the most files the model reads in this run; 0 is no bound
	Files     []string  // only these files, whatever their ledger state: their facts are written again
	MaxTokens int       // the bound of one answer; 0 is 4000, and a retry has three times as much
	Out       io.Writer // the progress: counts and file paths, never a name or a quote
}

// Summary is what one run did, and why it stopped.
type Summary struct {
	Applied int    `json:"applied"`
	Held    int    `json:"held"`
	Stuck   int    `json:"stuck"`
	Calls   int    `json:"model_calls"`
	Stop    string `json:"stop"`
}

type runner struct {
	ctx     context.Context
	c       *client.Client
	ws      *importer.Workspace
	m       Model
	o       Options
	actions map[string]api.Action
	tried   map[string]bool // the files this run worked on: each one once
	read    int             // the files the model read
	sum     Summary
}

// stopped is the end of a run that is not an error: a gate, a judgement or a bound; its message says what to do.
type stopped struct{ msg string }

func (s *stopped) Error() string { return s.msg }

func stop(format string, a ...any) error { return &stopped{fmt.Sprintf(format, a...)} }

// Run follows import status and does each step that needs no judgement; the facts of a file come from m. Every
// write goes through c, the writer's catalog; ws only reads the source and the rules, and keeps RefusedFile.
func Run(ctx context.Context, c *client.Client, ws *importer.Workspace, m Model, o Options) (*Summary, error) {
	if o.MaxTokens <= 0 {
		o.MaxTokens = defaultTokens
	}
	if o.Out == nil {
		o.Out = io.Discard
	}
	r := &runner{ctx: ctx, c: c, ws: ws, m: m, o: o, tried: map[string]bool{}, actions: map[string]api.Action{}}
	actions, err := c.CatalogContext(ctx)
	if err != nil {
		return nil, err
	}
	for _, a := range actions {
		r.actions[a.Name] = a
	}
	if len(o.Files) > 0 {
		err = r.only()
	} else {
		err = r.follow()
	}
	var s *stopped
	if errors.As(err, &s) {
		r.sum.Stop, err = s.msg, nil
		r.say("stop: %s", s.msg)
	}
	r.say("end: %d applied, %d held for names, %d stuck, %d model calls", r.sum.Applied, r.sum.Held, r.sum.Stuck, r.sum.Calls)
	return &r.sum, err
}

// follow is one turn after another: status, then what its step asks for.
func (r *runner) follow() error {
	last := ""
	for {
		if err := r.ctx.Err(); err != nil {
			return err
		}
		st, err := r.status()
		if err != nil {
			return err
		}
		if st.Step != "facts" && st.Step+"\n"+st.DoNow == last {
			return stop("the step %s did not change the status: %s", st.Step, st.DoNow)
		}
		last = st.Step + "\n" + st.DoNow
		switch st.Step {
		case "move-name-lines":
			err = r.step("propose-entities")
		case "tombstone-rejected":
			err = r.step("tombstone-rejected")
		case "apply-vault":
			var n int
			if n, err = r.planProblems(); err == nil && n > 0 {
				return stop("the vault plan lists %d problems: fix them (fix-plan), then run again", n)
			}
			if err == nil {
				err = r.step("apply-vault")
			}
		case "make-ledger":
			// after apply-vault, so a metric that a note is about adopts the note's page (D27)
			if st.Gates["metrics.md"] == "approved" {
				err = r.step("register-metrics")
			}
			if err == nil {
				err = r.step("make-ledger")
			}
		case "facts":
			var n int
			if n, err = r.batch(); err == nil && n == 0 {
				return stop("%s This run has tried every file it can do (%d stuck).", st.DoNow, r.sum.Stuck)
			}
			if err == nil && r.o.Max > 0 && r.read >= r.o.Max {
				return stop("the run read its %d files (--max)", r.o.Max)
			}
		case "propose-names":
			e, err := r.do("propose-entities", nil)
			if err != nil {
				return err
			}
			var res struct {
				Proposed []json.RawMessage `json:"proposed"`
			}
			if err := decode(e.Result, &res); err != nil {
				return err
			}
			return stop("%d names proposed in entities.md: decide them, approve (lifelog import approve entities), then run again", len(res.Proposed))
		case "done":
			e, err := r.do("integrity-check", nil)
			if err != nil {
				return err
			}
			var res struct {
				OK bool `json:"ok"`
			}
			if err := decode(e.Properties, &res); err != nil {
				return err
			}
			if !res.OK {
				return stop("every file is done, but the integrity check failed: lifelog do integrity-check --workspace W")
			}
			return stop("every file is done; the integrity check is clean: %s", ledgerCounts(st.Ledger))
		default:
			return stop("%s", st.DoNow)
		}
		if err != nil {
			return err
		}
	}
}

// only does the facts of the files the owner named, whatever their ledger state.
func (r *runner) only() error {
	for _, f := range r.o.Files {
		if r.tried[f] {
			continue
		}
		r.tried[f] = true
		if err := r.file(f); err != nil {
			return err
		}
	}
	return stop("the named files are done")
}

// batch works the ledger: first each file held for names, with no model call (its names may be decided now), then
// up to statusEvery files to do, in ledger order. It returns how many files it worked on.
func (r *runner) batch() (int, error) {
	lines, err := r.ledger()
	if err != nil {
		return 0, err
	}
	worked := 0
	for _, l := range lines {
		if l.State != " " || !strings.HasPrefix(l.Note, "held:") || r.tried[l.File] {
			continue
		}
		r.tried[l.File] = true
		worked++
		if err := r.applyHeld(l.File); err != nil {
			return worked, err
		}
	}
	read := 0
	for _, l := range lines {
		if read >= statusEvery || (r.o.Max > 0 && r.read >= r.o.Max) {
			break
		}
		if l.State != " " || strings.HasPrefix(l.Note, "held:") || r.tried[l.File] {
			continue
		}
		r.tried[l.File] = true
		worked++
		read++
		if err := r.file(l.File); err != nil {
			return worked, err
		}
	}
	return worked, nil
}

func (r *runner) applyHeld(file string) error {
	_, err := r.do("apply-facts", map[string]string{"file": file})
	var ce *client.Error
	switch {
	case err == nil:
		r.sum.Applied++
		r.say("APPLIED %s (its names are decided)", file)
	case errors.As(err, &ce) && ce.Status == 422:
		r.say("HELD %s (its names still wait for the owner)", file)
	default:
		return r.failed(file, err)
	}
	return nil
}

// file does the facts of one source file: the model's answer, then write, check and apply.
func (r *runner) file(file string) error {
	ext := strings.ToLower(path.Ext(file))
	if !readable[ext] {
		return stop("%s is a %s file: the model reads only .md, .txt and .vcf. Prepare it (draft-prepared), skip it (skip-file) or hold it for a later pass (defer-file), then run again", file, ext)
	}
	text, err := r.ws.ReadSource(file)
	if err != nil {
		r.sum.Stuck++
		r.say("STUCK %s (the source file cannot be read)", file)
		return nil
	}
	if n := utf8.RuneCountInString(text); ext != ".vcf" && n > maxText {
		return stop("%s has %d characters, more than one model call reads (%d): skip it, hold it for later, or split it, then run again", file, n, maxText)
	}
	r.read++
	facts, err := r.factsOf(file, ext, text)
	if err != nil {
		return err
	}
	if facts == nil {
		r.sum.Stuck++
		r.say("[%d] STUCK %s (no JSON from the model)", r.read, file)
		return nil
	}
	return r.writeCheckApply(file, text, facts)
}

var thinkRE = regexp.MustCompile(`(?s)<think>.*?</think>`)

// factsOf asks the model for the facts of one file; a .vcf goes in batches of cards, and their facts are merged. A
// nil object is no JSON: the model was asked twice, the second time with three times the bound of its answer.
func (r *runner) factsOf(file, ext, text string) (map[string]any, error) {
	rules, err := r.rules()
	if err != nil {
		return nil, err
	}
	day := ""
	if m := dayRE.FindStringSubmatch(file); m != nil {
		day = m[1]
	}
	parts := []string{text}
	if ext == ".vcf" && len(text) > cardBatch {
		parts = cards(text)
	}
	writes, kept := []any{}, []any{}
	for i, p := range parts {
		user := prompt(file, day, rules, p, i+1, len(parts))
		var obj map[string]any
		for _, tokens := range []int{r.o.MaxTokens, 3 * r.o.MaxTokens} {
			r.sum.Calls++
			answer, err := r.m.Facts(r.ctx, instructions, user, tokens)
			if err != nil {
				return nil, fmt.Errorf("the model: %w", err)
			}
			if obj = parseFacts(answer); obj != nil {
				break
			}
		}
		if obj == nil {
			return nil, nil
		}
		writes = append(writes, list(obj["writes"])...)
		kept = append(kept, list(obj["kept_as_text"])...)
	}
	return map[string]any{"file": file, "writes": writes, "kept_as_text": kept}, nil
}

var dayRE = regexp.MustCompile(`(\d{4}-\d{2}-\d{2})\.md$`)

func prompt(file, day, rules, text string, part, parts int) string {
	var b strings.Builder
	b.WriteString("Source file: " + file)
	if parts > 1 {
		fmt.Fprintf(&b, " (part %d of %d: a batch of its contact cards)", part, parts)
	}
	if day == "" {
		day = "none from its name (a date: line in its frontmatter counts)"
	}
	b.WriteString("\nIts own day: " + day + "\n\nThe rules of this source:\n" + rules)
	b.WriteString("\n\nThe file's text, between the markers:\n<<<FILE\n" + text + "\nFILE>>>\n\nReturn the facts JSON object for this file.")
	return b.String()
}

// cards splits a vCard file into batches of whole cards, each at most cardBatch characters unless one card is longer.
func cards(text string) []string {
	var out []string
	cur := ""
	for _, c := range splitBefore(text, "BEGIN:VCARD") {
		if strings.TrimSpace(c) == "" {
			continue
		}
		if cur != "" && len(cur)+len(c) > cardBatch {
			out = append(out, cur)
			cur = ""
		}
		cur += c
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}

// splitBefore cuts text before each sep after its first byte: "a S b S c" is "a ", "S b ", "S c".
func splitBefore(text, sep string) []string {
	var out []string
	for len(text) > 1 {
		i := strings.Index(text[1:], sep)
		if i < 0 {
			break
		}
		out = append(out, text[:i+1])
		text = text[i+1:]
	}
	return append(out, text)
}

// parseFacts is the JSON object of an answer: the text from its first { to its last }, with any thinking left out.
func parseFacts(answer string) map[string]any {
	answer = thinkRE.ReplaceAllString(answer, "")
	a, b := strings.Index(answer, "{"), strings.LastIndex(answer, "}")
	if a < 0 || b <= a {
		return nil
	}
	dec := json.NewDecoder(strings.NewReader(answer[a : b+1]))
	dec.UseNumber()
	var obj map[string]any
	if dec.Decode(&obj) != nil {
		return nil
	}
	return obj
}

func list(v any) []any {
	l, _ := v.([]any)
	return l
}

// writeCheckApply stores the facts, checks them and applies them. Each write the check refuses is kept as text with
// its class, except a held name and a write that waits for one: the owner decides those. Each kept_as_text entry
// whose quote is not in the file is dropped, since it would refuse the whole file.
func (r *runner) writeCheckApply(file, text string, facts map[string]any) error {
	refused := map[string]int{}
	dropped, err := r.dropMissingKept(file, text, facts)
	if err != nil {
		return err
	}
	for try := 1; try <= maxChecks; try++ {
		data, err := json.Marshal(facts)
		if err != nil {
			return err
		}
		if _, err := r.do("write-facts", map[string]string{"file": file, "facts": string(data)}); err != nil {
			return r.failed(file, err)
		}
		e, err := r.do("check-facts", map[string]string{"file": file})
		if err != nil {
			return r.failed(file, err)
		}
		var rep importer.Report
		if err := decode(e.Properties, &rep); err != nil {
			return err
		}
		held := map[string]bool{}
		for _, x := range rep.Refused {
			if x.Class == "held" {
				held[x.What] = true
			}
		}
		var drop []importer.Refusal
		for _, x := range rep.Refused {
			if x.Class != "held" && !(x.Class == "not_yet" && held[x.Waits]) {
				drop = append(drop, x)
			}
		}
		if len(drop) == 0 {
			_, err := r.do("apply-facts", map[string]string{"file": file})
			var ce *client.Error
			switch {
			case err == nil:
				r.sum.Applied++
				r.say("[%d] DONE %s %s%s", r.read, file, written(facts), extras(refused, dropped))
			case len(held) > 0 && errors.As(err, &ce) && ce.Status == 422:
				r.sum.Held++
				r.say("[%d] HELD %s: %d names wait for the owner (entities.md)%s", r.read, file, len(held), extras(refused, dropped))
			default:
				return r.failed(file, err)
			}
			return nil
		}
		if err := r.dropWrites(file, facts, drop, refused); err != nil {
			return err
		}
		n, err := r.dropMissingKept(file, text, facts)
		if err != nil {
			return err
		}
		dropped += n
	}
	r.sum.Stuck++
	r.say("[%d] STUCK %s (still refused after %d checks)", r.read, file, maxChecks)
	return nil
}

// dropWrites moves each refused write into kept_as_text with its class and reason, and records it for the owner.
func (r *runner) dropWrites(file string, facts map[string]any, drop []importer.Refusal, refused map[string]int) error {
	writes := list(facts["writes"])
	gone := map[int]bool{}
	var notes []string
	for _, x := range drop {
		k := x.Write - 1
		if k < 0 || k >= len(writes) || gone[k] {
			continue
		}
		gone[k] = true
		quote := ""
		if w, ok := writes[k].(map[string]any); ok {
			quote, _ = w["quote"].(string)
		}
		facts["kept_as_text"] = append(list(facts["kept_as_text"]), map[string]any{"quote": quote, "why": fmt.Sprintf("refused by the writer (%s): %s", x.Class, x.Reason)})
		refused[x.Class]++
		line := fmt.Sprintf("%s | write %d (%s %s) | %s | %s", file, x.Write, x.Kind, x.What, x.Class, x.Reason)
		if len(x.Candidates) > 0 {
			line += " | candidates: " + strings.Join(x.Candidates, "; ")
		}
		notes = append(notes, line)
	}
	kept := []any{}
	for i, w := range writes {
		if !gone[i] {
			kept = append(kept, w)
		}
	}
	facts["writes"] = kept
	return r.note(notes...)
}

// dropMissingKept takes out each kept_as_text entry without a quote and a why, or whose quote is not in the file,
// and records it for the owner. It returns how many it took out.
func (r *runner) dropMissingKept(file, text string, facts map[string]any) (int, error) {
	entries := list(facts["kept_as_text"])
	quotes := make([]string, len(entries))
	bad := map[int]bool{}
	for i, e := range entries {
		m, _ := e.(map[string]any)
		q, _ := m["quote"].(string)
		why, _ := m["why"].(string)
		quotes[i] = q
		if strings.TrimSpace(q) == "" || strings.TrimSpace(why) == "" {
			bad[i] = true
		}
	}
	for _, i := range importer.KeptQuotesMissing(text, quotes) {
		bad[i] = true
	}
	if len(bad) == 0 {
		return 0, nil
	}
	keep := []any{}
	var notes []string
	for i, e := range entries {
		if bad[i] {
			notes = append(notes, fmt.Sprintf("%s | kept_as_text %d dropped: no quote or why, or the quote is not in the file | %s", file, i+1, quotes[i]))
			continue
		}
		keep = append(keep, e)
	}
	facts["kept_as_text"] = keep
	return len(bad), r.note(notes...)
}

// failed turns a refusal of the writer into a stuck file, with its class only; any other error ends the run.
func (r *runner) failed(file string, err error) error {
	var ce *client.Error
	if !errors.As(err, &ce) {
		return err
	}
	code := "refused"
	if p, ok := ce.Entity.Properties.(map[string]any); ok {
		if c, _ := p["code"].(string); c != "" {
			code = c
		}
	}
	r.sum.Stuck++
	r.say("[%d] STUCK %s (the writer refused it: %s)", r.read, file, code)
	return nil
}

// written counts the facts' writes by kind.
func written(facts map[string]any) string {
	kinds := map[string]int{}
	writes := list(facts["writes"])
	for _, w := range writes {
		if m, ok := w.(map[string]any); ok {
			for _, k := range []string{"person", "place", "link", "reading", "page"} {
				if _, ok := m[k]; ok {
					kinds[k]++
				}
			}
		}
	}
	return fmt.Sprintf("%d writes (%s), %d kept as text", len(writes), counts(kinds), len(list(facts["kept_as_text"])))
}

func extras(refused map[string]int, dropped int) string {
	s := ""
	if n := sum(refused); n > 0 {
		s += fmt.Sprintf(", %d refused and kept as text (%s)", n, counts(refused))
	}
	if dropped > 0 {
		s += fmt.Sprintf(", %d kept-as-text entries dropped", dropped)
	}
	if s != "" {
		s += "; details in " + RefusedFile
	}
	return s
}

func counts(m map[string]int) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = fmt.Sprintf("%s=%d", k, m[k])
	}
	return strings.Join(parts, " ")
}

func sum(m map[string]int) int {
	n := 0
	for _, v := range m {
		n += v
	}
	return n
}

func ledgerCounts(l map[string]int) string {
	return fmt.Sprintf("%d done, %d waiting, %d later, %d skipped", l["done"], l["waiting"], l["later"], l["skipped"])
}

// note appends lines to RefusedFile in the workspace: the owner's file, private like the workspace itself.
func (r *runner) note(lines ...string) error {
	if len(lines) == 0 {
		return nil
	}
	f, err := os.OpenFile(filepath.Join(r.ws.Dir, RefusedFile), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	stamp := time.Now().Format(time.DateTime)
	var b bytes.Buffer
	for _, l := range lines {
		b.WriteString(stamp + " " + l + "\n")
	}
	if _, err := f.Write(b.Bytes()); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// rules is the text of the workspace's rules.md without its status line: the model reads the folder lines and the
// decisions.
func (r *runner) rules() (string, error) {
	b, err := os.ReadFile(filepath.Join(r.ws.Dir, "rules.md"))
	if err != nil {
		return "", err
	}
	text := strings.ReplaceAll(string(b), "\r\n", "\n")
	if first, rest, ok := strings.Cut(text, "\n"); ok && strings.HasPrefix(first, "status:") {
		text = rest
	}
	return text, nil
}

func (r *runner) status() (*importer.Status, error) {
	e, err := r.c.GetContext(r.ctx, "/import")
	if err != nil {
		return nil, err
	}
	var st importer.Status
	return &st, decode(e.Properties, &st)
}

func (r *runner) ledger() ([]importer.Line, error) {
	e, err := r.c.GetContext(r.ctx, "/import/ledger")
	if err != nil {
		return nil, err
	}
	var l struct {
		Files []importer.Line `json:"files"`
	}
	return l.Files, decode(e.Properties, &l)
}

func (r *runner) planProblems() (int, error) {
	e, err := r.c.GetContext(r.ctx, "/import/plan")
	if err != nil {
		return 0, err
	}
	var p importer.Plan
	if err := decode(e.Properties, &p); err != nil {
		return 0, err
	}
	n := 0
	for _, note := range p.Notes {
		n += len(note.Problems)
	}
	return n, nil
}

// step runs one action that needs no judgement and says so.
func (r *runner) step(name string) error {
	if _, err := r.do(name, nil); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	r.say("%s: done", name)
	return nil
}

func (r *runner) do(name string, values map[string]string) (*api.Entity, error) {
	a, ok := r.actions[name]
	if !ok {
		return nil, fmt.Errorf("the catalog has no action %q", name)
	}
	return r.c.DoContext(r.ctx, a, values)
}

func (r *runner) say(format string, a ...any) {
	fmt.Fprintln(r.o.Out, time.Now().Format(time.TimeOnly), fmt.Sprintf(format, a...))
}

// decode reads a generic JSON value (an entity's properties or result) into a typed one.
func decode(v, out any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}
