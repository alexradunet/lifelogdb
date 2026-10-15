package core

import (
	"context"
	"errors"
	"testing"
	"time"
)

// A search that runs out of time says so: the cause stays in the error, so the api answers 504, not 422
// (docs/plans/098-the-codebase-made-cohesive.md).
func TestAnExpiredSearchKeepsItsCause(t *testing.T) {
	s := fresh(t)
	expired, cancel := context.WithDeadline(ctx, time.Now().Add(-time.Second))
	defer cancel()
	_, err := s.Search(expired, "one", 10, 0)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("the cause is lost: %v", err)
	}
}
