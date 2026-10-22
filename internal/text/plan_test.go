package text

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"strings"
	"testing"
)

const planContract = "../../docs/contract/plan-lines.md"

// planVector is one printed vector of the contract: a body and the plan lines it holds.
type planVector struct {
	Body  string
	Plans []struct {
		Line     int
		Box      string
		Text     string
		On       string
		Due      string
		Done     string
		Every    string
		FromDone bool `json:"from_done"`
		Bad      string
	}
}

type nextVector struct {
	On, Due, Every string
	FromDone       bool `json:"from_done"`
	Done           string
	NextOn         string `json:"next_on"`
	NextDue        string `json:"next_due"`
	NextEvery      string `json:"next_every"`
	Error          bool
}

// vectorsAfter cuts the JSON block that follows a marker comment of the contract page.
func vectorsAfter(t *testing.T, marker string, into any) {
	t.Helper()
	raw, err := os.ReadFile(planContract)
	if err != nil {
		t.Fatal(err)
	}
	page := strings.ReplaceAll(string(raw), "\r\n", "\n")
	_, rest, ok := strings.Cut(page, marker)
	if !ok {
		t.Fatalf("marker %s not found", marker)
	}
	_, rest, ok = strings.Cut(rest, "```json\n")
	if !ok {
		t.Fatalf("no JSON block after %s", marker)
	}
	block, _, ok := strings.Cut(rest, "```")
	if !ok {
		t.Fatalf("unterminated JSON block after %s", marker)
	}
	if err := json.Unmarshal([]byte(block), into); err != nil {
		t.Fatal(err)
	}
}

func TestPlanLineVectors(t *testing.T) {
	var vs []planVector
	vectorsAfter(t, "<!-- plan-line-vectors -->", &vs)
	if len(vs) < 40 {
		t.Fatalf("only %d plan-line vectors, want at least 40", len(vs))
	}
	for _, v := range vs {
		got := Plans(v.Body)
		if len(got) != len(v.Plans) {
			t.Errorf("body %q: %d plan lines, want %d", v.Body, len(got), len(v.Plans))
			continue
		}
		for i, w := range v.Plans {
			g := got[i]
			check := func(field string, got, want any) {
				if got != want {
					t.Errorf("body %q, plan %d, %s = %#v, want %#v", v.Body, i, field, got, want)
				}
			}
			check("line", g.Line, w.Line)
			check("box", string(g.Box), w.Box)
			check("text", g.Text, w.Text)
			check("on", g.On, w.On)
			check("due", g.Due, w.Due)
			check("done", g.Done, w.Done)
			check("every", g.Every, w.Every)
			check("from_done", g.FromDone, w.FromDone)
			check("bad", g.Bad, w.Bad)
		}
	}
}

func TestNextDayVectors(t *testing.T) {
	var vs []nextVector
	vectorsAfter(t, "<!-- next-day-vectors -->", &vs)
	if len(vs) < 25 {
		t.Fatalf("only %d next-day vectors, want at least 25", len(vs))
	}
	for _, v := range vs {
		p := Plan{On: v.On, Due: v.Due, Every: v.Every, FromDone: v.FromDone}
		on, due, every, err := Next(p, v.Done)
		if v.Error {
			if err == nil {
				t.Errorf("Next(%+v, %s) = %q %q %q, want an error", v, v.Done, on, due, every)
			}
			continue
		}
		if err != nil {
			t.Errorf("Next(%+v) failed: %v", v, err)
			continue
		}
		if on != v.NextOn || due != v.NextDue || every != v.NextEvery {
			t.Errorf("Next(on %q, due %q, every %q, from_done %v, done %s) = %q %q %q, want %q %q %q",
				v.On, v.Due, v.Every, v.FromDone, v.Done, on, due, every, v.NextOn, v.NextDue, v.NextEvery)
		}
	}
}

type reopenVector struct {
	Body, Text, After, Removed, Error string
	Line                              int
}

