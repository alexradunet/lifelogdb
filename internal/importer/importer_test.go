package importer

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"lifelog/internal/core"
	"lifelog/internal/db"
)

var ctx = context.Background()

// a synthetic vault: every name is a placeholder
var vault = map[string]string{
	".obsidian/app.json":     "{}",
	"Journal/2031-04-11.md":  "Swam at Riverside Pool with Bobby.\nmood: 4\n\nSee [[Contacts/Bob Sample|Bob]] and [[Recipes#Bread]].\n",
	"Journal/2031-04-12.md":  "Coffee with Cara. Next month we try the lake.\n",
	"Contacts/Bob Sample.md": "Bob Sample, a friend from school. ![[photo.png]]\n",
	"Medical/Ferritin.md":    "# Ferritin\n\n| date | value |\n|---|---|\n| 2031-03-01 | 48 ng/mL |\n| 2031-04-01 | 52 ng/mL |\n",
	"Recipes.md":             "## Bread\n\nflour, water `[[not a link]]`\n",
	"Medical/Twice.md":       "2031-05-01 morning: 48 ng/mL\n2031-05-01 evening: 52 ng/mL\n",
	"photo.png":              "\x89PNG\x00\x00",
	"Notes/A — B.png":        "\x89PNG\x00\x00",
}

const rulesBody = `source: import:notebook

## Folders
- Journal/*.md — one note per day

## Aliases
- "Bobby" → "Bob Sample"

## Distinct
`

type fixture struct {
	w     *Workspace
	s     *core.Store
	trial string
}

func setup(t *testing.T) *fixture {
	t.Helper()
	root := t.TempDir()
	src := filepath.Join(root, "Notebook")
	for p, body := range vault {
		full := filepath.Join(src, filepath.FromSlash(p))
		os.MkdirAll(filepath.Dir(full), 0o755)
		os.WriteFile(full, []byte(body), 0o644)
	}
	w, err := Open(src + ".lifelog")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Setup(""); err != nil {
		t.Fatal(err)
	}
	d, err := db.Open(w.TrialDB())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return &fixture{w, &core.Store{DB: d}, w.TrialDB()}
}

func (f *fixture) approveRules(t *testing.T, body string) {
	t.Helper()
	if err := f.w.DraftRules(body); err != nil {
		t.Fatal(err)
	}
	if err := ownerApproves(f.w, "rules.md"); err != nil {
		t.Fatal(err)
	}
}

