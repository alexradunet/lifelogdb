package view

import (
	"html/template"
	"slices"
	"strings"

	"lifelog/internal/text"
	"lifelog/siren"
)

// The cards of a thread (docs/plans/148-the-chat-screen.md): each address that an agent's tool line names is a thing
// of the database. The server asks its own handler for the entity at the address (Facts.Cards); this file draws the
// entity as a card by its class, as the browser draws a value by its type. A card is one link to the thing.

// CardsPerTurn is how many cards a turn draws, and so how many of its addresses the server resolves.
const CardsPerTurn = 6

// Card is one address of a turn, as the thread draws it. Kind picks the parts: link (the address alone), day,
// page, person, place, file, metric, series, measurement, plan, search, conversation or other. A part that a kind does not use stays empty. The
// compact form of the column draws the icon, the title and the value; the rest is the full form's.
type Card struct {
	Href, Kind, Title string
	Icon              string
	Value             string // the one value of the compact form: a mood, a reading, a count
	Day               string // the day of a leaf
	Lines             []string
	Tiles             []readingTile
	Picture           string
	Spark             template.HTML
	Rows              []string
}

// CardsOf are the cards of a turn's addresses, in order, at most CardsPerTurn. An address that the server did not
// resolve (an older turn, beyond the bound of the view) is the plain link, as an address that answered an error.
func (v *view) CardsOf(refs []string) []Card {
	var out []Card
	for _, ref := range refs[:min(len(refs), CardsPerTurn)] {
		out = append(out, v.cardOf(ref, v.cards[ref]))
	}
	return out
}

// cardIcons are the icons of the kinds of a card that is not a leaf.
var cardIcons = map[string]string{"page": "files", "person": "people", "place": "places", "file": "image", "metric": "metrics",
	"series": "metrics", "measurement": "metrics", "plan": "plans", "search": "search", "conversation": "chat", "day": "calendar"}

// cardOf is the card of the entity e at href. A nil entity, an entity with a property that JSON cannot carry, and
// an entity of no title give the plain link.
func (v *view) cardOf(href string, e *siren.Entity) Card {
	link := Card{Href: href, Kind: "link", Title: linkWords(href)}
	if e == nil {
		return link
	}
	shape, err := jsonShape(e.Properties)
	if err != nil {
		return link
	}
	props, _ := shape.(map[string]any)
	cv := &view{Entity: e, Props: shape, Shell: v.Shell}
	c := Card{Href: href, Title: e.Title}
	line := planLineOf(href, props)
	switch kind := cardKind(e.Class); {
	case line != nil:
		c = cv.planCard(c, line)
	case kind == "day" || kind == "day-page":
		c = cv.dayCard(c, props, kind)
	case kind == "series":
		c = cv.seriesCard(c, props)
	case kind == "measurement":
		c.Kind, c.Title = "measurement", Num(props["metric"])
		c.Value = cv.valueWords(props["value"], Num(props["unit"]), Num(props["day"]))
	case kind == "search":
		c = cv.searchCard(c, props)
	case kind == "conversation":
		c.Kind, c.Day = "conversation", Num(props["day"])
		c.Title = "Chat, " + cv.Date(c.Day)
		c.Value = Count(len(anyList(props["turns"])), "turn", "turns")
	case kind != "":
		c = cv.pageCard(c, props, kind)
	default:
		c.Kind = "other"
		if len(e.Class) > 0 {
			c.Value = e.Class[0]
		}
	}
	if c.Title == "" {
		c.Title = linkWords(href)
	}
	c.Icon = cardIcons[c.Kind]
	return c
}

// linkWords are the words of a plain link: the address, cut at 80 characters with an ellipsis. The href stays whole.
func linkWords(href string) string {
	if r := []rune(href); len(r) > 80 {
		return string(r[:80]) + "…"
	}
	return href
}

// cardKind picks the kind of an entity from its classes. A conversation is also a page, and a day page is a page
// and a day page: the more particular class wins. A kind of page is its own (person, place, file, metric, page); "" is a class without a card.
func cardKind(class []string) string {
	for _, k := range []string{"conversation", "day", "day-page", "series", "measurement", "search", "person", "place", "file", "metric", "page"} {
		if slices.Contains(class, k) {
			return k
		}
	}
	return ""
}

// valueWords is a reading as a line says it: "7 h · Tue 27 Oct".
func (v *view) valueWords(value any, unit, day string) string {
	words := Num(value)
	if unit != "" {
		words += " " + unitWords(unit)
	}
	if day != "" {
		short, _, _ := strings.Cut(v.clock().relative(day), " · ")
		words += " · " + short
	}
	return words
}