func TestReopenVectors(t *testing.T) {
	var vs []reopenVector
	vectorsAfter(t, "<!-- reopen-vectors -->", &vs)
	if len(vs) < 14 {
		t.Fatalf("only %d reopen vectors, want at least 14", len(vs))
	}
	errs := map[string]error{"changed": ErrLineChanged, "carried": ErrCarried, "malformed": ErrMalformed}
	for _, v := range vs {
		got, err := Reopen(v.Body, v.Line, v.Text)
		if v.Error != "" {
			if !errors.Is(err, errs[v.Error]) {
				t.Errorf("Reopen(%q, %d) error = %v, want %s", v.Body, v.Line, err, v.Error)
			}
			continue
		}
		if err != nil || got.Body != v.After || got.Removed != v.Removed {
			t.Errorf("Reopen(%q, %d) = %q, %q, %v, want %q, %q", v.Body, v.Line, got.Body, got.Removed, err, v.After, v.Removed)
		}
	}
}

type scheduleVector struct {
	Body, Text, On, Due, After, Error string
	Line                              int
}

func TestScheduleVectors(t *testing.T) {
	var vs []scheduleVector
	vectorsAfter(t, "<!-- schedule-vectors -->", &vs)
	if len(vs) < 12 {
		t.Fatalf("only %d schedule vectors, want at least 12", len(vs))
	}
	for _, v := range vs {
		got, err := Schedule(v.Body, v.Line, v.Text, v.On, v.Due)
		switch v.Error {
		case "":
			if err != nil || got != v.After {
				t.Errorf("Schedule(%q, %d, on %q, due %q) = %q, %v, want %q", v.Body, v.Line, v.On, v.Due, got, err, v.After)
			}
		case "changed":
			if !errors.Is(err, ErrLineChanged) {
				t.Errorf("Schedule(%q, %d) error = %v, want changed", v.Body, v.Line, err)
			}
		case "malformed":
			if !errors.Is(err, ErrMalformed) {
				t.Errorf("Schedule(%q, %d) error = %v, want malformed", v.Body, v.Line, err)
			}
		default: // invalid: any other refusal
			if err == nil || errors.Is(err, ErrLineChanged) || errors.Is(err, ErrMalformed) || got != "" {
				t.Errorf("Schedule(%q, %d, on %q, due %q) = %q, %v, want an invalid refusal", v.Body, v.Line, v.On, v.Due, got, err)
			}
		}
	}
}

// A tick, then a reopen, gives back the body for every open line of the plan-line vectors, with the next line of
// a repeat appended as the writer does.
func TestReopenUndoesTick(t *testing.T) {
	var vs []planVector
	vectorsAfter(t, "<!-- plan-line-vectors -->", &vs)
	n := 0
	for _, v := range vs {
		for _, p := range Plans(v.Body) {
			if p.Box != ' ' || p.Bad != "" || p.Text == "" {
				continue
			}
			ticked, next, err := Tick(v.Body, p.Line, p.Text, 'x', "2026-10-22")
			if err != nil {
				continue
			}
			if next != "" {
				ticked += "\n\n" + next
			}
			got, err := Reopen(ticked, p.Line, p.Text)
			if err != nil || got.Body != v.Body {
				t.Errorf("body %q line %d: tick then reopen gave %q, %v", v.Body, p.Line, got.Body, err)
			}
			n++
		}
	}
	if n < 10 {
		t.Fatalf("only %d round trips", n)
	}
}

func TestLine(t *testing.T) {
	ok := []struct {
		text, on, due, every string
		from                 bool
		want                 string
	}{
		{"Call the bank", "", "", "", false, "- [ ] Call the bank"},
		{"Pay  rent", "2026-10-22", "2026-10-24", "month", true, "- [ ] Pay rent on:2026-10-22 due:2026-10-24 every:month from:done"},
		{"Run", "", "2026-10-22", "weekday", false, "- [ ] Run due:2026-10-22 every:weekday"},
	}
	for _, c := range ok {
		got, err := Line(c.text, c.on, c.due, c.every, c.from)
		if err != nil || got != c.want {
			t.Errorf("Line(%q) = %q, %v, want %q", c.text, got, err, c.want)
			continue
		}
		ps := Plans(got)
		if len(ps) != 1 || ps[0].Box != ' ' || ps[0].On != c.on || ps[0].Due != c.due || ps[0].Every != c.every || ps[0].FromDone != c.from || ps[0].Bad != "" {
			t.Errorf("Line(%q) = %q does not read back: %+v", c.text, got, ps)
		}
	}
	bad := []struct {
		name, text, on, due, every string
		from                       bool
	}{
		{"empty text", "", "2026-10-22", "", "", false},
		{"only spaces", "   ", "", "", "", false},
		{"line break", "a\nb", "", "", "", false},
		{"carriage return", "a\rb", "", "", "", false},
		{"token in the text", "a on:2026-10-22", "", "", "", false},
		{"bad day", "a", "2026-02-30", "", "", false},
		{"bad repeat", "a", "2026-10-22", "", "1-days", false},
		{"every without a day", "a", "", "", "week", false},
		{"from:done without every", "a", "2026-10-22", "", "", true},
	}
	for _, c := range bad {
		if got, err := Line(c.text, c.on, c.due, c.every, c.from); err == nil {
			t.Errorf("%s: Line gave %q, want an error", c.name, got)
		}
	}
}

