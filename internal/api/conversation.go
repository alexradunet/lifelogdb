package api

// The conversation of a day: a plain page, as API.md describes it. This file is the one home of its convention in
// the program: the title, the category, the first line, the owner's paragraph and the parse of the turns. The core
// calls no model. An agent (an extension) reads the page and writes its own paragraphs in the same format.

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strings"

	"lifelog/internal/api/siren"
	"lifelog/internal/core"
	"lifelog/internal/text"
)

// conversationCategory is the category page that every conversation page is filed in.
const conversationCategory = "Conversations"

// ConversationTitle is the title of the conversation page of a local day.
func ConversationTitle(day string) string { return "Conversation " + day }

// conversationFirstLine is the first line of the page: a link to the day it is about.
func conversationFirstLine(day string) string { return "[[" + day + "]]" }

// ownerParagraph is the paragraph of one turn of the owner: the stamp is the time in the server's zone, HH.MM. The
// text has LF line ends (a browser sends CRLF) and no blank edge.
func ownerParagraph(stamp, words string) string {
	return "**Owner** (" + stamp + "): " + strings.TrimSpace(strings.ReplaceAll(words, "\r\n", "\n"))
}

// conversationDay is the day of a conversation page, from its title; "" when the title is not one.
func conversationDay(title string) string {
	day, ok := strings.CutPrefix(title, "Conversation ")
	if !ok || !text.IsDay(day) || ConversationTitle(day) != title {
		return ""
	}
	return day
}

// Turn is one turn of a conversation, as ParseTurns reads it. Who is owner, lifelog, or text for a paragraph that no
// turn starts (the owner edited the page): At is then empty. Tools are the tool lines of a lifelog turn.
type Turn struct {
	Who   string   `json:"who"`
	At    string   `json:"at,omitempty"`
	Text  string   `json:"text"`
	Tools []string `json:"tools,omitempty"`
}

var turnStart = regexp.MustCompile(`^\*\*(Owner|lifelog)\*\* \((\d\d\.\d\d)\): ?`)

// ParseTurns reads the turns of the body of the conversation page of day. A paragraph is a block between blank
// lines (CRLF is read as LF). The first line of a turn is `**Owner** (HH.MM): ` or `**lifelog** (HH.MM): `. A
// paragraph of lines that start with "- `" right before a lifelog turn is that turn's tool lines. The first line of
// the page, [[day]], is no turn. Any other paragraph is a turn of Who text, shown as it is.
func ParseTurns(day, body string) []Turn {
	turns := []Turn{}
	var tools []string // a block of tool lines that waits for the paragraph after it
	flush := func() {  // the block was not followed by a lifelog turn: it is a free paragraph
		if tools != nil {
			turns = append(turns, Turn{Who: "text", Text: strings.Join(tools, "\n")})
			tools = nil
		}
	}
	for i, para := range paragraphs(body) {
		lines := strings.Split(para, "\n")
		if i == 0 && para == conversationFirstLine(day) {
			continue
		}
		if toolLines(lines) {
			flush()
			tools = lines
			continue
		}
		m := turnStart.FindStringSubmatch(para)
		if m == nil {
			flush()
			turns = append(turns, Turn{Who: "text", Text: para})
			continue
		}
		t := Turn{Who: strings.ToLower(m[1]), At: m[2], Text: strings.TrimSpace(para[len(m[0]):])}
		if t.Who == "lifelog" {
			t.Tools, tools = tools, nil
		}
		flush()
		turns = append(turns, t)
	}
	flush()
	return turns
}

// paragraphs are the blocks of body between blank lines, each without its blank edge.
func paragraphs(body string) []string {
	var out, cur []string
	end := func() {
		if len(cur) > 0 {
			out = append(out, strings.Join(cur, "\n"))
			cur = nil
		}
	}
	for _, line := range strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n") {
		if strings.TrimSpace(line) == "" {
			end()
			continue
		}
		cur = append(cur, strings.TrimRight(line, " \t"))
	}
	end()
	return out
}

// toolLines says whether every line starts with "- `": the tool lines of an agent's turn.
func toolLines(lines []string) bool {
	return !slices.ContainsFunc(lines, func(l string) bool { return !strings.HasPrefix(l, "- `") })
}

// say is the owner's turn: it adds the paragraph to the conversation page of the day, made on the first turn.
func (h *server) say(r *http.Request, act spec, src string) (*siren.Entity, error) {
	v, err := fieldsOf(r, act)
	if err != nil {
		return nil, err
	}
	day, err := h.conversationDayOf(r)
	if err != nil {
		return nil, err
	}
	words := strings.TrimSpace(v.Get("text"))
	if words == "" {
		return nil, core.Invalid("missing field text")
	}
	_, sync, err := h.s.AppendParagraph(r.Context(), src, ConversationTitle(day), day, conversationCategory,
		conversationFirstLine(day), ownerParagraph(h.s.Local().Format("15.04"), words))
	if err != nil {
		return nil, err
	}
	return withResult(h.conversationEntity(r.Context(), day))(sync)
}

// conversation is GET /days/{day}/conversation.
func (h *server) conversation(r *http.Request) (*siren.Entity, error) {
	day, err := h.conversationDayOf(r)
	if err != nil {
		return nil, err
	}
	return h.conversationEntity(r.Context(), day)
}

// conversationDayOf is the {day} of the path: a day, or the word today.
func (h *server) conversationDayOf(r *http.Request) (string, error) {
	day := h.dayNamed(r.PathValue("day"))
	if !text.IsDay(day) {
		return "", core.Invalid("day %q is not a local day: YYYY-MM-DD or today", day)
	}
	return day, nil
}

// conversationHref is the address of the conversation of a day.
func conversationHref(day string) string { return siren.DayHref(day) + "/conversation" }

// conversationEntity is the conversation of a day: its turns, and say. When the page exists it carries the page as a
// metric's series does (plan 144): its properties, links and actions, so one view draws both.
func (h *server) conversationEntity(ctx context.Context, day string) (*siren.Entity, error) {
	title := ConversationTitle(day)
	e := &siren.Entity{Class: []string{"conversation"}, Title: title,
		Links:   []siren.Link{link("self", conversationHref(day), title), link("day", siren.DayHref(day), day), link("index", "/", "Home")},
		Actions: []siren.Action{action("say", map[string]string{"day": day}, nil)}}
	props := map[string]any{"day": day, "turns": []Turn{}}
	e.Properties = props
	id, err := h.s.PageID(ctx, title)
	if err != nil || id == 0 {
		return e, err
	}
	pe, err := h.pageEntity(ctx, id)
	if err != nil {
		return nil, err
	}
	if slices.Contains(pe.Class, "deleted") { // a tombstoned page is no conversation: say revives it
		return e, nil
	}
	if err := h.carryPage(ctx, e, id); err != nil {
		return nil, err
	}
	e.Class = append(e.Class, "page")
	e.Links = append(e.Links, link("page", siren.PageHref(id), title))
	props["turns"] = ParseTurns(day, fmt.Sprint(props["body"]))
	return e, nil
}
