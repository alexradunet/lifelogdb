// Package takeout inventories the public shapes of a Google Takeout export without printing values.
package takeout

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

var errPrivateInventory = errors.New("takeout inventory could not read one file; no filename or value is reported")

// Report is the privacy-safe Phase A summary. It names known public product families, counts records and files,
// reports month ranges, and records schema shapes without values or private filenames.
type Report struct {
	TopFolders []TopFolder `json:"top_folders"`
	Families   []Family    `json:"families"`
	Overlaps   []Overlap   `json:"overlaps,omitempty"`
}

type TopFolder struct {
	Name       string           `json:"name"`
	Files      int              `json:"files"`
	Bytes      int64            `json:"bytes"`
	Extensions []ExtensionCount `json:"extensions,omitempty"`
}

type ExtensionCount struct {
	Extension string `json:"extension"`
	Count     int    `json:"count"`
}

type Family struct {
	Name       string      `json:"name"`
	Files      int         `json:"files,omitempty"`
	Records    int         `json:"records,omitempty"`
	FirstMonth string      `json:"first_month,omitempty"`
	LastMonth  string      `json:"last_month,omitempty"`
	CSVColumns []CSVColumn `json:"csv_columns,omitempty"`
	Shapes     []Shape     `json:"shapes,omitempty"`
}

type CSVColumn struct {
	Name  string      `json:"name"`
	Types []TypeCount `json:"types,omitempty"`
	Count int         `json:"count"`
}

type Shape struct {
	Path  string      `json:"path"`
	Types []TypeCount `json:"types,omitempty"`
	Count int         `json:"count"`
}

type TypeCount struct {
	Type  string `json:"type"`
	Count int    `json:"count"`
}

type Overlap struct {
	Metric  string   `json:"metric"`
	Sources []string `json:"sources"`
	Months  []string `json:"months"`
}

type inventory struct {
	top      map[string]*topAcc
	families map[string]*familyAcc
	metrics  map[string]map[string]map[string]bool // metric -> month -> source set
}

type topAcc struct {
	name  string
	files int
	bytes int64
	exts  map[string]int
}

type familyAcc struct {
	name        string
	files       int
	records     int
	months      map[string]bool
	csv         map[string]map[string]int
	csvCounts   map[string]int
	shapes      map[string]map[string]int
	shapeCounts map[string]int
}

// Inventory reads folder read-only and returns a value-only summary for Timeline, Fit and Fitbit. It never returns
// private paths or source values in its errors; callers may print the returned report as JSON.
func Inventory(folder string) (*Report, error) {
	info, err := os.Stat(folder)
	if err != nil || !info.IsDir() {
		return nil, errors.New("takeout inventory needs an existing folder")
	}
	base := takeoutBase(folder)
	inv := &inventory{top: map[string]*topAcc{}, families: map[string]*familyAcc{}, metrics: map[string]map[string]map[string]bool{}}
	err = filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return errPrivateInventory
		}
		rel, err := filepath.Rel(base, path)
		if err != nil {
			return errPrivateInventory
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if rel != "." && publicTopFolder(rel) == "Google Photos" {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return errPrivateInventory
		}
		top := publicTopFolder(rel)
		if top == "Google Photos" || top == "" {
			return nil
		}
		inv.addTop(top, filepath.Ext(rel), info.Size())
		if err := inv.addKnownFile(path, rel, top); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, errPrivateInventory
	}
	return inv.report(), nil
}

func takeoutBase(folder string) string {
	p := filepath.Join(folder, "Takeout")
	if st, err := os.Stat(p); err == nil && st.IsDir() {
		return p
	}
	return folder
}

func publicTopFolder(rel string) string {
	first, _, _ := strings.Cut(rel, "/")
	low := strings.ToLower(first)
	switch {
	case strings.Contains(low, "location") || strings.Contains(low, "timeline"):
		return "Location History"
	case low == "fit" || strings.Contains(low, "google fit"):
		return "Fit"
	case strings.Contains(low, "fitbit"):
		return "Fitbit"
	case strings.Contains(low, "photo"):
		return "Google Photos"
	default:
		return "Other"
	}
}

func (i *inventory) addTop(name, ext string, size int64) {
	if ext == "" {
		ext = "(none)"
	}
	ext = strings.ToLower(ext)
	if !safeExtensions[ext] {
		ext = "<other>"
	}
	t := i.top[name]
	if t == nil {
		t = &topAcc{name: name, exts: map[string]int{}}
		i.top[name] = t
	}
	t.files++
	t.bytes += size
	t.exts[ext]++
}