func (f *fixture) metrics(t *testing.T) {
	t.Helper()
	for _, m := range []Metric{{Name: "ferritin", Unit: "ng/mL", Note: "Ferritin (blood)"}, {Name: "mood", Note: "1-5"}} {
		if err := f.w.ProposeMetric(m); err != nil {
			t.Fatal(err)
		}
	}
	if err := ownerApproves(f.w, "metrics.md"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.w.RegisterMetrics(ctx, f.s); err != nil {
		t.Fatal(err)
	}
}

func (f *fixture) facts(t *testing.T, file string, v any) error {
	t.Helper()
	b, _ := json.Marshal(v)
	return f.w.WriteFacts(file, b)
}

func code(err error) int {
	var e *core.Error
	if errors.As(err, &e) {
		return e.Status
	}
	return 0
}

var dayFacts = map[string]any{
	"file": "Journal/2031-04-11.md",
	"writes": []any{
		map[string]any{"place": map[string]any{"title": "Riverside Pool"}, "quote": "Swam at Riverside Pool"},
		map[string]any{"person": map[string]any{"title": "Bob Sample"}, "quote": "with Bobby"},
		map[string]any{"page": map[string]any{"title": "2031-04-11"}, "quote": "Swam at Riverside Pool"},
		map[string]any{"link": map[string]any{"from": "2031-04-11", "to": "Riverside Pool", "kind": "at"}, "quote": "Swam at Riverside Pool"},
		map[string]any{"link": map[string]any{"from": "2031-04-11", "to": "Bob Sample", "kind": "about"}, "quote": "with Bobby"},
		map[string]any{"reading": map[string]any{"metric": "mood", "day": "2031-04-11", "value": "4"}, "quote": "mood: 4"},
	},
}

func TestGates(t *testing.T) {
	f := setup(t)
	if g, _ := f.w.Gate("rules.md"); g != "missing" {
		t.Errorf("no rules: %s", g)
	}
	if err := f.w.DraftRules("status: approved\n" + rulesBody); err == nil {
		t.Error("a model wrote a status line")
	}
	f.approveRules(t, rulesBody)
	if g, _ := f.w.Gate("rules.md"); g != "approved" {
		t.Errorf("after approve: %s", g)
	}
	p := filepath.Join(f.w.Dir, "rules.md")
	b, _ := os.ReadFile(p)
	os.WriteFile(p, append(b, []byte("- \"Bob\" → \"Bob Sample\"\n")...), 0o644)
	if g, _ := f.w.Gate("rules.md"); g != "stale" {
		t.Errorf("an edit after approval leaves the gate %s", g)
	}
}

func TestLedger(t *testing.T) {
	f := setup(t)
	n, err := f.w.MakeLedger()
	if err != nil || n != 8 {
		t.Fatalf("ledger: %d files, %v", n, err)
	}
	// issue 0002: a name holding the ledger's own " — " separator survives the round trip
	if err := f.w.Skip("Notes/A — B.png", "an attachment — with — dashes in its name"); err != nil {
		t.Fatalf("skip a name with an em dash: %v", err)
	}
	lines, _, err := f.w.Ledger()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, l := range lines {
		if l.File == "Notes/A — B.png" {
			found = true
			if l.State != "-" || l.Note != "skip: an attachment — with — dashes in its name" {
				t.Errorf("the em-dash name was cut: %+v", l)
			}
		}
	}
	if !found {
		t.Error("the em-dash file is not in the ledger")
	}
	if _, err := f.w.MakeLedger(); code(err) != 409 {
		t.Errorf("a second ledger: %v", err)
	}
	if err := f.w.Skip("photo.png", "attachment"); err != nil {
		t.Fatal(err)
	}
	f.approveRules(t, rulesBody)
	if err := f.facts(t, "photo.png", map[string]any{"file": "photo.png", "writes": []any{}}); err == nil {
		t.Error("facts for a skipped file")
	}
	if _, err := f.w.SourcePath("../secret.md"); err == nil {
		t.Error("a path out of the source")
	}
	if _, err := f.w.SourcePath(".obsidian/app.json"); err == nil {
		t.Error("a hidden folder")
	}
}

func TestApplyIsIdempotentAndChecked(t *testing.T) {
	f := setup(t)
	f.approveRules(t, rulesBody)
	f.w.MakeLedger()
	f.metrics(t)
	if err := f.facts(t, "Journal/2031-04-11.md", dayFacts); err != nil {
		t.Fatal(err)
	}
	r, err := f.w.Check(ctx, f.s, "Journal/2031-04-11.md")
	if err != nil {
		t.Fatal(err)
	}
	if r.Summary != "1 page, 1 place, 1 person, 2 link, 1 reading (6 new)" {
		t.Errorf("check: %s", r.Summary)
	}
	if c, _ := f.s.Counts(ctx); c.Pages != 0 {
		t.Errorf("check wrote %d pages", c.Pages)
	}
	if r, err = f.w.Apply(ctx, f.s, "Journal/2031-04-11.md"); err != nil {
		t.Fatal(err)
	}
	lines, _, _ := f.w.Ledger()
	for _, l := range lines {
		if l.File == "Journal/2031-04-11.md" && (l.State != "x" || !strings.Contains(l.Note, "6 new")) {
			t.Errorf("ledger line %+v", l)
		}
	}
	if r, err = f.w.Apply(ctx, f.s, "Journal/2031-04-11.md"); err != nil || r.Summary != "1 page, 1 place, 1 person, 2 link, 1 reading (6 existing)" {
		t.Errorf("a second apply: %v, %v", r, err)
	}
	st, err := f.w.Status(ctx, f.s, f.trial)
	if err != nil || len(st.Mismatches) != 0 {
		t.Errorf("status: %+v, %v", st, err)
	}
}

func TestRefusals(t *testing.T) {
	f := setup(t)
	f.approveRules(t, rulesBody)
	f.w.MakeLedger()
	f.metrics(t)
	s := f.s
	s.Do(ctx, "cli", func(tx *core.Tx) error { _, _, err := tx.CreatePerson("Cara Example", "", "", "", ""); return err })
	file := "Journal/2031-04-12.md"
	one := func(w map[string]any) map[string]any { return map[string]any{"file": file, "writes": []any{w}} }
	cases := []struct {
		name  string
		facts map[string]any
		want  string
	}{
		{"quote not in the file", one(map[string]any{"person": map[string]any{"title": "Cara"}, "quote": "tea with Cara"}), "not in"},
		{"quote inside a word", one(map[string]any{"place": map[string]any{"title": "ake"}, "quote": "ake"}), "not in"},
		{"an alias as a title", one(map[string]any{"person": map[string]any{"title": "Bobby"}, "quote": "Coffee with Cara"}), "alias"},
		{"an event", one(map[string]any{"event": map[string]any{"title": "coffee"}, "quote": "Coffee with Cara"}), "D22"},
		{"a task", one(map[string]any{"task": map[string]any{"title": "lake"}, "quote": "Next month we try the lake"}), "D23"},
		{"two kinds", one(map[string]any{"person": map[string]any{"title": "Cara"}, "place": map[string]any{"title": "Cara"}, "quote": "Coffee with Cara"}), "exactly one kind"},
		{"a question number", one(map[string]any{"person": map[string]any{"title": "Cara Q4"}, "quote": "Coffee with Cara"}), "question or row number"},
		{"a look-alike", one(map[string]any{"person": map[string]any{"title": "Cara"}, "quote": "Coffee with Cara"}), "looks like"},
		{"an unapproved metric", one(map[string]any{"reading": map[string]any{"metric": "caffeine", "day": "2031-04-12", "value": "1"}, "quote": "Coffee with Cara"}), "not approved"},
		{"a wikilink", one(map[string]any{"link": map[string]any{"from": "2031-04-12", "to": "Cara Example", "kind": "wikilink"}, "quote": "Coffee with Cara"}), "wikilink"},
		{"a reference not written", one(map[string]any{"link": map[string]any{"from": "2031-04-12", "to": "Cara", "kind": "about"}, "quote": "Coffee with Cara"}), "not written yet"},
	}
	for _, c := range cases {
		if err := f.facts(t, file, c.facts); err != nil {
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("%s: refused on write: %v", c.name, err)
			}
			continue
		}
		if _, err := f.w.Check(ctx, s, file); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v (want %q)", c.name, err, c.want)
		}
	}
	if err := f.w.WriteFacts(file, []byte(`{"file": "`+file+`", "writes": [], "colour": "red"}`)); err == nil {
		t.Error("an unknown field was accepted")
	}
	// a distinct line lets the second Cara through
	f.approveRules(t, rulesBody+`- "Cara" ≠ "Cara Example"`+"\n")
	f.facts(t, file, one(map[string]any{"person": map[string]any{"title": "Cara"}, "quote": "Coffee with Cara"}))
	if _, err := f.w.Check(ctx, s, file); err != nil {
		t.Errorf("a distinct name: %v", err)
	}
}

