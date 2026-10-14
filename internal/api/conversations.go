package api

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"lifelog/internal/core"
)

// ConversationsTitle is the title of the page every conversation is filed in, by part-of.
const ConversationsTitle = "Conversations"

// ConversationTitle is the title of the conversation page of a local day: the conversation is one, continued all
// day, and compacted into the day's journal page at the first question of a later day
// (docs/plans/106-one-continuous-conversation.md).
func ConversationTitle(day string) string { return "Conversation " + day }

// conversationDay is the day of a conversation page, from its title: the ten characters after "Conversation ",
// when they are a day. A title of plan 102, "Conversation YYYY-MM-DD HH.MM.SS", has its day too. "" for a page in
// Conversations whose title names no day: it is read, never compacted, and never the previous conversation.
func conversationDay(title string) string {
	rest, ok := strings.CutPrefix(title, "Conversation ")
	if !ok || len(rest) < 10 || (len(rest) > 10 && rest[10] != ' ') || !core.IsDay(rest[:10]) {
		return ""
	}
	return rest[:10]
}

// Asker answers a question in words with an agent that uses the catalog (internal/chat), and keeps the turn on the
// conversation page of the server's day (docs/plans/102-a-conversation-with-lifelog.md, plan 106).
type Asker interface {
	Ask(ctx context.Context, question string) (Answer, error)
}

// Answer is the result of ask: the conversation page, the model's reply and each tool it called.
type Answer struct {
	Conversation int64  `json:"conversation"`
	Reply        string `json:"reply"`
	Steps        []Step `json:"steps"`
}

// Step is one tool call of a turn: the href of what it answered, or the error it gave back to the model. The
// compaction of an earlier day is a step too, named compact, with the day page it wrote or why it did not.
type Step struct {
	Tool  string `json:"tool"`
	Href  string `json:"href,omitempty"`
	Error string `json:"error,omitempty"`
}

// Turn is one turn of a conversation as its page keeps it: the owner's question and its time (HH.MM, the server's
// local time), the tools the model called, and the model's answer or why there is none.
type Turn struct {
	At       string `json:"at"`
	Question string `json:"question"`
	Steps    []Step `json:"steps"`
	Reply    string `json:"reply,omitempty"`
	Error    string `json:"error,omitempty"`
}

// PreviousConversation is the conversation of the nearest earlier day, and whether it is compacted into its day's
// journal page yet.
type PreviousConversation struct {
	ID        int64  `json:"id"`
	Title     string `json:"title"`
	Day       string `json:"day"`
	Compacted bool   `json:"compacted"`
}

// The transcript format of a conversation page (docs/plans/103-the-conversation-on-the-screen.md). FormatTurn and
// FormatCompacted write it and ParseTurns reads it: this file is its one home.
const (
	ownerMark     = "**Owner** ("
	replyMark     = "**lifelog:** "
	failedMark    = "**lifelog** did not answer: "
	compactedMark = "**Compacted** into [["
)

var (
	ownerLine     = regexp.MustCompile(`^\*\*Owner\*\* \((\d\d\.\d\d)\): ?(.*)$`)
	stepLine      = regexp.MustCompile("^- `([^`]+)`(?: → \\[[^\\]]*\\]\\(([^)]*)\\)|: (.*))?$")
	compactedLine = regexp.MustCompile(`^\*\*Compacted\*\* into \[\[(\d{4}-\d\d-\d\d)\]\]$`)
)

// FormatTurn is one turn as the page keeps it. failure is why the model gave no answer; "" when it answered.
func FormatTurn(at time.Time, question string, steps []Step, reply, failure string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s%s): %s\n\n", ownerMark, at.Format("15.04"), strings.TrimSpace(question))
	for _, s := range steps {
		switch {
		case s.Error != "":
			fmt.Fprintf(&b, "- `%s`: %s\n", s.Tool, firstLine(s.Error))
		case s.Href != "":
			fmt.Fprintf(&b, "- `%s` → [%s](%s)\n", s.Tool, s.Href, s.Href)
		default:
			fmt.Fprintf(&b, "- `%s`\n", s.Tool)
		}
	}
	if len(steps) > 0 {
		b.WriteString("\n")
	}
	if failure != "" {
		fmt.Fprintf(&b, "%s%s\n", failedMark, firstLine(failure))
	} else {
		fmt.Fprintf(&b, "%s%s\n", replyMark, strings.TrimSpace(reply))
	}
	return b.String()
}

// FormatCompacted is the trailer of a conversation page whose summary is on the journal page of day: the page is
// compacted, and its [[day]] links the page that holds the summary.
func FormatCompacted(day string) string { return compactedMark + day + "]]\n" }