func TestTick(t *testing.T) {
	body := "Week\r\n\r\n- [ ] pay rent on:2026-01-31 every:month\r\n- [ ] other\r\n"
	got, next, err := Tick(body, 3, "pay rent", 'x', "2026-01-30")
	want := "Week\r\n\r\n- [x] pay rent on:2026-01-31 every:month done:2026-01-30\r\n- [ ] other\r\n"
	if err != nil || got != want {
		t.Fatalf("Tick = %q, %v, want %q", got, err, want)
	}
	if wantNext := "- [ ] pay rent on:2026-02-28 every:month-31"; next != wantNext {
		t.Errorf("appended = %q, want %q", next, wantNext)
	}

	// done without every adds nothing; the done token goes after the last token, and after the words if no token.
	got, next, err = Tick("- [ ] a on:2026-10-22 b", 1, "a b", 'x', "2026-10-23")
	if err != nil || next != "" || got != "- [x] a on:2026-10-22 done:2026-10-23 b" {
		t.Errorf("Tick = %q, %q, %v", got, next, err)
	}
	got, _, err = Tick("- [ ] a b  \n- c", 1, "a b", 'x', "2026-10-23")
	if err != nil || got != "- [x] a b done:2026-10-23  \n- c" {
		t.Errorf("Tick = %q, %v", got, err)
	}

	// dropped: only the box changes, and no next line.
	got, next, err = Tick("- [ ] a on:2026-10-22 every:day\n", 1, "a", '-', "2026-10-23")
	if err != nil || next != "" || got != "- [-] a on:2026-10-22 every:day\n" {
		t.Errorf("dropped Tick = %q, %q, %v", got, next, err)
	}

	for _, c := range []struct {
		name, body string
		line       int
		text       string
		want       error
	}{
		{"text changed", "- [ ] a", 1, "b", ErrLineChanged},
		{"line missing", "- [ ] a", 2, "a", ErrLineChanged},
		{"not a plan line", "text", 1, "text", ErrLineChanged},
		{"done line", "- [x] a", 1, "a", ErrLineChanged},
		{"carried line", "- [>] a", 1, "a", ErrLineChanged},
		{"malformed", "- [ ] a on:x", 1, "a", ErrMalformed},
		{"empty text", "- [ ] on:2026-10-22", 1, "", ErrMalformed},
	} {
		if got, _, err := Tick(c.body, c.line, c.text, 'x', "2026-10-23"); !errors.Is(err, c.want) || got != "" {
			t.Errorf("%s: Tick = %q, %v, want %v", c.name, got, err, c.want)
		}
	}
	if _, _, err := Tick("- [ ] a on:9999-12-31 every:day", 1, "a", 'x', "9999-12-31"); err == nil {
		t.Error("a next date outside the range must refuse the tick")
	}
	if _, _, err := Tick("- [ ] a", 1, "a", 'q', "2026-10-23"); err == nil {
		t.Error("an unknown outcome must be refused")
	}
	if _, _, err := Tick("- [ ] a", 1, "a", 'x', "2026-02-30"); err == nil {
		t.Error("a bad done day must be refused")
	}
}

