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
	"regexp"
	"sort"
	"strings"
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
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return errPrivateInventory
		}
		rel, err := filepath.Rel(base, path)
		if err != nil {
			return errPrivateInventory
		}
		rel = filepath.ToSlash(rel)
		top := publicTopFolder(rel)
		if top == "" {
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
	if name == "Google Photos" { // Plan input narrowed Phase A to Timeline/Fit/Fitbit; photo gap is owner-gated.
		return
	}
	if ext == "" {
		ext = "(none)"
	}
	ext = strings.ToLower(ext)
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
	if top == "Google Photos" || top == "Other" {
		return nil
	}
	low := strings.ToLower(rel)
	ext := strings.ToLower(filepath.Ext(rel))
	switch top {
	case "Location History":
		switch {
		case ext == ".json" && strings.Contains(low, "semantic location history"):
			return i.addTimelineSemantic(path)
		case ext == ".json" && filepath.Base(low) == "records.json":
			return i.addJSONFile("Timeline Records", path, countLocations, nil)
		case ext == ".json" && filepath.Base(low) == "timeline.json":
			return i.addJSONFile("Timeline On-Device", path, countOnDeviceTimeline, nil)
		}
	case "Fit":
		switch {
		case strings.Contains(low, "all data") || strings.Contains(low, "data source"):
			f := i.family("Fit Raw Data Sources")
			f.files++
			return nil
		case ext == ".csv" && (strings.Contains(low, "daily") || strings.Contains(low, "activity metrics")):
			return i.addCSVFile("Fit Daily Aggregates", path, func(row map[string]string) {
				m := monthFromRow(row, "Date", "date")
				if m != "" {
					i.addMetricMonth("steps", "Fit", m)
				}
			})
		case (ext == ".csv" || ext == ".json") && strings.Contains(low, "session"):
			if ext == ".csv" {
				return i.addCSVFile("Fit Sessions", path, nil)
			}
			return i.addJSONFile("Fit Sessions", path, countTopRecords, nil)
		}
	case "Fitbit":
		name, metric := fitbitFamily(low)
		if name == "" {
			return nil
		}
		if ext == ".csv" {
			return i.addCSVFile(name, path, func(row map[string]string) {
				if metric != "" {
					if m := firstMonthInRow(row); m != "" {
						i.addMetricMonth(metric, "Fitbit", m)
					}
				}
			})
		}
		if ext == ".json" {
			return i.addJSONFile(name, path, countTopRecords, func(v any) {
				if metric != "" {
					for _, m := range monthsFromValue(v) {
						i.addMetricMonth(metric, "Fitbit", m)
					}
				}
			})
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

func (i *inventory) addTimelineSemantic(path string) error {
	return i.addJSONFile("Timeline Semantic Visits", path, func(v any) (int, []string) {
		var count int
		var months []string
		if root, ok := v.(map[string]any); ok {
			for _, item := range asSlice(root["timelineObjects"]) {
				obj, _ := item.(map[string]any)
				pv, ok := obj["placeVisit"]
				if !ok {
					continue
				}
				count++
				months = append(months, monthsFromValue(pv)...)
			}
		}
		return count, months
	}, nil)
}

func (i *inventory) addJSONFile(name, path string, count func(any) (int, []string), visit func(any)) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return errPrivateInventory
	}
	var v any
	dec := json.NewDecoder(strings.NewReader(string(b)))
	dec.UseNumber()
	if err := dec.Decode(&v); err != nil {
		return errPrivateInventory
	}
	f := i.family(name)
	f.files++
	if visit != nil {
		visit(v)
	}
	records, months := count(v)
	f.records += records
	for _, m := range months {
		f.months[m] = true
	}
	collectShapes(v, "$", f)
	return nil
}

func countLocations(v any) (int, []string) {
	root, _ := v.(map[string]any)
	locs := asSlice(root["locations"])
	return len(locs), nil
}

func countOnDeviceTimeline(v any) (int, []string) {
	root, _ := v.(map[string]any)
	var count int
	var months []string
	for _, item := range asSlice(root["semanticSegments"]) {
		count++
		months = append(months, monthsFromValue(item)...)
	}
	if count == 0 {
		for _, item := range asSlice(root["timelineObjects"]) {
			count++
			months = append(months, monthsFromValue(item)...)
		}
	}
	return count, months
}

func countTopRecords(v any) (int, []string) {
	switch x := v.(type) {
	case []any:
		return len(x), monthsFromValue(v)
	case map[string]any:
		var n int
		for _, val := range x {
			if a, ok := val.([]any); ok {
				n += len(a)
			} else {
				n++
			}
		}
		return n, monthsFromValue(v)
	default:
		return 1, monthsFromValue(v)
	}
}

func asSlice(v any) []any {
	if a, ok := v.([]any); ok {
		return a
	}
	return nil
}

func (i *inventory) addCSVFile(name, path string, visit func(map[string]string)) error {
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
		for idx, h := range headers {
			if idx >= len(row) {
				continue
			}
			vals[h] = row[idx]
			col := publicCSVHeader(h)
			if col == "" {
				continue
			}
			if f.csv[col] == nil {
				f.csv[col] = map[string]int{}
			}
			f.csv[col][scalarType(row[idx])]++
			f.csvCounts[col]++
		}
		if visit != nil {
			visit(vals)
		}
		if m := firstMonthInRow(vals); m != "" {
			f.months[m] = true
		}
	}
	return nil
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

func collectShapes(v any, path string, f *familyAcc) {
	t := jsonType(v)
	if f.shapes[path] == nil {
		f.shapes[path] = map[string]int{}
	}
	f.shapes[path][t]++
	f.shapeCounts[path]++
	switch x := v.(type) {
	case []any:
		for _, item := range x {
			collectShapes(item, path+"[]", f)
		}
	case map[string]any:
		for key, val := range x {
			collectShapes(val, path+"."+publicJSONKey(key), f)
		}
	}
}

func publicJSONKey(k string) string {
	if safeJSONKeys[k] {
		return k
	}
	return "<key>"
}

func jsonType(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case json.Number:
		return "number"
	case float64:
		return "number"
	case string:
		return "string"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	default:
		return "unknown"
	}
}

