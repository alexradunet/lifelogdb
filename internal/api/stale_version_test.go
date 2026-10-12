package api_test

import (
	"errors"
	"testing"

	"lifelog/internal/client"
)

// code is the error code a refused write answered with, "" for a success.
func code(err error) string {
	var ce *client.Error
	if !errors.As(err, &ce) {
		return ""
	}
	props, _ := ce.Entity.Properties.(map[string]any)
	c, _ := props["code"].(string)
	return c
}

// A write with an old version answers stale_version on every resource that carries one (API.md: "a save with an old
// version"), and a 409 that is not a version's keeps the general code conflict
// (docs/plans/098-the-codebase-made-cohesive.md).
func TestStaleVersionIsTheSameCodeOnEveryResource(t *testing.T) {
	c, _ := fresh(t)
	root := must(c.Get(t.Context(), "/"))
	kind := must(c.Do(t.Context(), find(root, "create-page"), map[string]string{"title": "Sleep"}))
	_ = kind
	session := must(c.Do(t.Context(), find(root, "capture-session"), map[string]string{"kind": "Sleep", "day": "2026-10-01", "start_at": "2026-10-01T22:00:00.000Z"}))
	edit := find(session, "edit-session")
	must(c.Do(t.Context(), edit, map[string]string{"day": "2026-10-02"}))
	if _, err := c.Do(t.Context(), edit, map[string]string{"day": "2026-10-03"}); code(err) != "stale_version" {
		t.Errorf("a session edited with an old version: code %q, want stale_version (%v)", code(err), err)
	}

	task := must(c.Do(t.Context(), find(root, "create-task"), map[string]string{"label": "Water the plants"}))
	stop := find(task, "edit-task")
	must(c.Do(t.Context(), stop, map[string]string{"label": "Water the plants well"}))
	if _, err := c.Do(t.Context(), stop, map[string]string{"label": "Water the plants again"}); code(err) != "stale_version" {
		t.Errorf("a task edited with an old version: code %q, want stale_version (%v)", code(err), err)
	}

	period := must(c.Do(t.Context(), find(root, "create-period"), map[string]string{"title": "School years"}))
	editPeriod := find(period, "edit-period")
	must(c.Do(t.Context(), editPeriod, map[string]string{"start_boundary": "2001"}))
	if _, err := c.Do(t.Context(), editPeriod, map[string]string{"start_boundary": "2002"}); code(err) != "stale_version" {
		t.Errorf("a period edited with an old version: code %q, want stale_version (%v)", code(err), err)
	}

	// Not a version's 409: a sender's key reused with another payload.
	must(c.Do(t.Context(), find(root, "create-task"), map[string]string{"label": "Pay the rent", "import_key": "rent"}))
	if _, err := c.Do(t.Context(), find(root, "create-task"), map[string]string{"label": "Pay the rent twice", "import_key": "rent"}); code(err) != "conflict" {
		t.Errorf("a key reused with another payload: code %q, want conflict (%v)", code(err), err)
	}
}
