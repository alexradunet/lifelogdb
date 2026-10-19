package core

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"lifelog/internal/text"
)

// Plan lines (docs/contract/plan-lines.md, D29): a plan is a line of a page body, and the file does not know it. The
// reads below list the lines of the live pages, and the writes edit the body as text. A plan write has no version
// token and appends or edits one line, as a capture does (D12). A stale line is the 409 of a stale save.

// PlanItem is one plan line and the page it is in.
type PlanItem struct {
	PageID    int64
	Title     string
	IsDayPage bool
	Period    string // year, month, quarter or week for a period page, else ""
	text.Plan
}

// maxWindowDays is the longest window of PlanLines.
const maxWindowDays = 366

// pagesWithBoxes reads every live page whose body can hold a plan line. has_box is the one home of that test, kept by
// SQLite and read through the index entities_plans: a filter on body itself reads every body (380 ms for the five
// boxes on the lifetime fixture, 1,600 ms). The filter is a superset, so the parser still reads the bodies. The title
// is the preferred name.
const pagesWithBoxes = `
	SELECT e.id, n.title, e.is_journal, coalesce(e.period, ''), e.body
	  FROM entities e JOIN entity_names n ON n.entity_id = e.id AND n.name_key = e.preferred_name_key
	 WHERE e.has_box AND e.deleted_at IS NULL`

// planItems lists the lines that keep accepts, with the day to sort them by, in the order of that day, title and line.
// A malformed line has no day, so keep sees it with Bad set and refuses it.
func (s *Store) planItems(ctx context.Context, keep func(text.Plan) (day string, ok bool)) ([]PlanItem, error) {
	type found struct {
		PlanItem
		day string
	}
	var all []found
	var it PlanItem
	var body string
	rows, err := s.DB.R.QueryContext(ctx, pagesWithBoxes)
	if err := each(rows, err, []any{&it.PageID, &it.Title, &it.IsDayPage, &it.Period, &body}, func() {
		for _, p := range text.Plans(body) {
			if day, ok := keep(p); ok {
				item := it
				item.Plan = p
				all = append(all, found{item, day})
			}
		}
	}); err != nil {
		return nil, err
	}
	slices.SortFunc(all, func(a, b found) int {
		if c := strings.Compare(a.day, b.day); c != 0 {
			return c
		}
		if c := strings.Compare(a.Title, b.Title); c != 0 {
			return c
		}
		return a.Line - b.Line
	})
	out := make([]PlanItem, len(all))
	for i, f := range all {
		out[i] = f.PlanItem
	}
	return out, nil
}

// plannedDay is the day a line is planned on: its on: day, else its due: day, else "".
func plannedDay(p text.Plan) string {
	if p.On != "" {
		return p.On
	}
	return p.Due
}

// Planned lists the open lines planned on or before day, for Home and for a day at or after today. It lists no
// malformed line and no line without a day. At most limit items come back (limit is 1 or more); more says how many
// were left out.
func (s *Store) Planned(ctx context.Context, day string, limit int) (items []PlanItem, more int, err error) {
	if err := checkDay("day", day); err != nil {
		return nil, 0, err
	}
	if limit < 1 {
		return nil, 0, Invalid("planned lines need a limit of 1 or more")
	}
	items, err = s.planItems(ctx, func(p text.Plan) (string, bool) {
		d := plannedDay(p)
		return d, p.Box == ' ' && p.Bad == "" && d != "" && d <= day
	})
	if err != nil {
		return nil, 0, err
	}
	if len(items) > limit {
		more = len(items) - limit
		items = items[:limit]
	}
	return items, more, nil
}

// PlanLines lists the lines planned from from through through (inclusive, at most 366 days), in any box, and the
// done lines whose done: day is in the window. A line sorts by its planned day, or by its done day if it has no
// planned day. It lists no malformed line.
func (s *Store) PlanLines(ctx context.Context, from, through string) ([]PlanItem, error) {
	if err := checkWindow(from, through); err != nil {
		return nil, err
	}
	first, _ := time.Parse(time.DateOnly, from)
	last, _ := time.Parse(time.DateOnly, through)
	if last.Sub(first) >= maxWindowDays*24*time.Hour {
		return nil, Invalid("a window of plan lines is at most %d days", maxWindowDays)
	}
	in := func(d string) bool { return d != "" && d >= from && d <= through }
	return s.planItems(ctx, func(p text.Plan) (string, bool) {
		if p.Bad != "" {
			return "", false
		}
		if d := plannedDay(p); in(d) || (p.Box == 'x' && in(p.Done)) {
			if d == "" {
				d = p.Done
			}
			return d, true
		}
		return "", false
	})
}

// planError turns an error of the plan parser into the answer of a surface: a line that changed since the client
// read it is a stale version (the recovery is the same: read the page again), and the rest is a refusal of the input.
func planError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, text.ErrLineChanged):
		return conflictCode(CodeStaleVersion, "the line changed, or it is not an open plan line: read the page again")
	default:
		return Invalid("%s", err)
	}
}

// bodyOf is the body of the page id.
func (t *Tx) bodyOf(id int64) (string, error) {
	var body string
	err := t.tx.QueryRowContext(t.ctx, `SELECT body FROM entities WHERE id = ?`, id).Scan(&body)
	return body, err
}

