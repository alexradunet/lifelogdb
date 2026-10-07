package api

import (
	"net/http"
	"net/url"
	"strconv"

	"lifelog/internal/core"
)

func optionalID(v url.Values, name string) (int64, error) {
	if v.Get(name) == "" {
		return 0, nil
	}
	id, err := strconv.ParseInt(v.Get(name), 10, 64)
	if err != nil {
		return 0, &core.Error{Status: 422, Msg: name + " must be an exact integer id"}
	}
	return id, nil
}
func (h *server) relocateReading(r *http.Request, src string) (*Entity, error) {
	id, err := idOf(r)
	if err != nil {
		return nil, err
	}
	v, err := form(r)
	if err != nil {
		return nil, err
	}
	if err = required(v, "metric", "day", "value"); err != nil {
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
	result, err := h.s.RelocateReading(r.Context(), src, id, core.Reading{Metric: v.Get("metric"), Day: v.Get("day"), Value: *value, SessionID: session, CapturedWith: captured, TakenAt: v.Get("taken_at"), TZ: v.Get("tz"), Key: v.Get("import_key")})
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
