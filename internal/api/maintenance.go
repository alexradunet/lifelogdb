package api

// The database as a whole: the integrity checks, the owner's snapshot and the owner's repairs of derived data.

import (
	"fmt"
	"net/http"

	"lifelog/internal/api/siren"
	"lifelog/internal/core"
)

// snapshot is the owner's action (docs/plans/082-snapshot-in-the-catalog.md): the file lands on this machine, in
// the store's folder; the answer is the path and the restore check, with self at the root so a browser form is
// answered home with them as feedback. Core reports a refusal as 422 snapshot_refused; a timeout and a failure are
// answered as on every route.
func (h *server) snapshot(r *http.Request, _ spec, _ string) (*siren.Entity, error) {
	path, res, err := h.s.TakeSnapshot(r.Context(), h.s.Local())
	if err != nil {
		return nil, err
	}
	if !res.OK { // the file is kept, as the check does not delete: the owner reads it before trusting it
		return nil, &core.Error{Status: http.StatusInternalServerError, Code: "restore_check_failed",
			Msg: fmt.Sprintf("%s failed its restore check: it is kept, but it is not one to restore", path)}
	}
	props := map[string]any{"snapshot": path, "restore_check": res}
	return &siren.Entity{Class: []string{"snapshot"}, Title: "Snapshot", Properties: props, Result: props,
		Links: []siren.Link{link("self", "/", "Home"), link("index", "/", "Home")}}, nil
}

func (h *server) integrity(r *http.Request, _ spec, _ string) (*siren.Entity, error) {
	res, err := h.s.Integrity(r.Context())
	if err != nil {
		return nil, err
	}
	return &siren.Entity{Class: []string{"integrity"}, Title: "Integrity checks", Properties: res,
		Links: []siren.Link{link("self", "/integrity", "Integrity"), link("index", "/", "Home")}}, nil
}

// rebuildSearch is the owner's repair of the full-text index (docs/contract/integrity-checks.md). The answer is the
// number of documents of the new index, with self at the root so that a browser form is answered home with it.
func (h *server) rebuildSearch(r *http.Request, _ spec, _ string) (*siren.Entity, error) {
	documents, err := h.s.RebuildSearch(r.Context())
	if err != nil {
		return nil, err
	}
	props := map[string]any{"documents": documents}
	return &siren.Entity{Class: []string{"rebuilt"}, Title: "Search rebuilt", Properties: props, Result: props,
		Links: []siren.Link{link("self", "/", "Home"), link("index", "/", "Home")}}, nil
}