func TestReadingsKeysAndReplay(t *testing.T) {
	f := setup(t)
	f.approveRules(t, rulesBody)
	f.w.MakeLedger()
	f.metrics(t)
	file := "Medical/Ferritin.md"
	r1 := map[string]any{"reading": map[string]any{"metric": "ferritin", "day": "2031-03-01", "value": "48 ng/mL"}, "quote": "| 2031-03-01 | 48 ng/mL |"}
	r2 := map[string]any{"reading": map[string]any{"metric": "ferritin", "day": "2031-04-01", "value": "52 ng/mL"}, "quote": "| 2031-04-01 | 52 ng/mL |"}
	f.facts(t, file, map[string]any{"file": file, "writes": []any{r1, r2}})
	if _, err := f.w.Apply(ctx, f.s, file); err != nil {
		t.Fatal(err)
	}
	// the same facts in another order are the same rows: the keys come from the source, not the facts file
	f.facts(t, file, map[string]any{"file": file, "writes": []any{r2, r1}})
	if r, err := f.w.Apply(ctx, f.s, file); err != nil || !strings.Contains(r.Summary, "2 existing") {
		t.Errorf("reordered facts: %v, %v", r, err)
	}
	// two readings of one metric on one day: told apart by where their quotes stand in the source file
	twice := "Medical/Twice.md"
	m1 := map[string]any{"reading": map[string]any{"metric": "ferritin", "day": "2031-05-01", "value": "48 ng/mL"}, "quote": "2031-05-01 morning: 48 ng/mL"}
	m2 := map[string]any{"reading": map[string]any{"metric": "ferritin", "day": "2031-05-01", "value": "52 ng/mL"}, "quote": "2031-05-01 evening: 52 ng/mL"}
	f.facts(t, twice, map[string]any{"file": twice, "writes": []any{m1, m2}})
	if _, err := f.w.Apply(ctx, f.s, twice); err != nil {
		t.Fatal(err)
	}
	f.facts(t, twice, map[string]any{"file": twice, "writes": []any{m2, m1}})
	if r, err := f.w.Check(ctx, f.s, twice); err != nil || !strings.Contains(r.Summary, "2 existing") {
		t.Errorf("two readings of a day, reordered: %v, %v", r, err)
	}
	bad := map[string]any{"reading": map[string]any{"metric": "ferritin", "day": "2031-03-01", "value": "48 mg/L"}, "quote": "| 2031-03-01 | 48 ng/mL |"}
	f.facts(t, file, map[string]any{"file": file, "writes": []any{bad}})
	if _, err := f.w.Check(ctx, f.s, file); err == nil {
		t.Error("a value not in its quote, or a unit not the metric's, was accepted")
	}
	f.facts(t, file, map[string]any{"file": file, "writes": []any{r1, r2}})

	// the owner corrects one reading on the trial; the replay makes the correction again
	var id int64
	f.s.Do(ctx, "import:notebook", func(tx *core.Tx) (err error) {
		id, err = tx.MeasurementByKey("import:notebook", "ferritin", "Medical/Ferritin.md|reading|ferritin|2031-03-01|1")
		return
	})
	v := 47.0
	if _, key, err := f.s.Correct(ctx, "cli", id, &v); err != nil {
		t.Fatal(err)
	} else if err := f.w.RecordCorrection(key); err != nil {
		t.Fatal(err)
	}
	// the corrected reading still checks as existing: apply compares with the value it first wrote
	if r, err := f.w.Check(ctx, f.s, file); err != nil || !strings.Contains(r.Summary, "2 existing") {
		t.Errorf("after a correction: %v, %v", r, err)
	}

	f.facts(t, "Journal/2031-04-11.md", dayFacts)
	if _, err := f.w.Apply(ctx, f.s, "Journal/2031-04-11.md"); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "life.db")
	res, err := f.w.Replay(ctx, f.s, target)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Initialised || len(res.Differences) != 0 || !res.Integrity.OK || res.Corrections != 1 {
		t.Errorf("replay: %+v", res)
	}
	res, err = f.w.Replay(ctx, f.s, target)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range res.Files {
		if strings.Contains(r.Summary, "new") {
			t.Errorf("a second replay wrote %s: %s", r.File, r.Summary)
		}
	}
	if res.Corrections != 0 || len(res.Differences) != 0 {
		t.Errorf("a second replay: %+v", res)
	}
}

