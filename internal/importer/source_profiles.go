package importer

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"lifelog/internal/core"
)

const maxProfileBytes = 1 << 20
const maxProfileRecords = 8192

// SourceQuantity uses fixed source meaning codes, never current metric spellings or database IDs.
type SourceQuantity struct {
	Code  string `json:"code"`
	Unit  string `json:"unit"`
	Value string `json:"value"`
}
type SourceRecord struct {
	Key         string           `json:"key"`
	Day         string           `json:"day"`
	Activity    string           `json:"activity,omitempty"`
	StartAt     string           `json:"start_at,omitempty"`
	StartLocal  string           `json:"start_local,omitempty"`
	StartOffset string           `json:"start_offset,omitempty"`
	EndAt       string           `json:"end_at,omitempty"`
	EndLocal    string           `json:"end_local,omitempty"`
	EndOffset   string           `json:"end_offset,omitempty"`
	Quantities  []SourceQuantity `json:"quantities"`
}
type SourceProfile struct {
	Records  []SourceRecord `json:"records"`
	Excluded []string       `json:"excluded,omitempty"`
}

// FitSessionBinding is an explicit owner-assigned identity/day/activity binding, not a provider ID inference.
type FitSessionBinding struct {
	Key      string `json:"key"`
	Day      string `json:"day"`
	Activity string `json:"activity"`
}

var sourceDigits = regexp.MustCompile(`^[0-9]{1,128}$`)
var sourceDecimal = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+)?$`)
var sourceClock = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(?:\.[0-9]{1,3})?(?:Z|[+-][0-9]{2}:[0-9]{2})?$`)

// decodeSourceProfile interprets only the approved fixed profiles. Errors reveal no source values or paths.
func decodeSourceProfile(profile string, data []byte, binding *FitSessionBinding) (*SourceProfile, error) {
	if !utf8.Valid(data) || binding != nil && (!utf8.ValidString(binding.Key) || !utf8.ValidString(binding.Activity)) {
		return nil, refuse("malformed source Unicode")
	}
	if len(data) > maxProfileBytes {
		return nil, refuse("selected source exceeds profile byte limit")
	}
	var result *SourceProfile
	var err error
	switch profile {
	case "fit-date-csv-v1":
		result, err = decodeFitDaily(data)
	case "legacy-sleep-array-v1":
		result, err = decodeLegacySleep(data)
	case "fit-session-object-v1":
		result, err = decodeFitSession(data, binding)
	default:
		return nil, refuse("unsupported selected source profile")
	}
	if err != nil {
		return nil, refuse("selected source does not satisfy the bounded profile")
	}
	return result, nil
}

func sourceNumber(text string, integer bool) error {
	if !sourceDecimal.MatchString(text) || integer && !sourceDigits.MatchString(text) {
		return errors.New("invalid numeric quantity")
	}
	n, err := strconv.ParseFloat(text, 64)
	if err != nil || math.IsInf(n, 0) || math.IsNaN(n) {
		return errors.New("unrepresentable quantity")
	}
	if n == 0 && strings.ContainsAny(text, "123456789") {
		return errors.New("nonzero quantity underflow")
	}
	if integer {
		original, ok := new(big.Int).SetString(text, 10)
		if !ok {
			return errors.New("invalid integer")
		}
		converted, _ := new(big.Float).SetFloat64(n).Int(nil)
		if original.Cmp(converted) != 0 {
			return errors.New("integer precision loss")
		}
	}
	return nil
}

func normalizeSourceClock(raw string) (at, local, offset string, err error) {
	if !sourceClock.MatchString(raw) {
		return "", "", "", errors.New("unsupported clock")
	}
	clock := raw
	switch {
	case strings.HasSuffix(clock, "Z"):
		offset = "+00:00"
		clock = strings.TrimSuffix(clock, "Z")
	case len(clock) >= 6 && (clock[len(clock)-6] == '+' || clock[len(clock)-6] == '-'):
		offset = clock[len(clock)-6:]
		clock = clock[:len(clock)-6]
		if offset == "-00:00" || offset[1:3] > "23" || offset[4:] > "59" {
			return "", "", "", errors.New("invalid offset")
		}
	}
	if !strings.Contains(clock, ".") {
		clock += ".000"
	} else {
		clock += strings.Repeat("0", 23-len(clock))
	}
	parsed, e := time.Parse("2006-01-02T15:04:05.000", clock)
	if e != nil || parsed.Format("2006-01-02T15:04:05.000") != clock {
		return "", "", "", errors.New("invalid calendar clock")
	}
	if offset == "" {
		return "", clock, "", nil
	}
	hours, _ := strconv.Atoi(offset[1:3])
	minutes, _ := strconv.Atoi(offset[4:])
	delta := time.Duration(hours*60+minutes) * time.Minute
	if offset[0] == '-' {
		delta = -delta
	}
	utc := parsed.Add(-delta).UTC().Format("2006-01-02T15:04:05.000Z")
	if !core.IsInstant(utc) {
		return "", "", "", errors.New("offset leaves admitted calendar")
	}
	return utc, "", offset, nil
}