// ParseTurns reads the turns of a conversation page's body. Text before the first turn is the preamble. A line the
// format does not know stays in the text it falls in, so an edit by the owner is shown, never dropped. The trailer
// of FormatCompacted belongs to no turn, wherever the owner moved it; compacted is its day, "" when there is none.
func ParseTurns(body string) (preamble string, turns []Turn, compacted string) {
	var pre, question, reply []string
	var cur *Turn
	inReply := false
	end := func() {
		if cur != nil {
			cur.Question = strings.TrimSpace(strings.Join(question, "\n"))
			if inReply && cur.Error == "" {
				cur.Reply = strings.TrimSpace(strings.Join(reply, "\n"))
			} else if cur.Error != "" {
				cur.Error = strings.TrimSpace(cur.Error + "\n" + strings.Join(reply, "\n"))
			}
			turns = append(turns, *cur)
		}
		question, reply, inReply = nil, nil, false
	}
	for _, line := range strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n") {
		if m := compactedLine.FindStringSubmatch(line); m != nil {
			compacted = m[1]
			continue
		}
		if m := ownerLine.FindStringSubmatch(line); m != nil {
			end()
			cur = &Turn{At: m[1], Steps: []Step{}}
			question = []string{m[2]}
			continue
		}
		switch {
		case cur == nil:
			pre = append(pre, line)
		case inReply:
			reply = append(reply, line)
		case strings.HasPrefix(line, replyMark):
			inReply, reply = true, []string{strings.TrimPrefix(line, replyMark)}
		case strings.HasPrefix(line, failedMark):
			inReply, cur.Error = true, strings.TrimPrefix(line, failedMark)
		default:
			if m := stepLine.FindStringSubmatch(line); m != nil {
				cur.Steps = append(cur.Steps, Step{Tool: m[1], Href: m[2], Error: m[3]})
			} else {
				question = append(question, line)
			}
		}
	}
	end()
	return strings.TrimSpace(strings.Join(pre, "\n")), turns, compacted
}

// firstLine is the start of an error: one line, never a whole page.
func firstLine(s string) string {
	s, _, _ = strings.Cut(strings.TrimSpace(s), "\n")
	if len(s) > 300 {
		s = s[:300] + "…"
	}
	return s
}

// NoModel is the refusal of ask when no model is configured.
var NoModel = &core.Error{Status: http.StatusServiceUnavailable, Code: "no_model",
	Msg: "no model is configured: set LIFELOG_MODEL_URL and LIFELOG_MODEL where lifelog runs"}

// ask is POST /conversations. The agent writes as agent:chat whatever the source of the request: the rows say which
// model wrote them. The answer is today's conversation.
func (h *server) ask(r *http.Request, _ string) (*Entity, error) {
	v, err := form(r)
	if err != nil {
		return nil, err
	}
	if err := required(v, "question"); err != nil {
		return nil, err
	}
	if h.asker == nil {
		return nil, NoModel
	}
	a, err := h.asker.Ask(r.Context(), v.Get("question"))
	if err != nil {
		return nil, err
	}
	if wantsHTML(r) { // the chat page shows the turn: no toast repeats it
		return h.todayConversation(r.Context())
	}
	return withResult(h.todayConversation(r.Context()))(a)
}

// isConversation says whether a page is filed in Conversations: a turn is appended to such a page and to no other.
func isConversation(p *core.Page) bool {
	for _, l := range p.Out {
		if l.Kind == "part-of" && l.Title == ConversationsTitle {
			return true
		}
	}
	return false
}

func conversationHref(id int64) string { return "/conversations/" + strconv.FormatInt(id, 10) }

// conversationRow is a conversation in the list of the Conversations page.
type conversationRow struct {
	ID    int64
	Title string
}

// conversationList is every live conversation, the newest first: the live pages filed in Conversations.
func (h *server) conversationList(ctx context.Context) ([]conversationRow, error) {
	rows := []conversationRow{}
	id, err := h.s.PageID(ctx, ConversationsTitle)
	if err != nil || id == 0 {
		return rows, err
	}
	p, err := h.s.PageByID(ctx, id)
	if err != nil || p.DeletedAt != "" {
		return rows, err
	}
	for _, l := range p.In {
		if l.Kind == "part-of" && l.Type == "page" {
			rows = append(rows, conversationRow{ID: l.ID, Title: l.Title})
		}
	}
	slices.SortFunc(rows, func(a, b conversationRow) int { return int(b.ID - a.ID) })
	return rows, nil
}