// issue 0004: an about link to a note's page, applied before the file that makes that page a person, is an ordering
// error: applied alone it is refused with what to do; the replay applies it after the rest, whatever the ledger order.
// A link to a page that no file ever promotes still fails, and so does an end of a type no write can make fit.
func TestLinkBeforePromotion(t *testing.T) {
	f := setup(t)
	f.approveRules(t, rulesBody)
	f.w.MakeLedger()
	if _, err := f.w.PlanVault(ctx, f.s); err != nil {
		t.Fatal(err)
	}
	if _, err := f.w.ApplyVault(ctx, f.s); err != nil {
		t.Fatal(err)
	}
	day, bob := "Journal/2031-04-11.md", "Contacts/Bob Sample.md"
	linkTo := func(to string) map[string]any {
		return map[string]any{"file": day, "writes": []any{
			map[string]any{"link": map[string]any{"from": "2031-04-11", "to": to, "kind": "about"}, "quote": "Swam at Riverside Pool with Bobby"}}}
	}
	if err := f.facts(t, day, linkTo("Bob Sample")); err != nil {
		t.Fatal(err)
	}
	if err := f.facts(t, bob, map[string]any{"file": bob, "writes": []any{
		map[string]any{"person": map[string]any{"title": "Bob Sample"}, "quote": "Bob Sample, a friend from school"}}}); err != nil {
		t.Fatal(err)
	}

	// applied alone, before Bob's own file: refused as an ordering error that says what to do
	_, err := f.w.Apply(ctx, f.s, day)
	if !waitsForAnother(err) || code(err) != 422 || !strings.Contains(err.Error(), `"Bob Sample" is still a plain page`) {
		t.Fatalf("the about link before the person's file: %v", err)
	}
	if _, err := f.w.Apply(ctx, f.s, bob); err != nil {
		t.Fatal(err)
	}
	if r, err := f.w.Apply(ctx, f.s, day); err != nil || r.Summary != "1 link (1 new)" {
		t.Fatalf("after the person's file: %v, %v", r, err)
	}

	// the ledger lists the day before Bob's file: the replay applies the day after the rest
	lines, _, err := f.w.Ledger()
	if err != nil {
		t.Fatal(err)
	}
	var ordered []Line
	var bobLine Line
	for _, l := range lines {
		if l.File == bob {
			bobLine = l
			continue
		}
		ordered = append(ordered, l)
	}
	if err := f.w.writeLedger(append(ordered, bobLine)); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "life.db")
	res, err := f.w.Replay(ctx, f.s, target)
	if err != nil {
		t.Fatalf("replay with the day ledgered first: %v", err)
	}
	if len(res.Files) != 2 || res.Files[0].File != bob || len(res.Differences) != 0 || !res.Integrity.OK {
		t.Errorf("replay: %+v", res)
	}
	if res, err = f.w.Replay(ctx, f.s, target); err != nil {
		t.Fatal(err)
	}
	for _, r := range res.Files {
		if strings.Contains(r.Summary, "new") || strings.Contains(r.Summary, "promoted") {
			t.Errorf("a second replay wrote %s: %s", r.File, r.Summary)
		}
	}

	// a page that no file promotes: refused alone, and the replay stops on it at the end, naming it
	if err := f.facts(t, day, linkTo("Recipes")); err != nil {
		t.Fatal(err)
	}
	if _, err := f.w.Check(ctx, f.s, day); !waitsForAnother(err) {
		t.Errorf("a link to a page no file promotes: %v", err)
	}
	if _, err := f.w.Replay(ctx, f.s, filepath.Join(t.TempDir(), "life.db")); err == nil || !strings.Contains(err.Error(), `"Recipes" is still a plain page`) {
		t.Errorf("the replay of a link to a page no file promotes: %v", err)
	}

	// a day page never becomes a person: the registry's own refusal, never retried
	if err := f.facts(t, day, linkTo("2031-04-12")); err != nil {
		t.Fatal(err)
	}
	if _, err := f.w.Check(ctx, f.s, day); err == nil || waitsForAnother(err) || !strings.Contains(err.Error(), "endpoint type not allowed") {
		t.Errorf("an about link to a day page: %v", err)
	}
}

