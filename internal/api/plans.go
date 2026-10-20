package api

// The plan lines (docs/contract/plan-lines.md, D29): the four actions that write one, and the lines that a day, a
// period and a period page show. The file does not know a plan; core reads it from the bodies.

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"lifelog/internal/api/siren"
	"lifelog/internal/core"
)

// plannedLimit is how many open lines a day shows; planned_more counts the rest.
const plannedLimit = 200

// planLine is a plan line and its page, as the API shows it. Box is the box of the line: " " open, "x" done, "-"
// dropped, ">" carried.
type planLine struct {
	PageID    int64  `json:"page_id"`
	Title     string `json:"title"`
	IsDayPage bool   `json:"is_day_page"`
	Period    string `json:"period,omitempty"`
	Line      int    `json:"line"`
	Box       string `json:"box"`
	Text      string `json:"text"`
	On        string `json:"on,omitempty"`
	Due       string `json:"due,omitempty"`
	Done      string `json:"done,omitempty"`
	Every     string `json:"every,omitempty"`
	FromDone  bool   `json:"from_done,omitempty"`
	Bad       string `json:"bad,omitempty"`
}

// planLinesOf is the lines as the API shows them. The list is never nil, so JSON says [] and not null.
func planLinesOf(items []core.PlanItem) []planLine {
	out := make([]planLine, len(items))
	for i, it := range items {
		out[i] = planLine{PageID: it.PageID, Title: it.Title, IsDayPage: it.IsDayPage, Period: it.Period, Line: it.Line,
			Box: string(it.Box), Text: it.Text, On: it.On, Due: it.Due, Done: it.Done, Every: it.Every, FromDone: it.FromDone, Bad: it.Bad}
	}
	return out
}

// linesIn are the lines planned from from through through, and the lines done in that window.
func (h *server) linesIn(ctx context.Context, from, through string) ([]planLine, error) {
	items, err := h.s.PlanLines(ctx, from, through)
	return planLinesOf(items), err
}

// plannedFor are the lines a day shows: from today on, the open lines planned on or before the day, at most
// plannedLimit and the count left out; before today, the lines of that day alone.
func (h *server) plannedFor(ctx context.Context, day string) (lines []planLine, more int, err error) {
	if day < h.s.Today() {
		lines, err = h.linesIn(ctx, day, day)
		return lines, 0, err
	}
	items, more, err := h.s.Planned(ctx, day, plannedLimit)
	return planLinesOf(items), more, err
}

// periodWindow is the first and the last day of a period page, by its period (entities.period) and its title, which is
// its key: 2026, 2026-10, 2026-Q4 or 2026-W43. A week is an ISO week. ok is false for a key that is not one of these.
func periodWindow(period, key string) (from, through string, ok bool) {
	var first, last time.Time
	var year, n int
	switch period {
	case "year":
		if _, err := fmt.Sscanf(key, "%d", &year); err != nil {
			return "", "", false
		}
		first, last = time.Date(year, time.January, 1, 0, 0, 0, 0, time.UTC), time.Date(year, time.December, 31, 0, 0, 0, 0, time.UTC)
	case "month":
		if _, err := fmt.Sscanf(key, "%d-%d", &year, &n); err != nil {
			return "", "", false
		}
		first = time.Date(year, time.Month(n), 1, 0, 0, 0, 0, time.UTC)
		last = first.AddDate(0, 1, -1)
	case "quarter":
		if _, err := fmt.Sscanf(key, "%d-Q%d", &year, &n); err != nil {
			return "", "", false
		}
		first = time.Date(year, time.Month(3*n-2), 1, 0, 0, 0, 0, time.UTC)
		last = first.AddDate(0, 3, -1)
	case "week":
		if _, err := fmt.Sscanf(key, "%d-W%d", &year, &n); err != nil {
			return "", "", false
		}
		// Week 1 is the week of January 4 (ISO 8601); a week starts on a Monday.
		jan4 := time.Date(year, time.January, 4, 0, 0, 0, 0, time.UTC)
		first = jan4.AddDate(0, 0, -((int(jan4.Weekday())+6)%7)+(n-1)*7)
		last = first.AddDate(0, 0, 6)
	default:
		return "", "", false
	}
	return first.Format(time.DateOnly), last.Format(time.DateOnly), true
}

// pageProperties are the properties of a page: the page, and for a period page the lines planned in its period.
type pageProperties struct {
	core.Page
	Lines []planLine `json:"lines,omitempty"`
}

// planAction is the plan action, with page filled in: the title of the page the line goes to.
func planAction(page string) siren.Action {
	return action("plan", nil, map[string]any{"page": page})
}

// lineActions are the tick, the carry and the schedule of a view that lists lines of any page (the day, a week, a month, a year). They
// stay templated: the line names its page, and the browser fills the address (view.LineAction).
func lineActions() []siren.Action {
	return []siren.Action{templated(specOf("tick")), templated(specOf("carry")), templated(specOf("schedule"))}
}