func TestCarry(t *testing.T) {
	body := "- [ ] a on:2026-10-22 due:2026-10-30 every:week from:done\n- [ ] b"
	got, cp, err := Carry(body, 1, "a", "2026-10-25")
	if err != nil || got != strings.Replace(body, "[ ] a", "[>] a", 1) {
		t.Fatalf("Carry = %q, %v", got, err)
	}
	if want := "- [ ] a on:2026-10-25 due:2026-10-30 every:week from:done"; cp != want {
		t.Errorf("copy = %q, want %q", cp, want)
	}
	_, cp, err = Carry("- [ ] b due:2026-10-30", 1, "b", "2026-10-25")
	if err != nil || cp != "- [ ] b on:2026-10-25 due:2026-10-30" {
		t.Errorf("on added: %q, %v", cp, err)
	}
	_, cp, err = Carry("- [ ] a on:2026-10-22", 1, "a", "")
	if err != nil || cp != "- [ ] a on:2026-10-22" {
		t.Errorf("on kept: %q, %v", cp, err)
	}
	if _, _, err := Carry("- [ ] a", 1, "x", ""); !errors.Is(err, ErrLineChanged) {
		t.Errorf("changed text: %v", err)
	}
	if _, _, err := Carry("- [ ] a every:week", 1, "a", ""); !errors.Is(err, ErrMalformed) {
		t.Errorf("malformed: %v", err)
	}
	if _, _, err := Carry("- [ ] a", 1, "a", "tomorrow"); err == nil {
		t.Error("a target that is not a day must be refused")
	}
}

func FuzzPlans(f *testing.F) {
	for _, s := range []string{
		"- [ ] a on:2026-10-22 every:week from:done",
		"> - [ ] q\n- [x] b done:2026-10-22\r\n- [-] c",
		"1. [ ] a\r- [ ] b due:2026-02-29",
		"- [ ] \tx on:2026-10-22 on:2026-10-23",
		"```\n- [ ] x\n```\n- [ ]",
	} {
		f.Add(s)
	}
	for _, c := range edgeCases {
		f.Add(c.body)
	}
	f.Fuzz(func(t *testing.T, body string) {
		for _, p := range Plans(body) {
			if p.Bad != "" || p.Text == "" {
				continue
			}
			line, err := Line(p.Text, p.On, p.Due, p.Every, p.FromDone)
			if err != nil {
				t.Fatalf("Line refused %+v: %v", p, err)
			}
			back := Plans(line)
			if len(back) != 1 {
				t.Fatalf("Line %q reads back as %d plan lines", line, len(back))
			}
			b := back[0]
			if b.Text != p.Text || b.On != p.On || b.Due != p.Due || b.Every != p.Every || b.FromDone != p.FromDone || b.Bad != "" {
				t.Fatalf("Line %q reads back as %+v, want the fields of %+v", line, b, p)
			}
		}
	})
}

func TestPlansOfALongBody(t *testing.T) {
	const n = 20000
	body := strings.Repeat("- [ ] a\n", n)
	if got := len(Plans(body)); got != n {
		t.Fatalf("got %d plans, want %d", got, n)
	}
}

// edgeCase is one body of the edges of the grammar. An empty tick, next or schedule is not checked.
type edgeCase struct {
	name     string
	body     string
	plans    int    // the number of plan lines
	text, on string // of the first plan line
	bad      string
	tick     string // the body after Tick of line 1 as done on 2026-03-15
	next     string // the line that the tick appends
	schedule string // the body after Schedule of line 1 to on:2026-05-05
	rangeErr bool   // Tick is refused by range and Schedule is accepted
}

const (
	edgeDone = "2026-03-15"
	edgeOn   = "2026-05-05"
)