func TestVault(t *testing.T) {
	f := setup(t)
	f.approveRules(t, rulesBody)
	// the day page of the 12th exists already: its note is appended to it, once
	if _, _, err := f.s.Capture(ctx, "cli", "2031-04-12", "Written on the day.", nil); err != nil {
		t.Fatal(err)
	}
	p, err := f.w.PlanVault(ctx, f.s)
	if err != nil {
		t.Fatal(err)
	}
	actions := map[string]string{}
	for _, n := range p.Notes {
		actions[n.Path] = n.Action
		if len(n.Problems) > 0 {
			t.Errorf("%s: %v", n.Path, n.Problems)
		}
	}
	if len(p.Notes) != 6 || actions["Journal/2031-04-12.md"] != "append" || actions["Recipes.md"] != "create" {
		t.Errorf("plan: %+v", p.Notes)
	}
	for i := 0; i < 2; i++ {
		res, err := f.w.ApplyVault(ctx, f.s)
		if err != nil {
			t.Fatal(err)
		}
		if i == 1 && (res.Created != 0 || res.Saved != 0 || res.Appended != 0) {
			t.Errorf("a second apply wrote %+v", res)
		}
	}
	id, _ := f.s.PageID(ctx, "2031-04-12")
	day, _ := f.s.PageByID(ctx, id)
	if day.Body != "Written on the day.\n\nCoffee with Cara. Next month we try the lake.\n" {
		t.Errorf("the appended day page: %q", day.Body)
	}
	id, _ = f.s.PageID(ctx, "2031-04-11")
	day, _ = f.s.PageByID(ctx, id)
	if !strings.Contains(day.Body, "[[Bob Sample|Bob]]") || !strings.Contains(day.Body, "[[Recipes|Recipes#Bread]]") {
		t.Errorf("rewritten links: %q", day.Body)
	}
	var out []string
	for _, e := range day.Out {
		out = append(out, e.Title)
	}
	if strings.Join(out, ",") != "Bob Sample,Recipes" {
		t.Errorf("the day's links land on the notes: %v", out)
	}
	id, _ = f.s.PageID(ctx, "Bob Sample")
	bob, _ := f.s.PageByID(ctx, id)
	if !strings.Contains(bob.Body, "`![[photo.png]]`") {
		t.Errorf("an embed is not a code span: %q", bob.Body)
	}
}

func TestRewriteLinks(t *testing.T) {
	p := &Plan{Notes: []Note{{Path: "Notes/Old name.md", Title: "New name"}, {Path: "B.md", Title: "B"}}}
	idx := linkIndex(p)
	for in, want := range map[string]string{
		"[[B]]":                          "[[B]]",
		"[[B|bee]]":                      "[[B|bee]]",
		"[[B.md]]":                       "[[B|B.md]]",
		"[[Notes/Old name]]":             "[[New name|Notes/Old name]]",
		"[[Old name#Part|see]]":          "[[New name|see]]",
		"[[B#^block]]":                   "[[B|B#^block]]",
		"[[#Heading]]":                   "`[[#Heading]]`",
		"![[pic.png]]":                   "`![[pic.png]]`",
		"[[doc.pdf]]":                    "`[[doc.pdf]]`",
		"`[[B.md]]` and [[B.md]]":        "`[[B.md]]` and [[B|B.md]]",
		"```\n[[B.md]]\n```\n[[B.md]]\n": "```\n[[B.md]]\n```\n[[B|B.md]]\n",
		"[[Unknown/Thing]]":              "[[Thing|Unknown/Thing]]",
	} {
		if got := rewriteLinks(in, &p.Notes[1], idx); got != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}
}
