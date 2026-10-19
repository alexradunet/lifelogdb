package text

import (
	"encoding/json"
	"errors"
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