// plainLines are the first n lines of a body with words in them, without their CommonMark. A line that is an embed
// (`![[…]]`) has none.
func plainLines(body string, n int) []string {
	var out []string
	for line := range strings.SplitSeq(body, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "![[") { // an embed (a cover) is a picture, not a line
			continue
		}
		if w := plainSnippet(line); w != "" {
			out = append(out, w)
			if len(out) == n {
				break
			}
		}
	}
	return out
}

// dayCard is a day, or a day page: the leaf, the mood, the first two lines of the journal and the readings.
func (v *view) dayCard(c Card, props map[string]any, kind string) Card {
	c.Kind, c.Day = "day", Num(props["day"])
	if !text.IsDay(c.Day) {
		c.Day = c.Title
	}
	c.Title = v.Date(c.Day)
	if kind == "day" {
		if journal := v.DayParts().Journal; len(journal) > 0 {
			c.Lines = plainLines(journal[0], 2)
		}
		for _, t := range v.ReadingTiles() {
			if t.ID != "" {
				c.Tiles = append(c.Tiles, t)
			}
		}
		if mood := anyList(props["mood"]); len(mood) > 0 {
			c.Value = "mood " + Num(mood[0])
			for _, m := range mood[1:] {
				c.Value += ", " + Num(m)
			}
		}
		return c
	}
	c.Lines = plainLines(Num(props["body"]), 2)
	return c
}

// pageCard is a page, a person, a place, a file or a metric's page: its title, the first line of its body and its
// cover (a file's own picture when it has no cover).
func (v *view) pageCard(c Card, props map[string]any, kind string) Card {
	c.Kind = kind
	c.Lines = plainLines(Num(props["body"]), 1)
	if cover, _ := props["cover"].(map[string]any); cover != nil {
		c.Picture = "/pages/" + Num(cover["id"]) + "/preview"
	} else if kind == "file" {
		c.Picture = v.Link("preview")
	}
	return c
}

// seriesCard is a metric: its name, its latest reading with the unit and the day, and the line of the recent readings.
func (v *view) seriesCard(c Card, props map[string]any) Card {
	c.Kind = "series"
	unit := Num(props["unit"])
	readings := anyList(props["readings"])
	if n := len(readings); n > 0 {
		last, _ := readings[n-1].(map[string]any)
		c.Value = v.valueWords(last["value"], unit, Num(last["day"]))
		c.Spark = sparkLine(v.clock(), c.Title, unit, readings[max(0, n-30):])
	}
	return c
}

// searchCard is a search: the count of its hits and the first three of them.
func (v *view) searchCard(c Card, props map[string]any) Card {
	c.Kind = "search"
	hits := anyList(props["hits"])
	c.Title = "Search: " + Num(props["q"])
	c.Value = Count(len(hits), "hit", "hits")
	for _, h := range hits[:min(3, len(hits))] {
		m, _ := h.(map[string]any)
		c.Rows = append(c.Rows, v.Date(m["title"]))
	}
	return c
}

// planCard is one plan line: its words, its box and its dates.
func (v *view) planCard(c Card, line map[string]any) Card {
	c.Kind, c.Title = "plan", Num(line["text"])
	c.Value = map[string]string{" ": "open", "x": "done", "-": "dropped", ">": "carried"}[Num(line["box"])]
	for _, d := range []struct{ key, word string }{{"on", "on"}, {"due", "due"}, {"done", "done"}} {
		if day := Num(line[d.key]); day != "" {
			c.Lines = append(c.Lines, d.word+" "+v.Date(day))
		}
	}
	return c
}

// planLineOf is the plan line that an address names with a fragment, #line-N: the entity at the address lists the
// line in planned[] (a day) or in lines[] (a period page), and the line is on the page of the entity. nil when the
// address names no line or the entity does not list it.
func planLineOf(href string, props map[string]any) map[string]any {
	_, frag, ok := strings.Cut(href, "#line-")
	if !ok || props == nil {
		return nil
	}
	owner := Num(props["page_id"])
	if owner == "" {
		owner = Num(props["id"])
	}
	for _, list := range []string{"planned", "lines"} {
		for _, x := range anyList(props[list]) {
			if m, _ := x.(map[string]any); m != nil && Num(m["line"]) == frag && Num(m["page_id"]) == owner {
				return m
			}
		}
	}
	return nil
}
