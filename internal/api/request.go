package api

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"

	"lifelog/internal/core"
)

// The parsers of a request: its fields, its ids, its numbers and flags, and the page of a list. Every handler reads
// its input through them, so one refusal has one message everywhere.

const maxRequestBody = 20 << 20

func requestBodyError(err error) error {
	var limit *http.MaxBytesError
	if errors.As(err, &limit) {
		return requestLimit("request body is larger than 20 MiB")
	}
	return &core.Error{Status: http.StatusBadRequest, Msg: "body: " + err.Error()}
}

func requestLimit(msg string) error {
	return &core.Error{Status: http.StatusRequestEntityTooLarge, Msg: msg}
}

// form reads an action's fields: urlencoded (what a browser and the CLI send) or a JSON object.
func form(r *http.Request) (url.Values, error) {
	defer r.Body.Close()
	r.Body = http.MaxBytesReader(nil, r.Body, maxRequestBody)
	ct, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if ct == "application/json" {
		var m map[string]any
		dec := json.NewDecoder(r.Body)
		if err := dec.Decode(&m); err != nil {
			return nil, requestBodyError(err)
		}
		if m == nil {
			return nil, requestBodyError(errors.New("expected a JSON object"))
		}
		var extra any
		if err := dec.Decode(&extra); err != io.EOF {
			if err == nil {
				err = errors.New("expected one JSON object")
			}
			return nil, requestBodyError(err)
		}
		v := url.Values{}
		for k, x := range m {
			switch x := x.(type) {
			case string:
				v.Set(k, x)
			case nil:
			default:
				b, _ := json.Marshal(x)
				v.Set(k, string(b))
			}
		}
		return v, nil
	}
	if err := r.ParseForm(); err != nil {
		return nil, requestBodyError(err)
	}
	return r.Form, nil
}

// required refuses a request that leaves one of the named fields empty.
func required(v url.Values, names ...string) error {
	for _, n := range names {
		if v.Get(n) == "" {
			return core.Invalid("missing field %s", n)
		}
	}
	return nil
}

// idOf is the {id} of the path; a value that is not an id is a 404, like an id no row has.
func idOf(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		return 0, core.NotFound("no such id %s", r.PathValue("id"))
	}
	return id, nil
}

// idForm is the opening of a write on a resource: the {id} of the path, the fields of the body, and the ones the
// action cannot do without.
func idForm(r *http.Request, names ...string) (int64, url.Values, error) {
	id, err := idOf(r)
	if err != nil {
		return 0, nil, err
	}
	v, err := form(r)
	if err != nil {
		return 0, nil, err
	}
	return id, v, required(v, names...)
}

// idParam is the path parameter of an action on the resource with this id.
func idParam(id int64) map[string]string { return map[string]string{"id": strconv.FormatInt(id, 10)} }

// number is a field's value as a number, nil when the field is empty.
func number(v url.Values, name string) (*float64, error) {
	s := v.Get(name)
	if s == "" {
		return nil, nil
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return nil, core.Invalid("%s %q is not a number", name, s)
	}
	return &f, nil
}

// flag is a 0/1 field: false when it is empty.
func flag(v url.Values, name string) (bool, error) { return flagValue(name, v.Get(name)) }

func flagValue(name, s string) (bool, error) {
	switch s {
	case "", "0":
		return false, nil
	case "1":
		return true, nil
	}
	return false, core.Invalid("%s must be 0 or 1", name)
}

// The lists page (docs/plans/079-client-contract-hygiene.md): limit and offset select a page, and the entity
// links the next one when a further row exists, the previous one when offset > 0.
const maxPageLimit = 500

// pageOf reads limit (1–maxPageLimit, def when absent) and offset (≥ 0) from the query.
func pageOf(r *http.Request, def int) (limit, offset int, err error) {
	q := r.URL.Query()
	limit = def
	if s := q.Get("limit"); s != "" {
		if limit, err = strconv.Atoi(s); err != nil || limit < 1 || limit > maxPageLimit {
			return 0, 0, core.Invalid("limit must be 1 to %d", maxPageLimit)
		}
	}
	if s := q.Get("offset"); s != "" {
		if offset, err = strconv.Atoi(s); err != nil || offset < 0 {
			return 0, 0, core.Invalid("offset must be 0 or more")
		}
	}
	return limit, offset, nil
}

// paged trims a read of limit+1 rows to the page, records limit and offset in the properties and adds the next
// and prev links; path and q are the list's own address.
func paged[T any](e *Entity, rows []T, path string, q url.Values, limit, offset int) []T {
	more := len(rows) > limit
	if more {
		rows = rows[:limit]
	}
	props, _ := e.Properties.(map[string]any)
	if props == nil {
		props = map[string]any{}
		e.Properties = props
	}
	props["limit"], props["offset"] = limit, offset
	href := func(offset int) string {
		v := url.Values{}
		for k, vs := range q {
			v[k] = vs
		}
		v.Set("limit", strconv.Itoa(limit))
		v.Set("offset", strconv.Itoa(offset))
		return path + "?" + v.Encode()
	}
	if offset > 0 {
		e.Links = append(e.Links, link("prev", href(max(offset-limit, 0)), "Previous page"))
	}
	if more {
		e.Links = append(e.Links, link("next", href(offset+limit), "Next page"))
	}
	return rows
}
