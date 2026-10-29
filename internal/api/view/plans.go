package view

// Plan lines in the browser (docs/contract/plan-lines.md, docs/plans/136-plan-lines-and-period-pages.md): the lines a
// day, a week, a month, a year or a period page lists, read from the entity's properties. The view only groups and
// draws them. The API reads the lines, and the actions write them.

import (
	"fmt"
	"html/template"
	"lifelog/siren"
	"net/url"
	"slices"
	"strconv"
	"strings"
)

// boxHTML is the box of a line that cannot tick, as a glyph. The words are for a reader who does not see it.
func boxHTML(box any) template.HTML {
	b, _ := box.(string)
	if b == "" {
		b = " "
	}
	g, ok := boxGlyph[b[0]]
	if !ok {
		return ""
	}
	return template.HTML(`<span class="box ` + g[0] + `" role="img" aria-label="` + g[0] + `">` + g[1] + `</span>`)
}

// lineHref is the address of an action on the page that holds a line: the line names its page, not the view.
func lineHref(name string, pageID any) string {
	n := Num(pageID)
	if n == "" || n == "0" {
		return ""
	}
	return "/pages/" + url.PathEscape(n) + "/" + name
}

// TickHref is the address of the tick of the page with that id, "" for no page or for a view that does not offer
// tick. It makes the boxes of a body tick.
func (v *view) TickHref(pageID any) string {
	if v.Action("tick") == nil {
		return ""
	}
	return lineHref("tick", pageID)
}

// LineAction is the entity's action of that name for one line: it posts to the line's page, and its line and text
// fields hold the line's. Pairs of name and value after the line fill other fields (carry's to). Nil when the view
// does not offer the action.
func (v *view) LineAction(name string, line any, values ...string) *shownAction {
	a := v.Action(name)
	m, _ := line.(map[string]any)
	if a == nil || m == nil {
		return nil
	}
	c := *a
	c.Href = lineHref(name, m["page_id"])
	c.Fields = nil
	for _, f := range a.Fields {
		if f.In == "path" {
			continue
		}
		switch f.Name {
		case "line", "text":
			f.Value = m[f.Name]
		}
		for i := 0; i+1 < len(values); i += 2 {
			if values[i] == f.Name {
				f.Value = values[i+1]
			}
		}
		c.Fields = append(c.Fields, f)
	}
	return &c
}

// plannedDay is the day a line is planned on: its on: day, else its due: day.
func plannedDay(l map[string]any) string {
	on, _ := l["on"].(string)
	if on != "" {
		return on
	}
	due, _ := l["due"].(string)
	return due
}

// lineDay is the day a line sits under in a list by day: a done line under the day it was done, else the day it is
// planned for.
func lineDay(l map[string]any) string {
	if l["box"] == "x" {
		if done, _ := l["done"].(string); done != "" {
			return done
		}
	}
	return plannedDay(l)
}

// plannedGroup is lines under a heading ("" for none).
type plannedGroup struct {
	Title string
	Lines []any
}

// PlannedGroups are the lines of a day's Planned: on today or a later day the lines planned for the day itself, then
// the earlier ones; on a past day the lines of that day alone, under no heading.
func (v *view) PlannedGroups() []plannedGroup {
	props, _ := v.Props.(map[string]any)
	lines, _ := props["planned"].([]any)
	today, _ := props["today"].(string)
	if len(lines) == 0 {
		return nil
	}
	if v.Title < today {
		return []plannedGroup{{Lines: lines}}
	}
	now := plannedGroup{Title: "This day"}
	if v.Title == today {
		now.Title = "Today"
	}
	earlier := plannedGroup{Title: "Earlier"}
	for _, l := range lines {
		m, _ := l.(map[string]any)
		if plannedDay(m) == v.Title {
			now.Lines = append(now.Lines, l)
		} else {
			earlier.Lines = append(earlier.Lines, l)
		}
	}
	var out []plannedGroup
	for _, g := range []plannedGroup{now, earlier} {
		if len(g.Lines) > 0 {
			out = append(out, g)
		}
	}
	return out
}

// LinesOn are the lines of the entity's lines list that sit under a day (lineDay), in their order.
func (v *view) LinesOn(day string) []any {
	props, _ := v.Props.(map[string]any)
	lines, _ := props["lines"].([]any)
	return slices.DeleteFunc(slices.Clone(lines), func(l any) bool {
		m, _ := l.(map[string]any)
		return lineDay(m) != day
	})
}

// periodWords is the words for the kind of a period page, "week page"; "" for a page that is not one.
func periodWords(period any) string {
	p, _ := period.(string)
	if p == "" {
		return ""
	}
	return strings.ToLower(p) + " page"
}

// PeriodViews are the links to the views of a period page: a week, a month or a year has one, and a quarter links its
// three months. Nil for a page that is not a period page.
func (v *view) PeriodViews() []siren.Link {
	props, _ := v.Props.(map[string]any)
	period, _ := props["period"].(string)
	switch period {
	case "week":
		return []siren.Link{{Href: "/weeks/" + v.Title, Title: "The week view"}}
	case "month":
		return []siren.Link{{Href: "/months/" + v.Title, Title: "The month view"}}
	case "year":
		return []siren.Link{{Href: "/years/" + v.Title, Title: "The year view"}}
	case "quarter":
		year, q, ok := strings.Cut(v.Title, "-Q")
		n, err := strconv.Atoi(q)
		if !ok || err != nil || n < 1 || n > 4 {
			return nil
		}
		var out []siren.Link
		for m := 3*n - 2; m <= 3*n; m++ {
			month := fmt.Sprintf("%s-%02d", year, m)
			out = append(out, siren.Link{Href: "/months/" + month, Title: monthName(month)})
		}
		return out
	}
	return nil
}

// plansGroup is the rows of the Plans page under one head. In a list by day the head is Title; in a list by page it is
// the page of the rows (Page, a line of the group), and Title is empty.
type plansGroup struct {
	Title string
	Page  map[string]any
	Lines []any
}

// PlansGroups groups the rows of the Plans page. By day (order "day") there are four groups, in this order, each only
// when it has rows: past its day, today, later, and no day. By page each page is a group, in the order of the rows,
// which the API sorts by title. A group that the row limit cuts is drawn again, with its head, on the next page.
func (v *view) PlansGroups() []plansGroup {
	props, _ := v.Props.(map[string]any)
	lines, _ := props["lines"].([]any)
	if props["order"] == "page" {
		var out []plansGroup
		for _, l := range lines {
			m, _ := l.(map[string]any)
			if n := len(out); n > 0 && Num(out[n-1].Page["page_id"]) == Num(m["page_id"]) {
				out[n-1].Lines = append(out[n-1].Lines, l)
			} else {
				out = append(out, plansGroup{Page: m, Lines: []any{l}})
			}
		}
		return out
	}
	today, _ := props["today"].(string)
	groups := []plansGroup{{Title: "Past its day"}, {Title: "Today"}, {Title: "Later"}, {Title: "No day"}}
	for _, l := range lines {
		m, _ := l.(map[string]any)
		day := plannedDay(m)
		i := 3
		switch {
		case day == "":
		case day < today:
			i = 0
		case day == today:
			i = 1
		default:
			i = 2
		}
		groups[i].Lines = append(groups[i].Lines, l)
	}
	return slices.DeleteFunc(groups, func(g plansGroup) bool { return len(g.Lines) == 0 })
}
