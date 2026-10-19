package tests

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"lifelog/internal/text"
)

// planLines executes the vectors of contract/plan-lines through the writer's own extraction (text.Plans) and its
// next-line rule (text.Next). The page is read through the kit, so a mutant can break it.
func planLines(s *S) {
	page := strings.ReplaceAll(s.d.Page("contract/plan-lines.md"), "\r\n", "\n")
	block := func(marker string, into any) {
		_, rest, ok := strings.Cut(page, marker)
		if !ok {
			stop("missing %s", marker)
		}
		_, rest, ok = strings.Cut(rest, "```json\n")
		if !ok {
			stop("missing JSON after %s", marker)
		}
		encoded, _, ok := strings.Cut(rest, "```")
		if !ok {
			stop("unterminated JSON after %s", marker)
		}
		if err := json.Unmarshal([]byte(encoded), into); err != nil {
			stop("parse %s: %v", marker, err)
		}
	}
	var lines []struct {
		Body  string
		Plans []struct {
			Line                      int
			Box, Text                 string
			On, Due, Done, Every, Bad string
			FromDone                  bool `json:"from_done"`
		}
	}
	block("<!-- plan-line-vectors -->", &lines)
	var days []struct {
		On        string `json:"on"`
		Due       string `json:"due"`
		Every     string `json:"every"`
		FromDone  bool   `json:"from_done"`
		Done      string `json:"done"`
		NextOn    string `json:"next_on"`
		NextDue   string `json:"next_due"`
		NextEvery string `json:"next_every"`
		Error     bool   `json:"error"`
	}
	block("<!-- next-day-vectors -->", &days)
	var reopens []struct {
		Body, Text, After, Removed, Error string
		Line                              int
	}
	block("<!-- reopen-vectors -->", &reopens)
	s.K("the reopen vectors are complete", len(reopens) >= 14, len(reopens))
	s.K("the plan-line vectors are complete", len(lines) >= 40, len(lines))
	s.K("the next-day vectors are complete", len(days) >= 25, len(days))

	for _, v := range lines {
		got := text.Plans(v.Body)
		ok := len(got) == len(v.Plans)
		for i := 0; ok && i < len(got); i++ {
			g, w := got[i], v.Plans[i]
			ok = g.Line == w.Line && string(g.Box) == w.Box && g.Text == w.Text && g.On == w.On && g.Due == w.Due &&
				g.Done == w.Done && g.Every == w.Every && g.FromDone == w.FromDone && g.Bad == w.Bad
		}
		s.K("every printed plan-line vector reproduces with the writer's extraction", ok, fmt.Sprintf("body %q: got %+v", v.Body, got))
	}
	for _, v := range days {
		on, due, every, err := text.Next(text.Plan{On: v.On, Due: v.Due, Every: v.Every, FromDone: v.FromDone}, v.Done)
		ok := err == nil && on == v.NextOn && due == v.NextDue && every == v.NextEvery
		if v.Error {
			ok = err != nil
		}
		s.K("every printed next-day vector reproduces", ok, fmt.Sprintf("%+v: got %q %q %q, %v", v, on, due, every, err))
	}
	errs := map[string]error{"changed": text.ErrLineChanged, "carried": text.ErrCarried, "malformed": text.ErrMalformed}
	for _, v := range reopens {
		got, err := text.Reopen(v.Body, v.Line, v.Text)
		ok := err == nil && got.Body == v.After && got.Removed == v.Removed
		if v.Error != "" {
			ok = errors.Is(err, errs[v.Error])
		}
		s.K("every printed reopen vector reproduces", ok, fmt.Sprintf("%+v: got %+v, %v", v, got, err))
	}
}

// planLinesIndex probes the derived index of contract/plan-lines: the generated column has_box and the partial index
// entities_plans. The index is a superset filter that follows every body write.
func planLinesIndex(s *S) {
	c := s.fresh()
	const q = "SELECT e.id FROM entities e WHERE e.has_box AND e.deleted_at IS NULL"
	plan := c.plan(q)
	s.K("the plans query reads the pages through entities_plans", strings.Contains(plan, "entities_plans"), plan)
	count := func() int64 { return c.n("SELECT count(*) FROM entities WHERE has_box AND deleted_at IS NULL") }
	id := c.pageW("Errands", nil, "no box here, only [a link] and []")
	s.K("a page without a box is not in the index", count() == 0)
	for _, box := range []string{"[ ]", "[x]", "[X]", "[-]", "[>]"} {
		c.must("UPDATE entities SET body = ? WHERE id = ?", "- "+box+" Call the bank", id)
		s.K("a body with the box "+box+" enters the index", count() == 1, box)
		c.must("UPDATE entities SET body = 'plain' WHERE id = ?", id)
		s.K("a body edited to plain text leaves the index after "+box, count() == 0, box)
	}
	c.must("UPDATE entities SET body = 'a [ ] in prose' WHERE id = ?", id)
	s.K("a box in prose counts: the index is a superset", count() == 1)
	c.must("UPDATE entities SET deleted_at = "+NOW+" WHERE id = ?", id)
	s.K("a tombstoned page leaves the index", count() == 0)
}
