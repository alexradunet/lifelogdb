package api

import (
	"context"

	"lifelog/internal/api/view"
	"lifelog/internal/core"
)

// todayChat is what the chat column of the shell shows (docs/plans/146-settings-and-the-chat-column.md): the turns of
// today's conversation page, its version, and the action say with the day today. The shell reads it once a page, as it
// reads the Today strip. A day with no page, and a page that is tombstoned, have no turns and no version.
func todayChat(ctx context.Context, s *core.Store, today string) (*view.Chat, error) {
	chat := &view.Chat{Say: action("say", map[string]string{"day": "today"}, nil)}
	id, err := s.PageID(ctx, ConversationTitle(today))
	if err != nil || id == 0 {
		return chat, err
	}
	p, err := s.PageByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if p.DeletedAt != "" {
		return chat, nil
	}
	chat.Version = p.Version
	for _, t := range ParseTurns(today, p.Body) {
		chat.Turns = append(chat.Turns, view.Turn{Who: t.Who, At: t.At, Via: t.Via, Text: t.Text, Tools: t.Tools, Refs: t.Refs})
	}
	return chat, nil
}
