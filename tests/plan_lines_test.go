package tests

import (
	"encoding/json"
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
}