// previousConversation is the newest conversation with a day before the page before (every one when before is 0),
// and whether it is compacted. nil when there is none.
func (h *server) previousConversation(ctx context.Context, before int64) (*PreviousConversation, error) {
	rows, err := h.conversationList(ctx)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		if before != 0 && row.ID >= before {
			continue
		}
		day := conversationDay(row.Title)
		if day == "" {
			continue
		}
		p, err := h.s.PageByID(ctx, row.ID)
		if err != nil {
			return nil, err
		}
		_, _, compacted := ParseTurns(p.Body)
		return &PreviousConversation{ID: row.ID, Title: row.Title, Day: day, Compacted: compacted != ""}, nil
	}
	return nil, nil
}

// memoryDay is the day of the nearest live journal page in the seven days before day: the memory the model is given
// for the conversation of day. "" when there is none.
func (h *server) memoryDay(ctx context.Context, day string) (string, error) {
	t, ok := dayOf(day)
	if !ok {
		return "", nil
	}
	entries, err := h.s.DayEntries(ctx, t.AddDate(0, 0, -7).Format(time.DateOnly), t.AddDate(0, 0, -1).Format(time.DateOnly), true, 1)
	if err != nil || len(entries) == 0 {
		return "", err
	}
	return entries[0].Day, nil
}

// todayConversation is GET /conversations: the conversation of the server's day, with no page yet when nothing was
// asked today.
func (h *server) todayConversation(ctx context.Context) (*Entity, error) {
	day := h.localDay()
	id, err := h.s.PageID(ctx, ConversationTitle(day))
	if err != nil {
		return nil, err
	}
	var p *core.Page
	if id != 0 {
		if p, err = h.s.PageByID(ctx, id); err != nil {
			return nil, err
		}
		if p.DeletedAt != "" || !isConversation(p) {
			p = nil
		}
	}
	return h.conversationView(ctx, p, day, "/conversations")
}

// conversation is GET /conversations/{id}: one conversation and its turns.
func (h *server) conversation(r *http.Request) (*Entity, error) {
	id, err := idOf(r)
	if err != nil {
		return nil, err
	}
	p, err := h.s.PageByID(r.Context(), id)
	if err != nil {
		return nil, err
	}
	if p.DeletedAt != "" || !isConversation(p) {
		return nil, core.NotFound("no conversation %d", id)
	}
	return h.conversationView(r.Context(), p, conversationDay(p.Title), conversationHref(id))
}

// conversationView is a conversation as a resource: its page (nil when the day has none yet), its day ("" for a page
// whose title names none), the previous conversation and the memory the model is given. ask is offered on the
// conversation of the server's day only: the conversation is one, continued all day.
func (h *server) conversationView(ctx context.Context, p *core.Page, day, self string) (*Entity, error) {
	title := ConversationTitle(day)
	props := map[string]any{"day": day, "turns": []Turn{}, "compacted": false}
	var id int64
	if p != nil {
		id, title = p.ID, p.Title
		preamble, turns, compacted := ParseTurns(p.Body)
		if turns == nil {
			turns = []Turn{}
		}
		props["id"], props["version"], props["created_at"] = id, p.Version, p.CreatedAt
		props["turns"], props["compacted"] = turns, compacted != ""
		if preamble != "" {
			props["preamble"] = preamble
		}
	}
	props["title"] = title
	links := []Link{link("self", self, title)}
	if p != nil {
		links = append(links, link("page", pageHref(id), "Its page"))
	}
	if self != "/conversations" {
		links = append(links, link("today", "/conversations", "Chat"))
	}
	prev, err := h.previousConversation(ctx, id)
	if err != nil {
		return nil, err
	}
	if prev != nil {
		props["previous"] = prev
		links = append(links, link("previous", conversationHref(prev.ID), prev.Title))
	}
	if day != "" {
		m, err := h.memoryDay(ctx, day)
		if err != nil {
			return nil, err
		}
		if m != "" {
			links = append(links, link("memory", dayHref(m), m))
		}
	}
	e := &Entity{Class: []string{"conversation"}, Title: title, Properties: props, Links: append(links, link("index", "/", "Home"))}
	if day == h.localDay() {
		e.Actions = []Action{action("ask", nil, nil)}
	}
	return e, nil
}

// conversationRedirect is where a browser goes for a conversation's page: its chat page, with a receipt of feedback.
func (h *server) conversationRedirect(r *http.Request, id int64) redirect {
	to := conversationHref(id)
	if t := r.URL.Query().Get("feedback"); t != "" && h.feedback.move(t, r.URL.Path, to) {
		to += "?" + url.Values{"feedback": {t}}.Encode()
	}
	return redirect{to}
}