var edgeCases = []edgeCase{
	{name: "ordered item", body: "1. [ ] ordered", plans: 1, text: "ordered",
		tick: "1. [x] ordered done:2026-03-15", schedule: "1. [ ] ordered on:2026-05-05"},
	{name: "tab after the bullet", body: "- [ ]\ttabbed", plans: 0},
	{name: "two blanks before the box", body: "-  [ ] two spaces", plans: 1, text: "two spaces",
		tick: "-  [x] two spaces done:2026-03-15"},
	{name: "two blanks after the box", body: "- [ ]  two spaces after", plans: 1, text: "two spaces after",
		tick: "- [x]  two spaces after done:2026-03-15", schedule: "- [ ] two spaces after on:2026-05-05"},
	{name: "no-break space joins its word", body: "- [ ] a\u00a0on:2026-01-01", plans: 1, text: "a\u00a0on:2026-01-01"},
	{name: "blank at the end", body: "- [ ] a on:2026-01-01 ", plans: 1, text: "a", on: "2026-01-01",
		tick: "- [x] a on:2026-01-01 done:2026-03-15 ", schedule: "- [ ] a on:2026-05-05"},
	{name: "lazy continuation", body: "- [ ] a\n  continuation on:2026-01-01", plans: 1, text: "a",
		tick: "- [x] a done:2026-03-15\n  continuation on:2026-01-01"},
	{name: "month end", body: "- [ ] a on:2026-01-31 every:month", plans: 1, text: "a", on: "2026-01-31",
		tick: "- [x] a on:2026-01-31 every:month done:2026-03-15", next: "- [ ] a on:2026-02-28 every:month-31"},
	{name: "distance of on from due", body: "- [ ] a due:2026-01-10 on:2026-01-12 every:week", plans: 1, text: "a",
		on: "2026-01-12", tick: "- [x] a due:2026-01-10 on:2026-01-12 every:week done:2026-03-15",
		next: "- [ ] a on:2026-01-19 due:2026-01-17 every:week"},
	{name: "count out of range", body: "- [ ] a on:2026-01-01 every:9999999999-days", plans: 1, text: "a",
		on: "2026-01-01", rangeErr: true},
	{name: "date out of range", body: "- [ ] a on:9999-12-31 every:day", plans: 1, text: "a",
		on: "9999-12-31", rangeErr: true},
	{name: "upper case key", body: "- [ ] a ON:2026-01-01", plans: 1, text: "a ON:2026-01-01"},
	{name: "on twice", body: "- [ ] a on:2026-01-01 on:2026-01-02", plans: 1, text: "a", bad: "on:2026-01-02"},
	{name: "from twice", body: "- [ ] a on:2026-01-01 every:day from:done from:done", plans: 1, text: "a", bad: "from:done"},
	{name: "every without a day", body: "- [ ] a every:day", plans: 1, text: "a", bad: "every:day"},
}