func (i *inventory) family(name string) *familyAcc {
	f := i.families[name]
	if f == nil {
		f = &familyAcc{name: name, months: map[string]bool{}, csv: map[string]map[string]int{}, csvCounts: map[string]int{}, shapes: map[string]map[string]int{}, shapeCounts: map[string]int{}}
		i.families[name] = f
	}
	return f
}

func (i *inventory) addKnownFile(path, rel, top string) error {
	if top == "Other" {
		return nil
	}
	low := strings.ToLower(rel)
	ext := strings.ToLower(filepath.Ext(rel))
	switch top {
	case "Location History":
		switch {
		case ext == ".json" && strings.Contains(low, "semantic location history"):
			return i.addJSONFile("Timeline Semantic Visits", path, "")
		case ext == ".json" && filepath.Base(low) == "records.json":
			return i.addJSONFile("Timeline Records", path, "")
		case ext == ".json" && filepath.Base(low) == "timeline.json":
			return i.addJSONFile("Timeline On-Device", path, "")
		}
	case "Fit":
		switch {
		case strings.Contains(low, "all data") || strings.Contains(low, "data source"):
			f := i.family("Fit Raw Data Sources")
			f.files++
			return nil
		case ext == ".csv" && (strings.Contains(low, "daily") || strings.Contains(low, "activity metrics")):
			return i.addCSVFile("Fit Daily Aggregates", path, "Fit")
		case (ext == ".csv" || ext == ".json") && strings.Contains(low, "session"):
			if ext == ".csv" {
				return i.addCSVFile("Fit Sessions", path, "")
			}
			return i.addJSONFile("Fit Sessions", path, "")
		}
	case "Fitbit":
		name, metric := fitbitFamily(low)
		if name == "" {
			return nil
		}
		if ext == ".csv" {
			return i.addCSVFile(name, path, "Fitbit:"+metric)
		}
		if ext == ".json" {
			return i.addJSONFile(name, path, metric)
		}
	}
	return nil
}

func fitbitFamily(low string) (string, string) {
	switch {
	case strings.Contains(low, "sleep"):
		return "Fitbit Sleep", "sleep"
	case strings.Contains(low, "step"):
		return "Fitbit Steps", "steps"
	case strings.Contains(low, "heart_rate") || strings.Contains(low, "heart rate") || strings.Contains(low, "heartrate"):
		return "Fitbit Heart Rate", "heart-rate"
	case strings.Contains(low, "weight") || strings.Contains(low, "body-weight"):
		return "Fitbit Weight", "weight"
	case strings.Contains(low, "exercise"):
		return "Fitbit Exercise", "exercise"
	default:
		return "", ""
	}
}

func (i *inventory) addJSONFile(name, path, metric string) error {
	fh, err := os.Open(path)
	if err != nil {
		return errPrivateInventory
	}
	defer fh.Close()
	f := i.family(name)
	f.files++
	s := jsonScanner{inv: i, family: f, familyName: name, metric: metric}
	dec := json.NewDecoder(fh)
	dec.UseNumber()
	if _, err := s.scanValue(dec, "$", ""); err != nil {
		return errPrivateInventory
	}
	if tok, err := dec.Token(); err != io.EOF || tok != nil {
		return errPrivateInventory
	}
	return nil
}

type jsonScanner struct {
	inv        *inventory
	family     *familyAcc
	familyName string
	metric     string
}

type jsonSummary struct {
	keys   map[string]bool
	months map[string]bool
}

func newSummary() jsonSummary { return jsonSummary{keys: map[string]bool{}, months: map[string]bool{}} }

