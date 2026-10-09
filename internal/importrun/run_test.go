package importrun

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"lifelog/internal/api"
	"lifelog/internal/client"
	"lifelog/internal/core"
	"lifelog/internal/db"
	"lifelog/internal/importer"
)

var ctx = context.Background()

// a synthetic notes folder: every name is a placeholder
const rulesBody = "source: import:notes\n\n## Folders\n- `**/*` — notes\n"

const entitiesHeader = "| status | kind | name | as | like | from | doubts |\n|---|---|---|---|---|---|---|\n"

type fixture struct {
	ws  *importer.Workspace
	s   *core.Store
	c   *client.Client
	out bytes.Buffer
}

// newFixture makes a source folder of files, its workspace with a trial database from the schema, and the writer's
// catalog on it, in-process, as the command uses it.
func newFixture(t *testing.T, files map[string]string) *fixture {
	t.Helper()
	src := filepath.Join(t.TempDir(), "Notes")
	for p, body := range files {
		full := filepath.Join(src, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ws, err := importer.Open(src + ".lifelog")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ws.Setup(""); err != nil {
		t.Fatal(err)
	}
	d, err := db.Open(ws.TrialDB())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	s := &core.Store{DB: d}
	return &fixture{ws: ws, s: s, c: client.InProcess(api.New(s, ws), "cli")}
}

// approve is the owner's stamp, as lifelog import approve gives it at a terminal.
func (f *fixture) approve(t *testing.T, name string) {
	t.Helper()
	review, err := f.ws.Review(name)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.ws.Approve(name, time.Date(2031, 1, 1, 0, 0, 0, 0, time.UTC), review.Hash); err != nil {
		t.Fatal(err)
	}
}

func (f *fixture) rulesAndLedger(t *testing.T) {
	t.Helper()
	if err := f.ws.DraftRules(rulesBody); err != nil {
		t.Fatal(err)
	}
	f.approve(t, "rules.md")
	if _, err := f.ws.MakeLedger(); err != nil {
		t.Fatal(err)
	}
}

// names writes entities.md with these rows and stamps it.
func (f *fixture) names(t *testing.T, rows ...string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(f.ws.Dir, "entities.md"), []byte("status: draft\n\n"+entitiesHeader+strings.Join(rows, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	f.approve(t, "entities.md")
}

func (f *fixture) run(t *testing.T, m Model, o Options) *Summary {
	t.Helper()
	o.Out = &f.out
	sum, err := Run(ctx, f.c, f.ws, m, o)
	if err != nil {
		t.Fatalf("run: %v\n%s", err, f.out.String())
	}
	return sum
}

func (f *fixture) state(t *testing.T, file string) importer.Line {
	t.Helper()
	lines, _, err := f.ws.Ledger()
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range lines {
		if l.File == file {
			return l
		}
	}
	t.Fatalf("%s is not in the ledger", file)
	return importer.Line{}
}

func (f *fixture) live(t *testing.T, kind string) int {
	t.Helper()
	c, err := f.s.Counts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return c.ByType[kind]
}

// fakeModel answers by source file, and records each call: the file, the bound of the answer and the prompt.
type fakeModel struct {
	answers map[string]string
	calls   []call
}

type call struct {
	file   string
	tokens int
	user   string
}

func (m *fakeModel) Facts(_ context.Context, system, user string, tokens int) (string, error) {
	file, _, _ := strings.Cut(strings.TrimPrefix(strings.SplitN(user, "\n", 2)[0], "Source file: "), " (part ")
	m.calls = append(m.calls, call{file, tokens, user})
	if system != instructions {
		return "", errors.New("not the instructions of the facts pass")
	}
	a, ok := m.answers[file]
	if !ok {
		return "", fmt.Errorf("no answer for %s", file)
	}
	return a, nil
}

func TestRunAppliesKeepsAsTextAndStopsForNames(t *testing.T) {
	f := newFixture(t, map[string]string{
		"empty.md":  "A quiet day at home.\n",
		"metric.md": "Ferritin 48 ng/mL on 2031-04-12.\n",
		"kept.md":   "Lunch was good.\n",
		"cara.md":   "Coffee with Cara Example.\n",
	})
	f.rulesAndLedger(t)
	m := &fakeModel{answers: map[string]string{
		"empty.md": `{"file": "empty.md", "writes": []}`,
		// a reading of a metric no stamped metrics.md approves: refused, kept as text with its class
		"metric.md": `{"writes": [{"reading": {"metric": "Ferritin", "day": "2031-04-12", "value": "48 ng/mL"}, "quote": "Ferritin 48 ng/mL"}]}`,
		// thinking before the object; a kept_as_text quote that is not in the note is dropped, the other kept
		"kept.md": "<think>a meal</think>\n{\"writes\": [], \"kept_as_text\": [{\"quote\": \"Dinner was great\", \"why\": \"a meal\"}, {\"quote\": \"Lunch was good\", \"why\": \"a meal\"}]}",
		// a new name: held for the owner
		"cara.md": `{"writes": [{"person": {"title": "Cara Example"}, "quote": "Coffee with Cara Example"}]}`,
	}}
	sum := f.run(t, m, Options{})
	if sum.Applied != 3 || sum.Held != 1 || sum.Stuck != 0 || sum.Calls != 4 {
		t.Fatalf("summary %+v\n%s", sum, f.out.String())
	}
	if !strings.HasPrefix(sum.Stop, "1 names proposed in entities.md") {
		t.Errorf("stop: %q", sum.Stop)
	}
	for _, file := range []string{"empty.md", "metric.md", "kept.md"} {
		if l := f.state(t, file); l.State != "x" {
			t.Errorf("%s: %+v, want done", file, l)
		}
	}
	if l := f.state(t, "cara.md"); l.State != " " || !strings.HasPrefix(l.Note, "held:") {
		t.Errorf("cara.md: %+v, want held", l)
	}
	rows, err := f.ws.Entities()
	if err != nil || len(rows) != 1 || rows[0].Name != "Cara Example" || rows[0].Status != "proposed" {
		t.Errorf("entities.md: %+v, %v", rows, err)
	}
	out := f.out.String()
	for _, private := range []string{"Cara Example", "Ferritin", "Dinner", "Lunch"} {
		if strings.Contains(out, private) {
			t.Errorf("the progress holds %q:\n%s", private, out)
		}
	}
	if !strings.Contains(out, "refused and kept as text (metric=1)") || !strings.Contains(out, "1 kept-as-text entries dropped") {
		t.Errorf("the progress does not count the refusals:\n%s", out)
	}
	details, err := os.ReadFile(filepath.Join(f.ws.Dir, RefusedFile))
	if err != nil || !strings.Contains(string(details), "Ferritin") || !strings.Contains(string(details), "Dinner was great") {
		t.Errorf("%s: %q, %v", RefusedFile, details, err)
	}

	// the owner approves the name: the held note applies with no model call, and the run ends clean
	f.approve(t, "entities.md")
	f.out.Reset()
	again := &fakeModel{}
	sum = f.run(t, again, Options{})
	if sum.Applied != 1 || len(again.calls) != 0 || !strings.HasPrefix(sum.Stop, "every file is done; the integrity check is clean") {
		t.Fatalf("after the stamp: %+v, %d calls\n%s", sum, len(again.calls), f.out.String())
	}
	if n := f.live(t, "person"); n != 1 {
		t.Errorf("people: %d, want 1", n)
	}
}

func TestRunRetriesWithoutJSONThenLeavesTheFileAndGoesOn(t *testing.T) {
	f := newFixture(t, map[string]string{"a.md": "Rain all day.\n", "b.md": "Sun all day.\n"})
	f.rulesAndLedger(t)
	m := &fakeModel{answers: map[string]string{"a.md": "I cannot answer that.", "b.md": `{"writes": []}`}}
	sum := f.run(t, m, Options{MaxTokens: 100})
	if sum.Applied != 1 || sum.Stuck != 1 || len(m.calls) != 3 {
		t.Fatalf("summary %+v, calls %+v", sum, m.calls)
	}
	if m.calls[0].file != "a.md" || m.calls[0].tokens != 100 || m.calls[1].file != "a.md" || m.calls[1].tokens != 300 {
		t.Errorf("the retry: %+v", m.calls[:2])
	}
	if l := f.state(t, "a.md"); l.State != " " {
		t.Errorf("a.md: %+v, want still to do", l)
	}
	if !strings.Contains(sum.Stop, "This run has tried every file it can do (1 stuck)") {
		t.Errorf("stop: %q", sum.Stop)
	}
}

func TestRunStopsAtAFileTheModelCannotRead(t *testing.T) {
	f := newFixture(t, map[string]string{
		"a.csv": "Date,Steps\n2031-01-01,10\n",
		"b.md":  strings.Repeat("word ", 13000),
	})
	f.rulesAndLedger(t)
	m := &fakeModel{}
	sum := f.run(t, m, Options{})
	if len(m.calls) != 0 || !strings.HasPrefix(sum.Stop, "a.csv is a .csv file: the model reads only .md, .txt and .vcf") {
		t.Fatalf("a CSV: %+v, %d calls", sum, len(m.calls))
	}
	if _, err := f.c.Do(action(t, f, "skip-file"), map[string]string{"file": "a.csv", "reason": "skip: a table"}); err != nil {
		t.Fatal(err)
	}
	sum = f.run(t, m, Options{})
	if len(m.calls) != 0 || !strings.HasPrefix(sum.Stop, "b.md has 65000 characters") {
		t.Fatalf("a long note: %+v, %d calls", sum, len(m.calls))
	}
}

func TestRunStepsThroughAVaultAndStopsAtAJudgement(t *testing.T) {
	f := newFixture(t, map[string]string{"Journal/2031-04-11.md": "A walk by the river.\n", "Ferritin.md": "# Ferritin\n\nIron stores.\n"})
	if err := f.ws.DraftRules(rulesBody); err != nil {
		t.Fatal(err)
	}
	f.approve(t, "rules.md")
	if err := f.ws.ProposeMetric(importer.Metric{Name: "Ferritin", Unit: "ng/mL"}); err != nil {
		t.Fatal(err)
	}
	f.approve(t, "metrics.md")
	m := &fakeModel{answers: map[string]string{"Journal/2031-04-11.md": `{"writes": []}`, "Ferritin.md": `{"writes": []}`}}
	// notes or not is the rules' word, a judgement: the run stops
	sum := f.run(t, m, Options{})
	if len(m.calls) != 0 || !strings.HasPrefix(sum.Stop, "The source holds 2 Markdown files") {
		t.Fatalf("before the plan: %+v", sum)
	}
	if _, err := f.c.Do(action(t, f, "plan-vault"), nil); err != nil {
		t.Fatal(err)
	}
	sum = f.run(t, m, Options{})
	if sum.Applied != 2 || !strings.HasPrefix(sum.Stop, "every file is done") {
		t.Fatalf("after the plan: %+v\n%s", sum, f.out.String())
	}
	out := f.out.String()
	for _, step := range []string{"apply-vault: done", "register-metrics: done", "make-ledger: done"} {
		if !strings.Contains(out, step) {
			t.Errorf("the run did not say %q:\n%s", step, out)
		}
	}
	if n := f.live(t, "metric"); n != 2 { // Mood, seeded, and Ferritin
		t.Errorf("metrics: %d, want 2", n)
	}
	if n := f.live(t, "page"); n < 1 {
		t.Errorf("pages: %d, want the note pages", n)
	}
}

func TestRunStopsBeforeAVaultPlanWithAProblem(t *testing.T) {
	f := newFixture(t, map[string]string{"A/Same.md": "one\n", "B/Same.md": "two\n"})
	if err := f.ws.DraftRules(rulesBody); err != nil {
		t.Fatal(err)
	}
	f.approve(t, "rules.md")
	if _, err := f.c.Do(action(t, f, "plan-vault"), nil); err != nil {
		t.Fatal(err)
	}
	sum := f.run(t, &fakeModel{}, Options{})
	if !strings.HasPrefix(sum.Stop, "the vault plan lists 2 problems") {
		t.Fatalf("stop: %q", sum.Stop)
	}
	if n := f.live(t, "page"); n != 0 {
		t.Errorf("pages: %d, want none before the plan is fixed", n)
	}
}

func TestRunTombstonesARejectedName(t *testing.T) {
	f := newFixture(t, map[string]string{"dana.md": "Tea with Dana Sample.\n"})
	f.rulesAndLedger(t)
	f.names(t, "| proposed | person | Dana Sample | | | | |")
	m := &fakeModel{answers: map[string]string{"dana.md": `{"writes": [{"person": {"title": "Dana Sample"}, "quote": "Tea with Dana Sample"}]}`}}
	if sum := f.run(t, m, Options{}); sum.Applied != 1 || f.live(t, "person") != 1 {
		t.Fatalf("first run: %+v, people %d", sum, f.live(t, "person"))
	}
	f.names(t, "| rejected | person | Dana Sample | | | | |")
	f.out.Reset()
	sum := f.run(t, &fakeModel{}, Options{})
	if !strings.Contains(f.out.String(), "tombstone-rejected: done") || !strings.HasPrefix(sum.Stop, "every file is done") {
		t.Fatalf("after the rejection: %+v\n%s", sum, f.out.String())
	}
	if n := f.live(t, "person"); n != 0 {
		t.Errorf("live people: %d, want 0", n)
	}
}

func TestRunSendsAVCardInBatches(t *testing.T) {
	var vcf strings.Builder
	for i := range 40 {
		fmt.Fprintf(&vcf, "BEGIN:VCARD\r\nVERSION:3.0\r\nFN:Placeholder %02d\r\nNOTE:%s\r\nEND:VCARD\r\n", i, strings.Repeat("x", 120))
	}
	f := newFixture(t, map[string]string{"contacts.vcf": vcf.String()})
	f.rulesAndLedger(t)
	m := &fakeModel{answers: map[string]string{"contacts.vcf": `{"writes": []}`}}
	sum := f.run(t, m, Options{})
	if sum.Applied != 1 || len(m.calls) != 2 {
		t.Fatalf("summary %+v, %d calls", sum, len(m.calls))
	}
	for i, c := range m.calls {
		_, text, _ := strings.Cut(c.user, "<<<FILE\n")
		if !strings.Contains(c.user, fmt.Sprintf("(part %d of 2: a batch of its contact cards)", i+1)) || !strings.HasPrefix(text, "BEGIN:VCARD") {
			t.Errorf("call %d: not a batch of whole cards:\n%.200s", i+1, c.user)
		}
	}
	if !strings.Contains(m.calls[0].user, "Placeholder 00") || !strings.Contains(m.calls[1].user, "Placeholder 39") {
		t.Error("the batches do not hold every card")
	}
}

func TestCardsKeepEveryCardWhole(t *testing.T) {
	long := "BEGIN:VCARD\nFN:A\nNOTE:" + strings.Repeat("y", cardBatch) + "\nEND:VCARD\n"
	for _, c := range []struct {
		name, text string
		want       int
	}{
		{"one short card", "BEGIN:VCARD\nFN:A\nEND:VCARD\n", 1},
		{"a card longer than a batch stays whole", long + "BEGIN:VCARD\nFN:B\nEND:VCARD\n", 2},
		{"words before the first card", "x\nBEGIN:VCARD\nFN:A\nEND:VCARD\n", 1},
	} {
		got := cards(c.text)
		if len(got) != c.want || strings.Join(got, "") != c.text {
			t.Errorf("%s: %d batches, want %d; joined equal: %v", c.name, len(got), c.want, strings.Join(got, "") == c.text)
		}
	}
}

func action(t *testing.T, f *fixture, name string) api.Action {
	t.Helper()
	actions, err := f.c.Catalog()
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range actions {
		if a.Name == name {
			return a
		}
	}
	t.Fatalf("no action %s", name)
	return api.Action{}
}