// lineNumber is the field line: a whole number of 1 or more.
func lineNumber(v url.Values) (int, error) {
	f, err := strconv.ParseFloat(v.Get("line"), 64)
	if err != nil || f != math.Trunc(f) || f < 1 || f > math.MaxInt32 {
		return 0, core.Invalid("line %q is not a line number: 1 or more", v.Get("line"))
	}
	return int(f), nil
}

func (h *server) plan(r *http.Request, act spec, src string) (*siren.Entity, error) {
	v, err := fieldsOf(r, act)
	if err != nil {
		return nil, err
	}
	fromDone, err := flag(v, "from_done")
	if err != nil {
		return nil, err
	}
	res, err := h.s.Plan(r.Context(), src, h.dayNamed(v.Get("page")), v.Get("text"), h.dayNamed(v.Get("on")), h.dayNamed(v.Get("due")), v.Get("every"), fromDone)
	if err != nil {
		return nil, err
	}
	return withResult(h.pageEntity(r.Context(), res.PageID))(map[string]any{"page_id": res.PageID, "line": res.Line, "written": res.Written})
}

func (h *server) tick(r *http.Request, act spec, src string) (*siren.Entity, error) {
	id, v, err := idForm(r, act)
	if err != nil {
		return nil, err
	}
	line, err := lineNumber(v)
	if err != nil {
		return nil, err
	}
	res, err := h.s.Tick(r.Context(), src, id, line, v.Get("text"), v.Get("outcome"), h.dayNamed(v.Get("done")))
	if err != nil {
		return nil, err
	}
	// text and page_id let a client undo the tick with a reopen.
	out := map[string]any{"line": res.Line, "outcome": v.Get("outcome"), "text": v.Get("text"), "page_id": id}
	if res.Appended != "" {
		out["appended"], out["appended_line"] = res.Appended, res.AppendedLine
	}
	if res.Removed != "" {
		out["removed"], out["removed_line"] = res.Removed, res.RemovedLine
	}
	if res.NextKept {
		out["next_kept"] = true
	}
	return withResult(h.pageEntity(r.Context(), id))(out)
}

func (h *server) carry(r *http.Request, act spec, src string) (*siren.Entity, error) {
	id, v, err := idForm(r, act)
	if err != nil {
		return nil, err
	}
	line, err := lineNumber(v)
	if err != nil {
		return nil, err
	}
	res, err := h.s.Carry(r.Context(), src, id, line, v.Get("text"), h.dayNamed(v.Get("to")))
	if err != nil {
		return nil, err
	}
	return withResult(h.pageEntity(r.Context(), id))(map[string]any{"line": res.Line, "to_id": res.ToID, "to_line": res.ToLine})
}

func (h *server) schedule(r *http.Request, act spec, src string) (*siren.Entity, error) {
	id, v, err := idForm(r, act)
	if err != nil {
		return nil, err
	}
	line, err := lineNumber(v)
	if err != nil {
		return nil, err
	}
	// A date the client leaves out stays; an empty one is removed (core.Store.Schedule).
	day := func(name string) *string {
		if !v.Has(name) {
			return nil
		}
		d := h.dayNamed(v.Get(name))
		return &d
	}
	res, err := h.s.Schedule(r.Context(), src, id, line, v.Get("text"), day("on"), day("due"))
	if err != nil {
		return nil, err
	}
	return withResult(h.pageEntity(r.Context(), id))(map[string]any{"line": res.Line, "page_id": id, "written": res.Written, "on": res.On, "due": res.Due})
}

// planOrders are the values of the field order of plans: by day, or by page.
var planOrders = []string{"day", "page"}

// plansProps are the properties of the plans answer. Total counts every open line, whatever the page of rows.
type plansProps struct {
	pageProps
	Today string     `json:"today"`
	Order string     `json:"order"`
	Total int        `json:"total"`
	Lines []planLine `json:"lines"`
}

// plans is GET /plans and the action plans: the open lines of every live page, by day or by page.
func (h *server) plans(r *http.Request, act spec, _ string) (*siren.Entity, error) {
	v, err := fieldsOf(r, act)
	if err != nil {
		return nil, err
	}
	order := v.Get("order")
	if order == "" {
		order = planOrders[0]
	}
	if !slices.Contains(planOrders, order) {
		return nil, core.Invalid("order must be one of %s", strings.Join(planOrders, ", "))
	}
	limit, offset, err := pageOf(r, plannedLimit)
	if err != nil {
		return nil, err
	}
	items, total, err := h.s.OpenLines(r.Context(), order == "page", limit, offset)
	if err != nil {
		return nil, err
	}
	q := url.Values{}
	self := "/plans"
	if order != planOrders[0] {
		q.Set("order", order)
		self += "?" + q.Encode()
	}
	e := &siren.Entity{Class: []string{"plans"}, Title: "Plans",
		Links:   []siren.Link{link("self", self, "Plans"), link("index", "/", "Home")},
		Actions: append([]siren.Action{planAction(h.s.Today())}, lineActions()...)}
	props := pageLinks(e, offset+len(items) < total, "/plans", q, limit, offset)
	e.Properties = plansProps{pageProps: props, Today: h.s.Today(), Order: order, Total: total, Lines: planLinesOf(items)}
	return e, nil
}