func (s jsonScanner) scanValue(dec *json.Decoder, path, parentKey string) (jsonSummary, error) {
	tok, err := dec.Token()
	if err != nil {
		return jsonSummary{}, err
	}
	sum := newSummary()
	switch x := tok.(type) {
	case json.Delim:
		switch x {
		case '{':
			s.recordShape(path, "object")
			for dec.More() {
				ktok, err := dec.Token()
				if err != nil {
					return jsonSummary{}, err
				}
				key, ok := ktok.(string)
				if !ok {
					return jsonSummary{}, errors.New("non-string JSON object key")
				}
				sum.keys[key] = true
				child, err := s.scanValue(dec, path+"."+publicJSONKey(key), key)
				if err != nil {
					return jsonSummary{}, err
				}
				mergeMonths(sum.months, child.months)
			}
			end, err := dec.Token()
			if err != nil || end != json.Delim('}') {
				return jsonSummary{}, errors.New("bad JSON object")
			}
			if s.isRecord(path, sum.keys) {
				s.family.records++
				if s.familyName != "Timeline Records" {
					mergeMonths(s.family.months, sum.months)
				}
				if s.metric != "" {
					for month := range sum.months {
						s.inv.addMetricMonth(s.metric, "Fitbit", month)
					}
				}
			}
			return sum, nil
		case '[':
			s.recordShape(path, "array")
			for dec.More() {
				child, err := s.scanValue(dec, path+"[]", parentKey)
				if err != nil {
					return jsonSummary{}, err
				}
				mergeMonths(sum.months, child.months)
			}
			end, err := dec.Token()
			if err != nil || end != json.Delim(']') {
				return jsonSummary{}, errors.New("bad JSON array")
			}
			return sum, nil
		}
	case string:
		s.recordShape(path, "string")
		if month := monthFromExplicitField(parentKey, x); month != "" {
			sum.months[month] = true
		}
	case json.Number:
		s.recordShape(path, "number")
		if month := monthFromExplicitField(parentKey, x.String()); month != "" {
			sum.months[month] = true
		}
	case float64:
		s.recordShape(path, "number")
	case bool:
		s.recordShape(path, "boolean")
	case nil:
		s.recordShape(path, "null")
	default:
		return jsonSummary{}, errors.New("unknown JSON token")
	}
	return sum, nil
}

func (s jsonScanner) isRecord(path string, keys map[string]bool) bool {
	switch s.familyName {
	case "Timeline Semantic Visits":
		return path == "$.timelineObjects[].placeVisit"
	case "Timeline Records":
		return path == "$.locations[]"
	case "Timeline On-Device":
		return path == "$.semanticSegments[]" || path == "$.timelineObjects[]"
	case "Fit Sessions":
		return isKnownRecordPath(path) && hasAnyKey(keys, "startTime", "Start time", "startTimestamp", "startTimestampMs")
	default:
		return isKnownRecordPath(path) && hasAnyKey(keys, "dateTime", "dateOfSleep", "startTime", "originalStartTime", "date", "Date")
	}
}

func isKnownRecordPath(path string) bool {
	return path == "$" || path == "$[]" || (strings.HasSuffix(path, "[]") && !strings.Contains(path, ".<key>"))
}

func hasAnyKey(keys map[string]bool, wants ...string) bool {
	for _, k := range wants {
		if keys[k] {
			return true
		}
	}
	return false
}

func (s jsonScanner) recordShape(path, typ string) {
	if s.family.shapes[path] == nil {
		s.family.shapes[path] = map[string]int{}
	}
	s.family.shapes[path][typ]++
	s.family.shapeCounts[path]++
}

func (i *inventory) addCSVFile(name, path, source string) error {
	fh, err := os.Open(path)
	if err != nil {
		return errPrivateInventory
	}
	defer fh.Close()
	r := csv.NewReader(fh)
	r.FieldsPerRecord = -1
	headers, err := r.Read()
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}
		return errPrivateInventory
	}
	f := i.family(name)
	f.files++
	for {
		row, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return errPrivateInventory
		}
		f.records++
		vals := map[string]string{}
		rowMonths := map[string]bool{}
		for idx, h := range headers {
			if idx >= len(row) {
				continue
			}
			vals[h] = row[idx]
			col := publicCSVHeader(h)
			if col != "" {
				if f.csv[col] == nil {
					f.csv[col] = map[string]int{}
				}
				f.csv[col][scalarType(row[idx])]++
				f.csvCounts[col]++
			}
			if month := monthFromExplicitField(h, row[idx]); month != "" {
				rowMonths[month] = true
			}
		}
		mergeMonths(f.months, rowMonths)
		i.addCSVMetrics(source, vals, rowMonths)
	}
	return nil
}