func decodeFitDaily(data []byte) (*SourceProfile, error) {
	reader := csv.NewReader(bytes.NewReader(data))
	header, err := reader.Read()
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	dateIndex := -1
	selected := map[string]struct{ code, unit string }{"Step count": {"steps", "steps"}, "Distance (m)": {"distance", "m"}, "Calories (kcal)": {"reported-calories", "kcal"}, "Average heart rate (bpm)": {"mean-heart-rate", "bpm"}}
	out := &SourceProfile{Records: []SourceRecord{}}
	excluded := false
	for i, h := range header {
		if seen[h] || h == "" {
			return nil, errors.New("duplicate header")
		}
		seen[h] = true
		if h == "Date" {
			dateIndex = i
		}
		if h == "Start time" || h == "End time" {
			return nil, errors.New("not daily profile")
		}
		if _, ok := selected[h]; !ok && h != "Date" {
			excluded = true
		}
	}
	if dateIndex < 0 {
		return nil, errors.New("missing reporting day")
	}
	days := map[string]bool{}
	for {
		row, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if len(out.Records) >= maxProfileRecords {
			return nil, errors.New("too many records")
		}
		day := row[dateIndex]
		if !core.IsDay(day) || days[day] {
			return nil, errors.New("ambiguous daily row")
		}
		days[day] = true
		record := SourceRecord{Key: "daily:" + day, Day: day, Quantities: []SourceQuantity{}}
		for i, h := range header {
			meaning, ok := selected[h]
			if !ok || row[i] == "" {
				continue
			}
			if err := sourceNumber(row[i], h == "Step count"); err != nil {
				return nil, err
			}
			record.Quantities = append(record.Quantities, SourceQuantity{meaning.code, meaning.unit, row[i]})
		}
		out.Records = append(out.Records, record)
	}
	if excluded {
		out.Excluded = []string{"unselected CSV columns"}
	}
	sort.Strings(out.Excluded)
	return out, nil
}

