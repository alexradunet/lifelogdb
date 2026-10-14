package api

import (
	"context"
	"net/http"
	"strconv"

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

// NoModel is the refusal of ask when no model is configured.
var NoModel = &core.Error{Status: http.StatusServiceUnavailable, Code: "no_model",
	Msg: "no model is configured: set LIFELOG_MODEL_URL and LIFELOG_MODEL where lifelog runs"}

// ask is POST /conversations. The agent writes as agent:chat whatever the source of the request: the rows say which
// model wrote them.
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
	return withResult(h.pageEntity(r.Context(), a.Conversation))(a)
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
