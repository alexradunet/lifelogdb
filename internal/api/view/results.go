package view

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net/url"
	"sort"
	"strings"
)

// The result of a write in sentences (docs/plans/093-words-phone-and-today.md), by the result shapes of API.md.
// The receipt carries the result's JSON across the browser's redirect; here it becomes the toast's lines. A key that
// no sentence knows is shown as a fact, so nothing of a result is lost.

// resultLine is one line of the toast: a sentence, or the facts of what no sentence said. A sentence may carry one
// form, a button that posts one more write (the Undo of a tick).
type resultLine struct {
	HTML template.HTML
	Form *resultForm
}

// resultForm is a post that a toast offers: its address, its hidden fields in order and the words of its button.
type resultForm struct {
	Href   string
	Hidden [][2]string
	Label  string
}

// ResultLines are the toast's lines for the view's result; a result that JSON cannot carry is an error.
func (v *view) ResultLines() ([]resultLine, error) {
	var x any
	switch r := v.Result.(type) {
	case string:
		if err := json.Unmarshal([]byte(r), &x); err != nil {
			return []resultLine{{HTML: template.HTML(template.HTMLEscapeString(r))}}, nil
		}
	default:
		var err error
		if x, err = jsonShape(r); err != nil {
			return nil, err
		}
	}
	m, ok := x.(map[string]any)
	if !ok {
		var sb strings.Builder
		v.clock().writeValue(&sb, x)
		return []resultLine{{HTML: template.HTML(sb.String())}}, nil
	}
	return resultSentences(m, v.clock(), v.Here), nil
}

// resultSentences says a result object. Each key a sentence says is taken; what is left is drawn as facts. here is the
// page the toast is drawn on, where the form of a sentence returns to.
func resultSentences(m map[string]any, c clock, here string) []resultLine {
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
	say := func(format string, a ...any) {
		out = append(out, resultLine{HTML: template.HTML(fmt.Sprintf(format, a...))})
	}

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
		say(`Kept the file: <a href="/pages/%s">its page</a>.`, Num(id))
	}
	if e, ok := left["existing"].(bool); ok { // add-file's
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
		say("Its page keeps the day %s.", c.timeHTML(fmt.Sprint(d)))
	}
	if d, ok := take("day"); ok {
		say("Its own day is %s.", c.timeHTML(fmt.Sprint(d)))
	}
	if b, _ := take("embedded"); b == true {
		say("Shown on its day page.")
	}

	// plan, tick and carry (docs/plans/136-plan-lines-and-period-pages.md). The line is the number in the body.
	line, hasLine := take("line")
	if _, ok := take("written"); ok {
		pageID, _ := take("page_id")
		say(`Planned on line %s of <a href="/pages/%s">its page</a>.`, Num(line), Num(pageID))
	} else if outcome, ok := take("outcome"); ok {
		pageID, _ := take("page_id")
		words, hasWords := take("text")
		switch outcome {
		case "reopen":
			say("Reopened.")
			if gone, ok := take("removed_line"); ok {
				delete(left, "removed")
				say("The next one, line %s, is removed.", Num(gone))
			}
			if b, _ := take("next_kept"); b == true {
				say("The next one was changed since, so it stays.")
			}
		default:
			say("%s.", map[bool]string{true: "Done", false: "Dropped"}[outcome == "done"])
			// The Undo posts a reopen of the same line to its page, and the browser returns to where it is.
			if n := Num(pageID); n != "" && n != "0" && hasLine && hasWords {
				f := &resultForm{Href: lineHref("tick", pageID), Label: "Undo",
					Hidden: [][2]string{{"line", Num(line)}, {"text", fmt.Sprint(words)}, {"outcome", "reopen"}}}
				if here != "" {
					f.Hidden = append([][2]string{{"back", here}}, f.Hidden...)
				}
				out[len(out)-1].Form = f
			}
			if next, ok := take("appended_line"); ok {
				delete(left, "appended")
				say("The next one is line %s.", Num(next))
			}
		}
	} else if to, ok := take("to_id"); ok {
		toLine, _ := take("to_line")
		say(`Carried to <a href="/pages/%s">its page</a>, line %s.`, Num(to), Num(toLine))
	} else if hasLine {
		left["line"] = line
	}

	// capture sent again (D28).
	if b, _ := take("already"); b == true {
		say("Already added. Nothing changed.")
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
		out = append(out, resultLine{HTML: template.HTML(sb.String())})
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
