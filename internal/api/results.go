package api

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

// The result of a write in sentences (docs/plans/093-words-phone-and-today.md), by the result shapes of API.md.
// The receipt carries the result's JSON across the browser's redirect; here it becomes the toast's lines. A key that
// no sentence knows is shown as a fact, so nothing of a result is lost.

// resultLine is one line of the toast: a sentence, or the facts of what no sentence said.
type resultLine = template.HTML

// ResultLines are the toast's lines for the view's result.
func (v *view) ResultLines() []resultLine {
	var x any
	switch r := v.Result.(type) {
	case string:
		if err := json.Unmarshal([]byte(r), &x); err != nil {
			return []resultLine{template.HTML(template.HTMLEscapeString(r))}
		}
	default:
		x = jsonShape(r)
	}
	m, ok := x.(map[string]any)
	if !ok {
		var sb strings.Builder
		v.clock().writeValue(&sb, x)
		return []resultLine{template.HTML(sb.String())}
	}
	return resultSentences(m, v.clock())
}

// resultSentences says a result object. Each key a sentence says is taken; what is left is drawn as facts.
func resultSentences(m map[string]any, c clock) []resultLine {
	left := map[string]any{}
	for k, v := range m {
		left[k] = v
	}
	take := func(k string) (any, bool) {
		v, ok := left[k]
		delete(left, k)
		return v, ok && v != nil
	}
	esc := template.HTMLEscapeString
	var out []resultLine
	say := func(format string, a ...any) { out = append(out, template.HTML(fmt.Sprintf(format, a...))) }

	// The sync shape: what the save contract did with the [[links]] and #tags of a body. Linked says the links this
	// save added, not every link of the page again; the whole list is the JSON's.
	sync := func(m map[string]any, where string) {
		if _, isList := m["linked"].([]any); isList {
			delete(m, "linked")
		}
		for _, s := range []struct{ key, words string }{{"added", "Linked"}, {"created", "Created"}, {"revived", "Revived"}, {"skipped", ""}} {
			list, isList := m[s.key].([]any)
			if !isList {
				continue // add-file's linked is a yes or no: its own sentence says it
			}
			delete(m, s.key)
			names := stringList(list)
			switch {
			case len(names) == 0:
			case s.key == "skipped":
				var quoted []string
				for _, n := range names {
					quoted = append(quoted, "<code>[["+esc(n)+"]]</code>")
				}
				say("%sNot linked, not a valid title: %s.", where, strings.Join(quoted, ", "))
			default:
				say("%s%s %s.", where, s.words, pageLinks(names))
			}
		}
	}
	sync(left, "")
	for _, k := range []struct{ key, where string }{{"sync", "In its page: "}, {"day_sync", "In its day page: "}} {
		if s, ok := left[k.key].(map[string]any); ok {
			delete(left, k.key)
			sync(s, k.where)
			for name, value := range s {
				left[k.key+"_"+name] = value
			}
		}
	}

	// add-file.
	if id, ok := take("id"); ok {
		say(`Kept the file: <a href="/pages/%s">its page</a>.`, num(id))
	}
	if e, ok := left["existing"].(bool); ok { // add-file's; readings-from-table's is a count
		delete(left, "existing")
		if e {
			say("The same original was kept before.")
		}
	}
	if b, _ := take("deleted"); b == true {
		say("Its page is deleted.")
	}
	if b, _ := take("preview_added"); b == true {
		say("Added its picture.")
	}
	if b, _ := take("promoted"); b == true {
		say("A page of that title became the file.")
	}
	if d, ok := take("stored_day"); ok {
		say("It was filed under %s before.", c.timeHTML(fmt.Sprint(d)))
	}
	if d, ok := take("day"); ok {
		say("Filed under %s.", c.timeHTML(fmt.Sprint(d)))
	}
	if p, ok := take("place"); ok {
		say("At %s.", pageLinks([]string{fmt.Sprint(p)}))
	}
	if b, _ := take("linked"); b == true { // add-file's own linked: its day got an at link
		say("Its day is linked to the place.")
	}
	if b, _ := take("point_set"); b == true {
		say("The place took this position as its point.")
	}
	if u, ok := take("unmatched"); ok {
		p, _ := u.(map[string]any)
		say(`Near no known place: %s, %s (<a href="%s" rel="noreferrer">map</a>).`, esc(num(p["lat"])), esc(num(p["lon"])), esc(fmt.Sprint(p["map"])))
	}
	if b, _ := take("embedded"); b == true {
		say("Shown on its day page.")
	}

	// readings-from-table.
	if n, ok := take("written"); ok {
		say("Wrote %s.", count(n, "reading", "readings"))
	}
	if n, ok := left["existing"].(float64); ok {
		delete(left, "existing")
		say("%s there before.", count(n, "reading was", "readings were"))
	}
	if rows, ok := take("reported"); ok {
		if list, _ := rows.([]any); len(list) > 0 {
			var sb strings.Builder
			sb.WriteString(count(float64(len(list)), "row gave", "rows gave") + " no reading:<ul>")
			for _, r := range list {
				row, _ := r.(map[string]any)
				sb.WriteString("<li>row " + esc(num(row["row"])) + ": " + esc(fmt.Sprint(row["reason"])) + "</li>")
			}
			sb.WriteString("</ul>")
			out = append(out, template.HTML(sb.String()))
		}
	}

	// snapshot.
	if p, ok := take("snapshot"); ok {
		say("Took a snapshot: <code>%s</code>.", esc(fmt.Sprint(p)))
	}

	if len(out) == 0 && len(left) == 0 {
		say("Done.")
	}
	if len(left) > 0 {
		var sb strings.Builder
		c.writeValue(&sb, left)
		out = append(out, template.HTML(sb.String()))
	}
	return out
}

// stringList are the strings of a JSON list, in order.
func stringList(list []any) []string {
	var out []string
	for _, it := range list {
		if s, ok := it.(string); ok {
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

// pageLinks links each title to its page: "A, B and C".
func pageLinks(titles []string) string {
	var links []string
	for _, t := range titles {
		links = append(links, `<a href="/pages?title=`+template.HTMLEscapeString(url.QueryEscape(t))+`">`+template.HTMLEscapeString(t)+`</a>`)
	}
	switch len(links) {
	case 0:
		return ""
	case 1:
		return links[0]
	}
	return strings.Join(links[:len(links)-1], ", ") + " and " + links[len(links)-1]
}

// count says a number and its noun: "1 reading", "3 readings".
func count(n any, one, many string) string {
	f, _ := n.(float64)
	if f == 1 {
		return "1 " + one
	}
	return strconv.FormatFloat(f, 'f', -1, 64) + " " + many
}
