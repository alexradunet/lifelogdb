package api_test

import (
	"errors"
	"strings"
	"testing"

	"lifelog/internal/api/apitest"
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
	c, _, _ := apitest.Fresh(t, "cli")
	root := apitest.Must(t)(c.Get(t.Context(), "/"))
	page := apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "create-page"), map[string]string{"title": "Garden", "body": "the beds"}))
	save := apitest.Action(t, page, "save-body")
	apitest.Must(t)(c.Do(t.Context(), save, map[string]string{"body": "the beds, dug"}))
	if _, err := c.Do(t.Context(), save, map[string]string{"body": "the beds, dug again"}); code(err) != "stale_version" {
		t.Errorf("a page saved with an old version: code %q, want stale_version (%v)", code(err), err)
	}

	// Not a version's 409: a file kept under the title of a page that has words of its own.
	apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "create-page"), map[string]string{"title": "Scan.pdf", "body": "my own words"}))
	if _, err := c.Do(t.Context(), apitest.Action(t, root, "add-file"), map[string]string{"title": "Scan.pdf", "sha256": strings.Repeat("a", 64), "mime": "application/pdf", "body": "OCR text"}); code(err) != "conflict" {
		t.Errorf("a file over a page with words: code %q, want conflict (%v)", code(err), err)
	}
}
