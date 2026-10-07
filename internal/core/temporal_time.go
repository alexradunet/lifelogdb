package core

import (
	"strconv"
	"strings"
	"time"
)

// periodRange describes possible endpoint days, not observed dates.
type periodRange struct {
	min, max            string
	comparable, ongoing bool
}

func periodBoundary(value *string, end bool) (periodRange, error) {
	if value == nil {
		return periodRange{}, nil
	}
	v := *value
	if v == ".." {
		if end {
			return periodRange{ongoing: true}, nil
		}
		return periodRange{}, invalid("ongoing marker is only an end boundary")
	}
	qualified := false
	if len(v) > 0 && strings.ContainsRune("?~%", rune(v[len(v)-1])) {
		qualified = true
		v = v[:len(v)-1]
	}
	if len(v) != 4 && len(v) != 7 && len(v) != 10 {
		return periodRange{}, invalid("invalid period boundary")
	}
	for i := range len(v) {
		if (i == 4 || i == 7) && v[i] == '-' {
			continue
		}
		if v[i] < '0' || v[i] > '9' {
			return periodRange{}, invalid("invalid period boundary")
		}
	}
	year, err := strconv.Atoi(v[:4])
	if err != nil {
		return periodRange{}, invalid("invalid period year")
	}
	month := 1
	if len(v) >= 7 {
		month, err = strconv.Atoi(v[5:7])
		if err != nil || month < 1 || month > 12 {
			return periodRange{}, invalid("invalid period month")
		}
	}
	lo, hi := v, v
	switch len(v) {
	case 4:
		lo = v + "-01-01"
		hi = v + "-12-31"
	case 7:
		lo = v + "-01"
		hi = time.Date(year, time.Month(month)+1, 0, 0, 0, 0, 0, time.UTC).Format(time.DateOnly)
	}
	if !IsDay(lo) || !IsDay(hi) {
		return periodRange{}, invalid("invalid period calendar boundary")
	}
	if qualified {
		return periodRange{}, nil
	}
	return periodRange{min: lo, max: hi, comparable: true}, nil
}

func periodSpan(start, end *string) (periodRange, periodRange, error) {
	s, err := periodBoundary(start, false)
	if err != nil {
		return periodRange{}, periodRange{}, err
	}
	e, err := periodBoundary(end, true)
	if err != nil {
		return periodRange{}, periodRange{}, err
	}
	if s.comparable && e.comparable && s.min > e.max {
		return periodRange{}, periodRange{}, invalid("period boundaries are definitely reversed")
	}
	return s, e, nil
}

func periodMembership(start, end *string, day, asOf string) (string, error) {
	if !IsDay(day) {
		return "", invalid("membership day must be an exact day")
	}
	if asOf != "" && !IsDay(asOf) {
		return "", invalid("as_of must be an exact day")
	}
	s, e, err := periodSpan(start, end)
	if err != nil {
		return "", err
	}
	if e.ongoing && !IsDay(asOf) {
		return "", invalid("ongoing membership requires an explicit exact as_of day")
	}
	if s.comparable && day < s.min {
		return "outside", nil
	}
	if e.ongoing {
		if day > asOf {
			return "incomparable", nil
		}
		// A horizon is not an observed end and cannot eliminate possible starts.
		if !s.comparable {
			return "incomparable", nil
		}
		if day >= s.max {
			return "definite", nil
		}
		return "possible", nil
	}
	if e.comparable && day > e.max {
		return "outside", nil
	}
	if !s.comparable || !e.comparable {
		return "incomparable", nil
	}
	// Condition on start<=end, rather than counting impossible endpoint pairs.
	if min(s.max, e.max) <= day && day <= max(e.min, s.min) {
		return "definite", nil
	}
	return "possible", nil
}

func validateSessionEndpoint(at, local, offset, zone string, required bool) error {
	if at != "" && local != "" {
		return invalid("session endpoint cannot be both UTC and local")
	}
	if at == "" && local == "" {
		if required {
			return invalid("session start is required")
		}
		if offset != "" || zone != "" {
			return invalid("session evidence requires an endpoint")
		}
		return nil
	}
	if at != "" && !IsInstant(at) {
		return invalid("session UTC endpoint must be a canonical instant")
	}
	if local != "" && (len(local) != 23 || !IsInstant(local+"Z")) {
		return invalid("session local endpoint must be a canonical unresolved clock")
	}
	if offset != "" {
		if at == "" {
			return invalid("known offset requires a UTC endpoint")
		}
		if len(offset) != 6 || (offset[0] != '+' && offset[0] != '-') || offset[3] != ':' || offset == "-00:00" {
			return invalid("invalid known offset spelling")
		}
		for _, i := range []int{1, 2, 4, 5} {
			if offset[i] < '0' || offset[i] > '9' {
				return invalid("invalid known offset spelling")
			}
		}
		if offset[1:3] > "23" || offset[4:6] > "59" {
			return invalid("invalid known offset range")
		}
	}
	if zone != "" {
		if len(zone) > 64 {
			return invalid("unverified zone label is too long")
		}
		for _, r := range zone {
			if !(r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || strings.ContainsRune("_/+-", r)) {
				return invalid("invalid unverified zone label")
			}
		}
	}
	return nil
}

// sessionElapsedMillis is exact across years0000–9999; time.Duration cannot
// represent that span. Local clocks and mixed endpoints never establish elapsed.
func sessionElapsedMillis(startAt, endAt string) (*int64, error) {
	if startAt == "" || endAt == "" {
		return nil, nil
	}
	if !IsInstant(startAt) || !IsInstant(endAt) {
		return nil, invalid("elapsed endpoints must be canonical UTC instants")
	}
	if endAt < startAt {
		return nil, invalid("session UTC endpoints are reversed")
	}
	start, err := time.Parse("2006-01-02T15:04:05.000Z", startAt)
	if err != nil {
		return nil, err
	}
	end, err := time.Parse("2006-01-02T15:04:05.000Z", endAt)
	if err != nil {
		return nil, err
	}
	result := end.UnixMilli() - start.UnixMilli()
	return &result, nil
}
