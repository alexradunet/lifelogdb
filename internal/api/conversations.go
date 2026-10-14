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

// Asker answers a question in words with an agent that uses the catalog (internal/chat), and keeps the turn on a
// conversation page (docs/plans/102-a-conversation-with-lifelog.md). conversation is the page to continue, 0 for a
// new one.
type Asker interface {
	Ask(ctx context.Context, question string, conversation int64) (Answer, error)
}

// Answer is the result of ask: the conversation page, the model's reply and each tool it called.
type Answer struct {
	Conversation int64  `json:"conversation"`
	Reply        string `json:"reply"`
	Steps        []Step `json:"steps"`
}

// Step is one tool call of a turn: the href of what it answered, or the error it gave back to the model.
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

// The transcript format of a conversation page (docs/plans/103-the-conversation-on-the-screen.md). FormatTurn
// writes it and ParseTurns reads it: this file is its one home.
const (
	ownerMark  = "**Owner** ("
	replyMark  = "**lifelog:** "
	failedMark = "**lifelog** did not answer: "
)

var (
	ownerLine = regexp.MustCompile(`^\*\*Owner\*\* \((\d\d\.\d\d)\): ?(.*)$`)
	stepLine  = regexp.MustCompile("^- `([^`]+)`(?: → \\[[^\\]]*\\]\\(([^)]*)\\)|: (.*))?$")
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

// ParseTurns reads the turns of a conversation page's body. Text before the first turn is the preamble. A line the
// format does not know stays in the text it falls in, so an edit by the owner is shown, never dropped.
func ParseTurns(body string) (preamble string, turns []Turn) {
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
	return strings.TrimSpace(strings.Join(pre, "\n")), turns
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
// model wrote them. The answer is the conversation.
func (h *server) ask(r *http.Request, _ string) (*Entity, error) {
	v, err := form(r)
	if err != nil {
		return nil, err
	}
	if err := required(v, "question"); err != nil {
		return nil, err
	}
	var conversation int64
	if c := v.Get("conversation"); c != "" {
		if conversation, err = strconv.ParseInt(c, 10, 64); err != nil || conversation <= 0 {
			return nil, core.Invalid("conversation must be the id of a conversation page")
		}
	}
	if conversation != 0 {
		p, err := h.s.PageByID(r.Context(), conversation)
		if err != nil {
			return nil, err
		}
		if p.DeletedAt != "" || !isConversation(p) {
			return nil, core.Invalid("page %d is not a live conversation: a turn is appended only to a page filed in %s", conversation, ConversationsTitle)
		}
	}
	if h.asker == nil {
		return nil, NoModel
	}
	a, err := h.asker.Ask(r.Context(), v.Get("question"), conversation)
	if err != nil {
		return nil, err
	}
	if wantsHTML(r) { // the chat page shows the turn: no toast repeats it
		return h.conversationEntity(r.Context(), a.Conversation)
	}
	return withResult(h.conversationEntity(r.Context(), a.Conversation))(a)
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

// conversationRow is a conversation in the list.
type conversationRow struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
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

// conversations is GET /conversations: the list, and ask for a new conversation.
func (h *server) conversations(r *http.Request) (*Entity, error) {
	list, err := h.conversationList(r.Context())
	if err != nil {
		return nil, err
	}
	return &Entity{Class: []string{"conversations"}, Title: "Chat",
		Properties: map[string]any{"conversations": list},
		Links:      []Link{link("self", "/conversations", "Chat"), link("index", "/", "Home")},
		Actions:    []Action{action("ask", nil, nil)}}, nil
}

// conversation is GET /conversations/{id}: one conversation and its turns.
func (h *server) conversation(r *http.Request) (*Entity, error) {
	id, err := idOf(r)
	if err != nil {
		return nil, err
	}
	return h.conversationEntity(r.Context(), id)
}

func (h *server) conversationEntity(ctx context.Context, id int64) (*Entity, error) {
	p, err := h.s.PageByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if p.DeletedAt != "" || !isConversation(p) {
		return nil, core.NotFound("no conversation %d", id)
	}
	list, err := h.conversationList(ctx)
	if err != nil {
		return nil, err
	}
	preamble, turns := ParseTurns(p.Body)
	if turns == nil {
		turns = []Turn{}
	}
	props := map[string]any{"id": id, "title": p.Title, "version": p.Version, "created_at": p.CreatedAt,
		"turns": turns, "conversations": list}
	if preamble != "" {
		props["preamble"] = preamble
	}
	return &Entity{Class: []string{"conversation"}, Title: p.Title, Properties: props,
		Links: []Link{link("self", conversationHref(id), p.Title), link("page", pageHref(id), "Its page"),
			link("conversations", "/conversations", "Chat"), link("index", "/", "Home")},
		Actions: []Action{action("ask", nil, map[string]any{"conversation": id})}}, nil
}

// conversationRedirect is where a browser goes for a conversation's page: its chat page, with a receipt of feedback.
func (h *server) conversationRedirect(r *http.Request, id int64) redirect {
	to := conversationHref(id)
	if t := r.URL.Query().Get("feedback"); t != "" && h.feedback.move(t, r.URL.Path, to) {
		to += "?" + url.Values{"feedback": {t}}.Encode()
	}
	return redirect{to}
}