func (i *inventory) addCSVMetrics(source string, row map[string]string, rowMonths map[string]bool) {
	if len(rowMonths) == 0 || source == "" {
		return
	}
	if strings.HasPrefix(source, "Fitbit:") {
		metric := strings.TrimPrefix(source, "Fitbit:")
		for month := range rowMonths {
			i.addMetricMonth(metric, "Fitbit", month)
		}
		return
	}
	if source != "Fit" {
		return
	}
	for header, metric := range fitDailyMetrics {
		if nonEmpty(row, header) {
			for month := range rowMonths {
				i.addMetricMonth(metric, "Fit", month)
			}
		}
	}
}

func nonEmpty(row map[string]string, header string) bool {
	for h, v := range row {
		if strings.EqualFold(strings.TrimSpace(h), header) && strings.TrimSpace(v) != "" {
			return true
		}
	}
	return false
}

func publicCSVHeader(h string) string {
	clean := strings.TrimSpace(h)
	if safeCSVHeaders[strings.ToLower(clean)] {
		return clean
	}
	if clean == "" {
		return ""
	}
	return "<column>"
}

func publicJSONKey(k string) string {
	if safeJSONKeys[k] {
		return k
	}
	return "<key>"
}

func mergeMonths(dst, src map[string]bool) {
	for month := range src {
		dst[month] = true
	}
}

func monthFromExplicitField(key, value string) string {
	key = strings.TrimSpace(key)
	if timestampMillisKeys[key] {
		return monthFromMillis(value)
	}
	if !explicitDateKeys[key] {
		return ""
	}
	return validMonth(value)
}

func monthFromMillis(value string) string {
	ms, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil {
		return ""
	}
	return time.UnixMilli(ms).UTC().Format("2006-01")
}

func validMonth(value string) string {
	v := strings.TrimSpace(value)
	if v == "" {
		return ""
	}
	for _, layout := range []string{
		time.RFC3339Nano,
		"2006-01-02T15:04:05.000",
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
		"2006-01-02 15:04",
		"2006-01-02",
	} {
		if ts, err := time.Parse(layout, v); err == nil {
			return ts.Format("2006-01")
		}
	}
	return ""
}

func scalarType(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "empty"
	}
	if strings.EqualFold(s, "true") || strings.EqualFold(s, "false") {
		return "boolean"
	}
	var n json.Number = json.Number(s)
	if _, err := n.Float64(); err == nil {
		return "number"
	}
	return "string"
}

func (i *inventory) addMetricMonth(metric, source, month string) {
	if metric == "" || source == "" || month == "" {
		return
	}
	if i.metrics[metric] == nil {
		i.metrics[metric] = map[string]map[string]bool{}
	}
	if i.metrics[metric][month] == nil {
		i.metrics[metric][month] = map[string]bool{}
	}
	i.metrics[metric][month][source] = true
}

func (i *inventory) report() *Report {
	var r Report
	for _, t := range i.top {
		var exts []ExtensionCount
		for e, n := range t.exts {
			exts = append(exts, ExtensionCount{Extension: e, Count: n})
		}
		sort.Slice(exts, func(a, b int) bool { return exts[a].Extension < exts[b].Extension })
		r.TopFolders = append(r.TopFolders, TopFolder{Name: t.name, Files: t.files, Bytes: t.bytes, Extensions: exts})
	}
	sort.Slice(r.TopFolders, func(a, b int) bool { return r.TopFolders[a].Name < r.TopFolders[b].Name })
	for _, f := range i.families {
		fam := Family{Name: f.name, Files: f.files, Records: f.records}
		var months []string
		for m := range f.months {
			months = append(months, m)
		}
		sort.Strings(months)
		if len(months) > 0 {
			fam.FirstMonth = months[0]
			fam.LastMonth = months[len(months)-1]
		}
		for col, types := range f.csv {
			fam.CSVColumns = append(fam.CSVColumns, CSVColumn{Name: col, Types: typeCounts(types), Count: f.csvCounts[col]})
		}
		sort.Slice(fam.CSVColumns, func(a, b int) bool { return fam.CSVColumns[a].Name < fam.CSVColumns[b].Name })
		for path, types := range f.shapes {
			fam.Shapes = append(fam.Shapes, Shape{Path: path, Types: typeCounts(types), Count: f.shapeCounts[path]})
		}
		sort.Slice(fam.Shapes, func(a, b int) bool { return fam.Shapes[a].Path < fam.Shapes[b].Path })
		r.Families = append(r.Families, fam)
	}
	sort.Slice(r.Families, func(a, b int) bool { return r.Families[a].Name < r.Families[b].Name })
	for metric, byMonth := range i.metrics {
		var months []string
		sources := map[string]bool{}
		for month, srcs := range byMonth {
			if len(srcs) < 2 {
				continue
			}
			months = append(months, month)
			for s := range srcs {
				sources[s] = true
			}
		}
		if len(months) == 0 {
			continue
		}
		r.Overlaps = append(r.Overlaps, Overlap{Metric: metric, Sources: keys(sources), Months: sorted(months)})
	}
	sort.Slice(r.Overlaps, func(a, b int) bool { return r.Overlaps[a].Metric < r.Overlaps[b].Metric })
	return &r
}

