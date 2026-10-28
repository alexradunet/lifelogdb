package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"time"

	"lifelog/internal/api/siren"
	"lifelog/internal/api/view"
)

// The cards of the chat (docs/plans/148-the-chat-screen.md): each address that a tool line of the agent names is
// asked of the server's own handler, and the view draws the entity that answers as a card. The request is the page's
// own in what it carries (its context, its host, its source), so what the page may read, the card may read.

const (
	// cardsPerView bounds the work of one page: the newest addresses are resolved first. The column of every page
	// resolves fewer than the conversation view, which is the whole chat.
	cardsPerView, cardsPerColumn = 36, 6
)

// cardRefs are the addresses to resolve, from the addresses of each turn, newest turn first, each once, at most limit.
func cardRefs(perTurn [][]string, limit int) []string {
	var refs []string
	for _, turnRefs := range slices.Backward(perTurn) {
		for _, ref := range turnRefs[:min(len(turnRefs), view.CardsPerTurn)] {
			if len(refs) < limit && !slices.Contains(refs, ref) {
				refs = append(refs, ref)
			}
		}
	}
	return refs
}

// chatExtras reads what the chat draws beside the turns. The conversation view is the whole chat: it gets the cards of
// its newest turns and the days of its day bar. The column of any other page gets the cards of the newest turns of
// today, fewer of them. A failed read of the days draws a bar of Today alone: the bar is a help, as the strip is.
func (h *server) chatExtras(r *http.Request, e *siren.Entity, chat *view.Chat, today string) (map[string]*siren.Entity, []string) {
	if slices.Contains(e.Class, "conversation") {
		props, _ := e.Properties.(map[string]any)
		turns, _ := props["turns"].([]Turn)
		perTurn := make([][]string, len(turns))
		for i, t := range turns {
			perTurn[i] = t.Refs
		}
		days, err := conversationDays(r.Context(), h, today)
		if err != nil {
			logFault(r, "the days of the chat cannot be read", err)
		}
		return h.cards(r, cardRefs(perTurn, cardsPerView)), days
	}
	if chat == nil {
		return nil, nil
	}
	perTurn := make([][]string, len(chat.Turns))
	for i, t := range chat.Turns {
		perTurn[i] = t.Refs
	}
	return h.cards(r, cardRefs(perTurn, cardsPerColumn)), nil
}

// cards asks the handler for each address, once, and keeps the entity that answers. An address that is not a path of
// this server, that answers a status other than 2xx or that answers no Siren entity keeps nil: the view draws it as a
// plain link. The page's context carries its deadline.
func (h *server) cards(r *http.Request, refs []string) map[string]*siren.Entity {
	out := make(map[string]*siren.Entity, len(refs))
	for _, ref := range refs {
		out[ref] = h.card(r, ref)
	}
	return out
}

func (h *server) card(r *http.Request, ref string) *siren.Entity { return h.cardAt(r, ref, 1) }

// cardAt asks for one address; a redirect to a path of this server (/days/today to the day, a title to its page) is
// followed hops times at most.
func (h *server) cardAt(r *http.Request, ref string, hops int) *siren.Entity {
	path, _, _ := strings.Cut(ref, "#") // a fragment stays with the browser: it is for the view
	u, err := url.ParseRequestURI(path)
	if err != nil || !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") || u.Host != "" {
		return nil
	}
	req := httptest.NewRequestWithContext(r.Context(), http.MethodGet, u.RequestURI(), nil)
	req.Host, req.RemoteAddr = r.Host, r.RemoteAddr
	req.Header.Set("Accept", "application/vnd.siren+json")
	req.Header.Set(siren.SourceHeader, source(r))
	rec := httptest.NewRecorder()
	h.mux.ServeHTTP(rec, req)
	if rec.Code/100 == 3 && hops > 0 {
		if to := rec.Header().Get("Location"); strings.HasPrefix(to, "/") {
			return h.cardAt(r, to, hops-1)
		}
		return nil
	}
	if rec.Code/100 != 2 || !strings.HasPrefix(rec.Header().Get("Content-Type"), "application/vnd.siren+json") {
		return nil
	}
	e := &siren.Entity{}
	if err := json.Unmarshal(rec.Body.Bytes(), e); err != nil || e.Title == "" && len(e.Class) == 0 {
		return nil
	}
	return e
}

// conversationDays are the days before today that have a live conversation page, the newest three, oldest first. It
// looks back over the last month: a day further back is a link from the day view.
func conversationDays(ctx context.Context, h *server, today string) ([]string, error) {
	t, err := time.Parse(time.DateOnly, today)
	if err != nil {
		return nil, err
	}
	var days []string
	for i := 1; i <= 31 && len(days) < 3; i++ {
		day := t.AddDate(0, 0, -i).Format(time.DateOnly)
		id, err := h.s.PageID(ctx, ConversationTitle(day))
		if err != nil {
			return nil, err
		}
		if id == 0 {
			continue
		}
		p, err := h.s.PageByID(ctx, id)
		if err != nil {
			return nil, err
		}
		if p.DeletedAt == "" {
			days = append([]string{day}, days...)
		}
	}
	return days, nil
}