// validateSourceJSON rejects duplicate keys and bounds depth/tokens before decoding into fixed records.
func validateSourceJSON(data []byte) error {
	if !utf8.Valid(data) || !losslessJSONStrings(data) {
		return errors.New("malformed JSON Unicode")
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	tokens := 0
	var value func(int) error
	value = func(depth int) error {
		if depth > 32 {
			return errors.New("too deep")
		}
		tokens++
		if tokens > 200000 {
			return errors.New("too many tokens")
		}
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		delim, ok := tok.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			seen := map[string]bool{}
			for dec.More() {
				key, err := dec.Token()
				if err != nil {
					return err
				}
				s, ok := key.(string)
				if !ok || seen[s] {
					return errors.New("duplicate key")
				}
				seen[s] = true
				if err := value(depth + 1); err != nil {
					return err
				}
			}
		case '[':
			for dec.More() {
				if err := value(depth + 1); err != nil {
					return err
				}
			}
		default:
			return errors.New("bad delimiter")
		}
		_, err = dec.Token()
		return err
	}
	if err := value(0); err != nil {
		return err
	}
	if _, err := dec.Token(); err != io.EOF {
		return errors.New("trailing input")
	}
	return nil
}
func decodeObject(data []byte) (map[string]json.RawMessage, error) {
	var v map[string]json.RawMessage
	err := json.Unmarshal(data, &v)
	if err != nil || v == nil {
		return nil, errors.New("not object")
	}
	return v, nil
}
func sourceString(v map[string]json.RawMessage, key string, required bool) (string, error) {
	b, ok := v[key]
	if !ok || bytes.Equal(b, []byte("null")) {
		if required {
			return "", errors.New("missing field")
		}
		return "", nil
	}
	var text string
	if err := json.Unmarshal(b, &text); err != nil {
		return "", err
	}
	return text, nil
}
func setRecordClocks(record *SourceRecord, v map[string]json.RawMessage) error {
	start, err := sourceString(v, "startTime", true)
	if err != nil {
		return err
	}
	record.StartAt, record.StartLocal, record.StartOffset, err = normalizeSourceClock(start)
	if err != nil {
		return err
	}
	end, err := sourceString(v, "endTime", false)
	if err != nil {
		return err
	}
	if end != "" {
		record.EndAt, record.EndLocal, record.EndOffset, err = normalizeSourceClock(end)
		if err != nil {
			return err
		}
	}
	if record.StartAt != "" && record.EndAt != "" && record.EndAt < record.StartAt {
		return errors.New("reversed UTC")
	}
	return nil
}
func decodeLegacySleep(data []byte) (*SourceProfile, error) {
	if err := validateSourceJSON(data); err != nil {
		return nil, err
	}
	var rows []json.RawMessage
	if err := json.Unmarshal(data, &rows); err != nil || rows == nil {
		return nil, errors.New("not root array")
	}
	if len(rows) > maxProfileRecords {
		return nil, errors.New("too many records")
	}
	out := &SourceProfile{Records: []SourceRecord{}}
	keys := map[string]bool{}
	excluded := false
	for _, row := range rows {
		v, err := decodeObject(row)
		if err != nil {
			return nil, err
		}
		idRaw, ok := v["logId"]
		if !ok {
			return nil, errors.New("missing identity")
		}
		id := string(idRaw)
		if len(id) > 0 && id[0] == '"' {
			if err := json.Unmarshal(idRaw, &id); err != nil {
				return nil, err
			}
		}
		if !sourceDigits.MatchString(id) || keys[id] {
			return nil, errors.New("invalid/duplicate identity")
		}
		keys[id] = true
		day, err := sourceString(v, "dateOfSleep", true)
		if err != nil || !core.IsDay(day) {
			return nil, errors.New("invalid day")
		}
		record := SourceRecord{Key: "sleep:" + id, Day: day, Activity: "sleep", Quantities: []SourceQuantity{}}
		if err := setRecordClocks(&record, v); err != nil {
			return nil, err
		}
		for _, field := range []string{"minutesAsleep", "timeInBed"} {
			raw, ok := v[field]
			if !ok || bytes.Equal(raw, []byte("null")) {
				continue
			}
			text := string(raw)
			if err := sourceNumber(text, true); err != nil {
				return nil, err
			}
			code := "asleep-minutes"
			if field == "timeInBed" {
				code = "in-bed-minutes"
			}
			record.Quantities = append(record.Quantities, SourceQuantity{code, "min", text})
		}
		for key := range v {
			switch key {
			case "logId", "dateOfSleep", "startTime", "endTime", "minutesAsleep", "timeInBed":
			case "duration", "efficiency", "levels", "logType", "mainSleep", "minutesAfterWakeup", "minutesAwake", "minutesToFallAsleep", "type":
				excluded = true
			default:
				return nil, errors.New("unsupported sleep shape")
			}
		}
		out.Records = append(out.Records, record)
	}
	if excluded {
		out.Excluded = []string{"sleep stages and unselected summaries"}
	}
	sort.Strings(out.Excluded)
	return out, nil
}
func decodeFitSession(data []byte, binding *FitSessionBinding) (*SourceProfile, error) {
	if err := validateSourceJSON(data); err != nil {
		return nil, err
	}
	v, err := decodeObject(data)
	if err != nil {
		return nil, err
	}
	if binding == nil || binding.Key == "" || len(binding.Key) > 256 || strings.ContainsRune(binding.Key, 0) || !core.IsDay(binding.Day) {
		return nil, errors.New("reviewed binding required")
	}
	activity, err := sourceString(v, "fitnessActivity", true)
	if err != nil || activity == "" || activity != binding.Activity {
		return nil, errors.New("activity binding mismatch")
	}
	record := SourceRecord{Key: "fit-bound:" + binding.Key, Day: binding.Day, Activity: activity, Quantities: []SourceQuantity{}}
	if err := setRecordClocks(&record, v); err != nil {
		return nil, err
	}
	out := &SourceProfile{Records: []SourceRecord{record}}
	for key := range v {
		switch key {
		case "startTime", "endTime", "fitnessActivity":
		case "aggregate", "segment", "duration":
			out.Excluded = append(out.Excluded, fmt.Sprintf("uninterpreted Fit %s", key))
		default:
			return nil, errors.New("unsupported session shape")
		}
	}
	sort.Strings(out.Excluded)
	return out, nil
}

// encoding/json replaces lone surrogates; source evidence must remain lossless.
func losslessJSONStrings(data []byte) bool {
	in := false
	for i := 0; i < len(data); i++ {
		if data[i] == 34 {
			in = !in
			continue
		}
		if !in || data[i] != 92 {
			continue
		}
		i++
		if i >= len(data) {
			return false
		}
		if data[i] != 117 {
			continue
		}
		if i+4 >= len(data) {
			return false
		}
		n, err := strconv.ParseUint(string(data[i+1:i+5]), 16, 16)
		if err != nil {
			return false
		}
		i += 4
		if n >= 0xdc00 && n <= 0xdfff {
			return false
		}
		if n >= 0xd800 && n <= 0xdbff {
			if i+6 >= len(data) || data[i+1] != 92 || data[i+2] != 117 {
				return false
			}
			low, err := strconv.ParseUint(string(data[i+3:i+7]), 16, 16)
			if err != nil || low < 0xdc00 || low > 0xdfff {
				return false
			}
			i += 6
		}
	}
	return true
}
