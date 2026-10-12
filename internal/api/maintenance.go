package api

// The database as a whole: a read-only query, the integrity checks and the owner's snapshot.

import (
	"fmt"
	"net/http"
	"time"

	"lifelog/internal/core"
)

func (h *server) query(r *http.Request, _ string) (*Entity, error) {
	v, err := form(r)
	if err != nil {
		return nil, err
	}
	if err := required(v, "sql"); err != nil {
		return nil, err
	}
	res, err := h.s.Query(r.Context(), v.Get("sql"), 500)
	if err != nil {
		return nil, err
	}
	return &Entity{Class: []string{"result"}, Title: "Query", Properties: res,
		Links:   []Link{link("index", "/", "Home")},
		Actions: []Action{action("query", nil, map[string]any{"sql": v.Get("sql")})}}, nil
}

// snapshot is the owner's action (docs/plans/082-snapshot-in-the-catalog.md): the file lands on this machine, in
// the store's folder; the answer is the path and the restore check, with self at the root so a browser form is
// answered home with them as feedback.
func (h *server) snapshot(r *http.Request, _ string) (*Entity, error) {
	path, res, err := h.s.TakeSnapshot(r.Context(), time.Now())
	if err != nil {
		return nil, &core.Error{Status: 422, Msg: err.Error(), Code: "snapshot_refused"}
	}
	if !res.OK { // the file is kept, as the check does not delete: the owner reads it before trusting it
		return nil, &core.Error{Status: http.StatusInternalServerError, Code: "restore_check_failed",
			Msg: fmt.Sprintf("%s failed its restore check: it is kept, but it is not one to restore", path)}
	}
	props := map[string]any{"snapshot": path, "restore_check": res}
	return &Entity{Class: []string{"snapshot"}, Title: "Snapshot", Properties: props, Result: props,
		Links: []Link{link("self", "/", "Home"), link("index", "/", "Home")}}, nil
}

func (h *server) integrity(r *http.Request) (*Entity, error) {
	res, err := h.s.Integrity(r.Context())
	if err != nil {
		return nil, err
	}
	return &Entity{Class: []string{"integrity"}, Title: "Integrity checks", Properties: res,
		Links: []Link{link("self", "/integrity", "Integrity"), link("index", "/", "Home")}}, nil
}