func typeCounts(m map[string]int) []TypeCount {
	var out []TypeCount
	for typ, n := range m {
		out = append(out, TypeCount{Type: typ, Count: n})
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Type < out[b].Type })
	return out
}

func keys(m map[string]bool) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return sorted(out)
}

func sorted(s []string) []string {
	sort.Strings(s)
	return s
}

var safeExtensions = map[string]bool{".json": true, ".csv": true, ".txt": true, "(none)": true}

var fitDailyMetrics = map[string]string{
	"Step count":          "steps",
	"Move Minutes count":  "active-minutes",
	"Average heart rate":  "heart-rate",
	"Min heart rate":      "heart-rate",
	"Max heart rate":      "heart-rate",
	"Weight":              "weight",
	"Calories":            "calories",
	"Calories (kcal)":     "calories",
	"Distance":            "distance",
	"Distance (m)":        "distance",
	"Heart Points":        "heart-points",
	"Heart Minutes count": "heart-minutes",
}

var safeCSVHeaders = map[string]bool{
	"date":                true,
	"start time":          true,
	"end time":            true,
	"activity type":       true,
	"duration (ms)":       true,
	"step count":          true,
	"move minutes count":  true,
	"distance":            true,
	"distance (m)":        true,
	"calories":            true,
	"calories (kcal)":     true,
	"heart points":        true,
	"heart minutes count": true,
	"average heart rate":  true,
	"min heart rate":      true,
	"max heart rate":      true,
	"weight":              true,
	"bmi":                 true,
	"sleep minutes":       true,
}

var explicitDateKeys = map[string]bool{
	"Date":              true,
	"date":              true,
	"Start time":        true,
	"End time":          true,
	"startTime":         true,
	"endTime":           true,
	"originalStartTime": true,
	"lastModified":      true,
	"dateOfSleep":       true,
	"dateTime":          true,
	"timestamp":         true,
	"startTimestamp":    true,
	"endTimestamp":      true,
}

var timestampMillisKeys = map[string]bool{"timestampMs": true, "startTimestampMs": true, "endTimestampMs": true}

var safeJSONKeys = map[string]bool{
	"timelineObjects":   true,
	"placeVisit":        true,
	"activitySegment":   true,
	"location":          true,
	"name":              true,
	"address":           true,
	"placeId":           true,
	"duration":          true,
	"startTimestamp":    true,
	"endTimestamp":      true,
	"startTimestampMs":  true,
	"endTimestampMs":    true,
	"activityType":      true,
	"locations":         true,
	"timestamp":         true,
	"timestampMs":       true,
	"latitudeE7":        true,
	"longitudeE7":       true,
	"semanticSegments":  true,
	"visit":             true,
	"topCandidate":      true,
	"semanticType":      true,
	"startTime":         true,
	"endTime":           true,
	"dateOfSleep":       true,
	"minutesAsleep":     true,
	"timeInBed":         true,
	"levels":            true,
	"data":              true,
	"dateTime":          true,
	"level":             true,
	"seconds":           true,
	"value":             true,
	"bpm":               true,
	"confidence":        true,
	"body-weight":       true,
	"activityName":      true,
	"exercise":          true,
	"sessions":          true,
	"sleep":             true,
	"steps":             true,
	"heartRate":         true,
	"weight":            true,
	"distance":          true,
	"calories":          true,
	"activeDuration":    true,
	"originalStartTime": true,
	"lastModified":      true,
	"logId":             true,
	"source":            true,
	"dataset":           true,
	"point":             true,
	"fpVal":             true,
	"intVal":            true,
	"stringVal":         true,
	"mapVal":            true,
	"exportDate":        true,
}

func (r *Report) String() string {
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return fmt.Sprintf("%+v", *r)
	}
	return string(b)
}