// appendLine adds a plan line to the end of a body as its own paragraph. at is the number of the new line. It is read
// back from the new body, and a line that the parser does not read as a new plan line (an open fence or a quote
// swallows it) is refused.
func appendLine(body, line string) (newBody string, at int, err error) {
	newBody = body
	if body != "" {
		newBody += "\n\n"
	}
	newBody += line
	plans := text.Plans(newBody)
	if n := len(plans); n == 0 || plans[n-1].Raw != line || n != len(text.Plans(body))+1 {
		return "", 0, Invalid("the page cannot take the line: its end is inside a code block or a quote")
	}
	return newBody, plans[len(plans)-1].Line, nil
}

// appendPlan adds the line to the end of the page id, which must exist, and gives its line number.
func (t *Tx) appendPlan(id int64, line string) (at int, err error) {
	body, err := t.bodyOf(id)
	if err != nil {
		return 0, err
	}
	if body, at, err = appendLine(body, line); err != nil {
		return 0, err
	}
	_, err = t.SetBody(id, body)
	return at, err
}

// planTarget is the id of the page titled title for a plan line: found, revived or made on the first write. A day
// title makes a day page, and a period title its canonical period page, whose existence the file decides.
func (t *Tx) planTarget(title string) (id int64, err error) {
	if err := checkTitle("title", title); err != nil {
		return 0, err
	}
	key := text.TitleKey(title)
	id, _, _, err = t.ensurePage(title, key, dayOfTitle(key))
	return id, err
}

// PlanResult is the line that Plan wrote: the page, the number of the line in its body, and its text.
type PlanResult struct {
	PageID  int64
	Line    int
	Written string
}

// Plan appends a plan line to the page titled title: a plain page, a day (YYYY-MM-DD) or a period key, made on the
// first write.
func (s *Store) Plan(ctx context.Context, source, title, words, on, due, every string, fromDone bool) (PlanResult, error) {
	return alone(s, ctx, source, func(t *Tx) (PlanResult, error) {
		line, err := text.Line(words, on, due, every, fromDone)
		if err != nil {
			return PlanResult{}, planError(err)
		}
		id, err := t.planTarget(title)
		if err != nil {
			return PlanResult{}, err
		}
		at, err := t.appendPlan(id, line)
		return PlanResult{id, at, line}, err
	})
}

// TickResult is what Tick did: the page and the line it closed, and the next line of a repeat (empty, with line 0,
// when there is none).
type TickResult struct {
	PageID       int64
	Line         int
	Appended     string
	AppendedLine int
}

// outcomes are the outcomes of a tick and the box each one writes.
var outcomes = map[string]byte{"done": 'x', "dropped": '-'}

// Tick closes the open line at line of the page id when its words match text. The outcome is "done" or "dropped".
// For done, done is the day it was done ("" means today). A done line with every: adds the next line in the same
// write.
func (s *Store) Tick(ctx context.Context, source string, id int64, line int, words, outcome, done string) (TickResult, error) {
	return alone(s, ctx, source, func(t *Tx) (TickResult, error) {
		box, ok := outcomes[outcome]
		if !ok {
			return TickResult{}, Invalid("outcome %q is not done or dropped", outcome)
		}
		if words == "" {
			return TickResult{}, Invalid("the text of the line is empty")
		}
		if done == "" {
			done = t.today()
		}
		if err := checkDay("done", done); err != nil {
			return TickResult{}, err
		}
		if _, err := t.livePage(id); err != nil {
			return TickResult{}, err
		}
		body, err := t.bodyOf(id)
		if err != nil {
			return TickResult{}, err
		}
		body, next, err := text.Tick(body, line, words, box, done)
		if err != nil {
			return TickResult{}, planError(err)
		}
		r := TickResult{PageID: id, Line: line, Appended: next}
		if next != "" {
			if body, r.AppendedLine, err = appendLine(body, next); err != nil {
				return TickResult{}, err
			}
		}
		_, err = t.SetBody(id, body)
		return r, err
	})
}

// CarryResult is what Carry did: the source page and line, and the target page and the line of the copy.
type CarryResult struct {
	PageID int64
	Line   int
	ToID   int64
	ToLine int
}

// Carry marks the open line at line of the page id carried, and appends its copy to the page titled to, made on the
// first write. If to is a day, the copy is planned on that day (on:). The two writes are one transaction.
func (s *Store) Carry(ctx context.Context, source string, id int64, line int, words, to string) (CarryResult, error) {
	return alone(s, ctx, source, func(t *Tx) (CarryResult, error) {
		if words == "" {
			return CarryResult{}, Invalid("the text of the line is empty")
		}
		if err := checkTitle("to", to); err != nil {
			return CarryResult{}, err
		}
		if _, err := t.livePage(id); err != nil {
			return CarryResult{}, err
		}
		body, err := t.bodyOf(id)
		if err != nil {
			return CarryResult{}, err
		}
		key := text.TitleKey(to)
		on := ""
		if text.IsDay(key) {
			on = key
		}
		body, copied, err := text.Carry(body, line, words, on)
		if err != nil {
			return CarryResult{}, planError(err)
		}
		toID, err := t.planTarget(to)
		if err != nil {
			return CarryResult{}, err
		}
		if toID == id {
			return CarryResult{}, Invalid("a line is carried to another page, not to page %d itself", id)
		}
		if _, err := t.SetBody(id, body); err != nil {
			return CarryResult{}, err
		}
		toLine, err := t.appendPlan(toID, copied)
		return CarryResult{id, line, toID, toLine}, err
	})
}
