package api

import (
	"context"

	"lifelog/internal/api/view"
	"lifelog/internal/core"
)

// todayStrip is what the Today strip of the rail shows (docs/plans/137-pixel-wood-and-the-focus-pass.md, part 4):
// whether today has words and a mood, the habits in use with what today says of each, and the streak. The rail
// reads it once a page, and so does the head of a day for its streak.
func todayStrip(ctx context.Context, s *core.Store, today string) (*view.Strip, error) {
	streak, written, err := s.Streak(ctx, today)
	if err != nil {
		return nil, err
	}
	moods, err := s.Moods(ctx, today, today)
	if err != nil {
		return nil, err
	}
	habits, err := s.Habits(ctx, today)
	if err != nil {
		return nil, err
	}
	strip := &view.Strip{Written: written, Mood: len(moods[today]) > 0, Streak: streak}
	for _, h := range habits {
		strip.Habits = append(strip.Habits, view.Habit{Name: h.Name, Done: h.Done})
	}
	return strip, nil
}
