package api

import (
	"net/http"

	"lifelog/internal/core"
)

// relocateReading is relocate-reading, the owner's correction of a reading's scope (docs/contract/planning.md is
// not involved; see core.RelocateReading): the answer is the replacement reading with the result on it.
func (h *server) relocateReading(r *http.Request, src string) (*Entity, error) {
	id, v, err := idForm(r, "metric", "day", "value")
	if err != nil {
		return nil, err
	}
	value, err := number(v, "value")
	if err != nil {
		return nil, err
	}
	session, err := optionalID(v, "session_id")
	if err != nil {
		return nil, err
	}
	captured, err := optionalID(v, "captured_with_id")
	if err != nil {
		return nil, err
	}
	result, err := h.s.RelocateReading(r.Context(), src, id, core.Reading{Metric: v.Get("metric"), Day: v.Get("day"), Value: *value, SessionID: session, CapturedWith: captured, TakenAt: v.Get("taken_at"), TZ: v.Get("tz"), ImportKey: v.Get("import_key")})
	if err != nil {
		return nil, err
	}
	e, err := h.measurementEntity(r.Context(), result.ReplacementID)
	if err != nil {
		return nil, err
	}
	e.Result = result
	return e, nil
}