var monthRE = regexp.MustCompile(`\b\d{4}-\d{2}`)

func monthsFromValue(v any) []string {
	seen := map[string]bool{}
	var out []string
	var walk func(any)
	walk = func(v any) {
		switch x := v.(type) {
		case string:
			m := monthRE.FindString(x)
			if m != "" && !seen[m] {
				seen[m] = true
				out = append(out, m)
			}
		case []any:
			for _, item := range x {
				walk(item)
			}
		case map[string]any:
			for _, val := range x {
				walk(val)
			}
		}
	}
	walk(v)
	sort.Strings(out)
	return out
}

func monthFromRow(row map[string]string, keys ...string) string {
	for _, want := range keys {
		for h, v := range row {
			if strings.EqualFold(strings.TrimSpace(h), want) {
				if m := monthRE.FindString(v); m != "" {
					return m
				}
			}
		}
	}
	return ""
}

func firstMonthInRow(row map[string]string) string {
	months := map[string]bool{}
	for _, v := range row {
		if m := monthRE.FindString(v); m != "" {
			months[m] = true
		}
	}
	var out []string
	for m := range months {
		out = append(out, m)
	}
	sort.Strings(out)
	if len(out) == 0 {
		return ""
	}
	return out[0]
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

var safeCSVHeaders = map[string]bool{
	"date":               true,
	"start time":         true,
	"end time":           true,
	"activity type":      true,
	"duration (ms)":      true,
	"step count":         true,
	"move minutes count": true,
	"distance":           true,
	"calories":           true,
	"average heart rate": true,
	"min heart rate":     true,
	"max heart rate":     true,
	"weight":             true,
	"bmi":                true,
	"sleep minutes":      true,
}

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
}

func (r *Report) String() string {
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return fmt.Sprintf("%+v", *r)
	}
	return string(b)
}