func TestPlansAtTheEdgesOfTheGrammar(t *testing.T) {
	for _, c := range edgeCases {
		t.Run(c.name, func(t *testing.T) {
			ps := Plans(c.body)
			if len(ps) != c.plans {
				t.Fatalf("%d plan lines, want %d", len(ps), c.plans)
			}
			if c.plans == 0 {
				return
			}
			p := ps[0]
			if p.Box != ' ' || p.Line != 1 || p.Text != c.text || p.On != c.on || p.Bad != c.bad {
				t.Fatalf("got box %q line %d text %q on %q bad %q, want text %q on %q bad %q",
					p.Box, p.Line, p.Text, p.On, p.Bad, c.text, c.on, c.bad)
			}
			if c.bad != "" {
				return
			}
			ticked, next, err := Tick(c.body, 1, p.Text, 'x', edgeDone)
			if c.rangeErr {
				if err == nil || !strings.Contains(err.Error(), "the next date is outside 0000-01-01 to 9999-12-31") {
					t.Errorf("Tick error = %v, want the range error", err)
				}
				if _, err := Schedule(c.body, 1, p.Text, edgeOn, ""); err != nil {
					t.Errorf("Schedule refused: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Tick: %v", err)
			}
			if c.tick != "" && ticked != c.tick {
				t.Errorf("Tick body = %q, want %q", ticked, c.tick)
			}
			if next != c.next {
				t.Errorf("Tick appended = %q, want %q", next, c.next)
			}
			if c.schedule != "" {
				got, err := Schedule(c.body, 1, p.Text, edgeOn, "")
				if err != nil || got != c.schedule {
					t.Errorf("Schedule = %q, %v, want %q", got, err, c.schedule)
				}
			}
			if c.tick == "" {
				return
			}
			if next != "" {
				ticked += "\n\n" + next
			}
			back, err := Reopen(ticked, 1, p.Text)
			if err != nil || back.Body != c.body || back.Removed != next {
				t.Errorf("Reopen = %q removed %q, %v, want %q removed %q", back.Body, back.Removed, err, c.body, next)
			}
		})
	}
}

func TestAnUpperCaseKeyIsAWordForLine(t *testing.T) {
	line, err := Line("a ON:2026-01-01", "", "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	ps := Plans(line)
	if len(ps) != 1 || ps[0].Text != "a ON:2026-01-01" || ps[0].Bad != "" {
		t.Fatalf("Line %q reads back as %+v", line, ps)
	}
}

// bodyLines are the lines of the generator. A multi-line entry is one unit with its own breaks.
var bodyLines = []string{
	"- [ ] a", "- [ ] b on:2026-03-01", "- [ ] c due:2026-03-10", "- [ ] d on:2026-03-01 due:2026-03-04",
	"- [ ] e on:2026-03-01 every:day", "- [ ] f on:2026-01-31 every:month", "- [ ] g on:2026-03-02 every:week from:done",
	"- [ ] h due:2026-03-10 on:2026-03-12 every:week", "  - [ ] nested on:2026-03-05", "1. [ ] ordered",
	"- [x] done a done:2026-02-01", "- [-] dropped", "- [>] carried", "plain text", "> - [ ] quoted",
	"```\n- [ ] fenced\n```", "", "", "- [ ] bad on:2026-03-01 on:2026-03-02", "- [ ]",
}

var lineEnds = []string{"\n", "\r\n", "\r"}

func randomBody(rng *rand.Rand) string {
	var b strings.Builder
	for n := 1 + rng.Intn(6); n > 0; n-- {
		b.WriteString(bodyLines[rng.Intn(len(bodyLines))])
		if n > 1 || rng.Intn(2) == 0 {
			b.WriteString(lineEnds[rng.Intn(len(lineEnds))])
		}
	}
	return b.String()
}

// splitLines cuts a body at lineStarts, so that LF, CRLF and CR each end a line once.
func splitLines(body string) []string {
	starts := lineStarts([]byte(body))
	out := make([]string, len(starts))
	for i, s := range starts {
		end := len(body)
		if i+1 < len(starts) {
			end = starts[i+1]
		}
		out[i] = body[s:end]
	}
	return out
}

// checkEveryWrite checks Tick with Reopen, Schedule and Carry on one open line. It returns the problem, or "".
func checkEveryWrite(body string, p Plan) string {
	ticked, next, err := Tick(body, p.Line, p.Text, 'x', "2026-03-15")
	if err != nil {
		return fmt.Sprintf("Tick: %v", err)
	}
	if next != "" {
		ticked += "\n\n" + next
	}
	back, err := Reopen(ticked, p.Line, p.Text)
	if err != nil || back.Body != body {
		return fmt.Sprintf("Reopen of %q gave %q, %v", ticked, back.Body, err)
	}

	scheduled, err := Schedule(body, p.Line, p.Text, "2026-05-05", "")
	if err != nil {
		return fmt.Sprintf("Schedule: %v", err)
	}
	before, after := splitLines(body), splitLines(scheduled)
	if len(before) != len(after) {
		return fmt.Sprintf("Schedule gave %q: %d lines, want %d", scheduled, len(after), len(before))
	}
	for i := range before {
		if i != p.Line-1 && before[i] != after[i] {
			return fmt.Sprintf("Schedule changed line %d: %q to %q", i+1, before[i], after[i])
		}
	}

	carried, _, err := Carry(body, p.Line, p.Text, "")
	if err != nil {
		return fmt.Sprintf("Carry: %v", err)
	}
	changed := 0
	if len(carried) != len(body) {
		return fmt.Sprintf("Carry gave %q: another length", carried)
	}
	for i := range body {
		if body[i] != carried[i] {
			changed++
		}
	}
	if changed != 1 {
		return fmt.Sprintf("Carry changed %d bytes: %q", changed, carried)
	}
	return ""
}

func TestEveryWriteOnARandomBodyChangesItsLineOnly(t *testing.T) {
	const seed = 142
	rng := rand.New(rand.NewSource(seed))
	checked := 0
	for i := 0; i < 400; i++ {
		body := randomBody(rng)
		for _, p := range Plans(body) {
			if p.Box != ' ' || p.Bad != "" || p.Text == "" {
				continue
			}
			if problem := checkEveryWrite(body, p); problem != "" {
				t.Errorf("seed %d, body %q, line %d: %s", seed, body, p.Line, problem)
			}
			checked++
		}
	}
	if checked < 200 {
		t.Fatalf("only %d open lines checked", checked)
	}
}
