package tests

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// diagrams: the mermaid diagrams of the docs say what the DDL says.
// A  structure: exactly the seven diagrams, each announced by a `%% diagram: <id>` line, each of a known type;
// B  the two ER diagrams draw keys only: every table is drawn; every drawn column is a PK or FK column with its
//
//	declared type and marks; every PK and FK column is drawn somewhere; every foreign key is a relationship and
//	every relationship a foreign key, labelled by its first column, with the cardinality the constraint implies
//	(NOT NULL `||`, nullable `|o`; unique in the child `o|`, else `o{`);
//
// C  the link map against `link_kinds`: same edges (a node naming several types stands for each), same symmetric arrows;
// D  the correction story of cookbook/correct-a-measurement, executed;
// E  the save flow and cookbook/save-a-body name the same steps; the other diagrams name what they rely on.
func diagrams(s *S) {
	// ---- A
	byID := map[string][]string{}
	head := regexp.MustCompile(`^%% diagram: ([a-z-]+)\n`)
	for _, m := range mermaidBlock.FindAllStringSubmatch(s.d.Text(), -1) {
		b := m[1]
		h := head.FindStringSubmatchIndex(b)
		s.K("every mermaid block starts with a `%% diagram: id` line", h != nil, clip(b, 40))
		if h != nil {
			id := b[h[2]:h[3]]
			byID[id] = append(byID[id], b[h[1]:])
		}
	}
	want := map[string]string{"er-core": "erDiagram", "er-facts": "erDiagram", "link-map": "flowchart", "page-life": "stateDiagram-v2",
		"correct-measurement": "stateDiagram-v2", "writers": "flowchart", "save-flow": "flowchart"}
	once := len(byID) == len(want)
	counts := map[string]int{}
	for k, v := range byID {
		counts[k] = len(v)
		_, known := want[k]
		once = once && known && len(v) == 1
	}
	s.K("exactly the seven diagrams, each once", once, counts)
	var wantIDs []string
	for k := range want {
		wantIDs = append(wantIDs, k)
	}
	sort.Strings(wantIDs)
	for _, k := range wantIDs {
		if v, ok := byID[k]; ok {
			s.K(k+" is a "+want[k], strings.HasPrefix(v[0], want[k]))
		}
	}
	body := func(k string) string {
		if v := byID[k]; len(v) > 0 {
			return v[0]
		}
		return ""
	}

	// ---- B
	c := s.connect("")
	c.must(s.ddl)
	tables := c.col("select name from pragma_table_list where schema='main' and type='table' and name not like 'sqlite_%'")
	type colInfo struct {
		typ     string
		notnull bool
		pk      bool
	}
	info := map[string]map[string]colInfo{}
	fkCols := map[string]map[string]bool{}
	type fk struct {
		child, parent string
		cols          []string
	}
	var fks []fk
	for _, t := range tables {
		info[t] = map[string]colInfo{}
		for _, r := range c.rows("select name, type, \"notnull\", pk from pragma_table_info(?)", t) {
			info[t][val(r[0])] = colInfo{val(r[1]), r[2].(int64) != 0, r[3].(int64) != 0}
		}
		fkCols[t] = map[string]bool{}
		groups := map[int64][][]any{}
		var order []int64
		for _, r := range c.rows("select id, seq, \"table\", \"from\" from pragma_foreign_key_list(?) order by id, seq", t) {
			id := r[0].(int64)
			if _, ok := groups[id]; !ok {
				order = append(order, id)
			}
			groups[id] = append(groups[id], r)
		}
		for _, id := range order {
			var cols []string
			for _, r := range groups[id] {
				cols = append(cols, val(r[3]))
				fkCols[t][val(r[3])] = true
			}
			fks = append(fks, fk{t, val(groups[id][0][2]), cols})
		}
	}
	uniqueSets := func(t string) []map[string]bool {
		pk := map[string]bool{}
		for x, v := range info[t] {
			if v.pk {
				pk[x] = true
			}
		}
		sets := []map[string]bool{pk}
		for _, r := range c.rows("select name, \"unique\" from pragma_index_list(?)", t) {
			if r[1].(int64) != 0 {
				set := map[string]bool{}
				for _, n := range c.col("select name from pragma_index_info(?)", r[0]) {
					set[n] = true
				}
				sets = append(sets, set)
			}
		}
		var out []map[string]bool
		for _, s := range sets {
			if len(s) > 0 {
				out = append(out, s)
			}
		}
		return out
	}
	type attr struct {
		col, typ string
		marks    map[string]bool
	}
	type drawing struct {
		diagram string
		attrs   []attr
	}
	ents := map[string][]drawing{}
	type rel struct{ parent, ps, cs, child, label string }
	var rels []rel
	drawnCols := map[string]map[string]bool{}
	for _, t := range tables {
		drawnCols[t] = map[string]bool{}
	}
	entRE := regexp.MustCompile(`(?sm)^    (\w+) \{\n(.*?)^    \}`)
	attrRE := regexp.MustCompile(`^\s+(\w+) (\w+)(?: ((?:PK|FK|UK)(?:, (?:PK|FK|UK))*))?(?: "[^"]*")?$`)
	relRE := regexp.MustCompile(`(?m)^    (\w+)\s+(\|\||\|o)--(o\||o\{)\s+(\w+)\s+: "(\w+)"`)
	for _, k := range []string{"er-core", "er-facts"} {
		for _, m := range entRE.FindAllStringSubmatch(body(k), -1) {
			var attrs []attr
			for _, line := range strings.Split(strings.TrimSuffix(m[2], "\n"), "\n") {
				a := attrRE.FindStringSubmatch(line)
				s.K(fmt.Sprintf("%s.%s: attribute line parses: %s", k, m[1], strings.TrimSpace(line)), a != nil)
				if a != nil {
					marks := map[string]bool{}
					if a[3] != "" {
						for _, x := range strings.Split(a[3], ", ") {
							marks[x] = true
						}
					}
					attrs = append(attrs, attr{a[2], a[1], marks})
				}
			}
			ents[m[1]] = append(ents[m[1]], drawing{k, attrs})
		}
		for _, m := range relRE.FindAllStringSubmatch(body(k), -1) {
			rels = append(rels, rel{m[1], m[2], m[3], m[4], m[5]})
		}
	}
	var undrawn, unknown []string
	for _, t := range tables {
		if _, ok := ents[t]; !ok {
			undrawn = append(undrawn, t)
		}
	}
	for e := range ents {
		if !contains(tables, e) {
			unknown = append(unknown, e)
		}
	}
	s.K("every table of the DDL is drawn", len(undrawn) == 0, undrawn)
	s.K("every drawn entity is a table of the DDL", len(unknown) == 0, sorted(unknown))
	var entNames []string
	for e := range ents {
		entNames = append(entNames, e)
	}
	sort.Strings(entNames)
	for _, name := range entNames {
		cols, ok := info[name]
		if !ok {
			continue
		}
		for _, d := range ents[name] {
			distinct := map[string]bool{}
			for _, a := range d.attrs {
				distinct[a.col] = true
			}
			s.K(fmt.Sprintf("%s.%s lists each column at most once", d.diagram, name), len(distinct) == len(d.attrs))
			for _, a := range d.attrs {
				ci, isCol := cols[a.col]
				if !s.K(fmt.Sprintf("%s.%s.%s is a column of %s", d.diagram, name, a.col, name), isCol) {
					continue
				}
				drawnCols[name][a.col] = true
				s.K(fmt.Sprintf("%s.%s.%s is a key column (the diagrams draw keys only)", d.diagram, name, a.col), ci.pk || fkCols[name][a.col])
				s.K(fmt.Sprintf("%s.%s.%s has the declared type", d.diagram, name, a.col), a.typ == ci.typ, a.typ)
				s.K(fmt.Sprintf("%s.%s.%s PK mark = primary key", d.diagram, name, a.col), a.marks["PK"] == ci.pk, a.marks)
				s.K(fmt.Sprintf("%s.%s.%s FK mark = part of a foreign key", d.diagram, name, a.col), a.marks["FK"] == fkCols[name][a.col], a.marks)
			}
		}
	}
	for _, t := range tables {
		var missing []string
		for x, v := range info[t] {
			if (v.pk || fkCols[t][x]) && !drawnCols[t][x] {
				missing = append(missing, x)
			}
		}
		s.K("every key column of "+t+" is drawn somewhere", len(missing) == 0, sorted(missing))
	}
	type relKey struct{ parent, child, label string }
	drawn := map[relKey][]string{}
	for _, r := range rels {
		k := relKey{r.parent, r.child, r.label}
		drawn[k] = append(drawn[k], r.ps+r.cs)
	}
	wantRel := map[relKey]fk{}
	for _, f := range fks {
		wantRel[relKey{f.parent, f.child, f.cols[0]}] = f
	}
	var notDrawn, notFK []string
	for k := range wantRel {
		if _, ok := drawn[k]; !ok {
			notDrawn = append(notDrawn, fmt.Sprint(k))
		}
	}
	for k := range drawn {
		if _, ok := wantRel[k]; !ok {
			notFK = append(notFK, fmt.Sprint(k))
		}
	}
	s.K("every foreign key is drawn as `parent --- child : first FK column`", len(notDrawn) == 0, sorted(notDrawn))
	s.K("every drawn relationship is a foreign key", len(notFK) == 0, sorted(notFK))
	var wantKeys []relKey
	for k := range wantRel {
		wantKeys = append(wantKeys, k)
	}
	sort.Slice(wantKeys, func(i, j int) bool { return fmt.Sprint(wantKeys[i]) < fmt.Sprint(wantKeys[j]) })
	for _, k := range wantKeys {
		f := wantRel[k]
		got, ok := drawn[k]
		if !ok {
			continue
		}
		nullable := false
		for _, x := range f.cols {
			nullable = nullable || (!info[f.child][x].notnull && !info[f.child][x].pk)
		}
		uniq := false
		for _, set := range uniqueSets(f.child) {
			sub := true
			for x := range set {
				sub = sub && contains(f.cols, x)
			}
			uniq = uniq || sub
		}
		exp := map[bool]string{true: "|o", false: "||"}[nullable] + map[bool]string{true: "o|", false: "o{"}[uniq]
		s.K(fmt.Sprintf("%s -> %s (%s): cardinality %s", k.parent, k.child, k.label, exp), contains(got, exp), got, exp)
	}

	// ---- C  the link map
	nodes := map[string][]string{}
	for _, m := range regexp.MustCompile(`(?m)^    (\w+)\(?\["?([^"\]]+)"?\]\)?$`).FindAllStringSubmatch(body("link-map"), -1) {
		if m[2] == "any entity" {
			nodes[m[1]] = []string{"any"}
			continue
		}
		for _, x := range strings.Split(m[2], ",") {
			nodes[m[1]] = append(nodes[m[1]], strings.TrimSpace(x))
		}
	}
	type edge struct {
		from, to string
		sym      bool
	}
	edges := map[edge]map[string]bool{}
	typesOf := func(n string) []string {
		if t, ok := nodes[n]; ok {
			return t
		}
		return []string{"?" + n}
	}
	for _, m := range regexp.MustCompile(`(?m)^    (\w+) (-->|<-->)\|"([^"]+)"\| (\w+)`).FindAllStringSubmatch(body("link-map"), -1) {
		for _, f := range typesOf(m[1]) {
			for _, t := range typesOf(m[4]) {
				e := edge{f, t, m[2] == "<-->"}
				if edges[e] == nil {
					edges[e] = map[string]bool{}
				}
				for _, k := range strings.Split(m[3], ",") {
					edges[e][strings.TrimSpace(k)] = true
				}
			}
		}
	}
	exp := map[edge]map[string]bool{}
	for _, r := range c.rows("select kind, symmetric, from_types, to_types from link_kinds") {
		split := func(v any) []string {
			if v == nil || v == "" {
				return []string{"any"}
			}
			return strings.Split(val(v), ",")
		}
		for _, f := range split(r[2]) {
			for _, t := range split(r[3]) {
				e := edge{f, t, r[1].(int64) != 0}
				if exp[e] == nil {
					exp[e] = map[string]bool{}
				}
				exp[e][val(r[0])] = true
			}
		}
	}
	setStr := func(m map[edge]map[string]bool) string {
		var out []string
		for e, ks := range m {
			var kk []string
			for k := range ks {
				kk = append(kk, k)
			}
			out = append(out, fmt.Sprintf("%s->%s sym=%v %v", e.from, e.to, e.sym, sorted(kk)))
		}
		return strings.Join(sorted(out), "; ")
	}
	s.K("the link map has the same edges as link_kinds", setStr(edges) == setStr(exp), "map: "+setStr(edges), "DDL: "+setStr(exp))
	known := true
	for _, ts := range nodes {
		for _, t := range ts {
			known = known && contains([]string{"any", "page", "person", "place", "metric", "file", "period"}, t)
		}
	}
	s.K("every node of the map names entity types or `any entity`", known, nodes)

	// ---- D  the correction story
	cm := body("correct-measurement")
	steps := regexp.MustCompile(`INSERT row (\d), value (\S+?)(?:, supersedes (\d))?\n`).FindAllStringSubmatch(cm+"\n", -1)
	var states []string
	for _, m := range regexp.MustCompile(`state "view shows ([^"]+)" as V\d`).FindAllStringSubmatch(cm, -1) {
		states = append(states, m[1])
	}
	c = s.fresh()
	mid := c.metric("weight", "kg")
	var shown, rows []string
	for _, st := range steps {
		rows = append(rows, st[1])
		var v, sup any
		if st[2] != "NULL" {
			f, _ := strconv.ParseFloat(st[2], 64)
			v = f
		}
		if st[3] != "" {
			sup, _ = strconv.Atoi(st[3])
		}
		c.measure(mid, "2026-09-30", v, M{"supersedes_id": sup})
		var vs []string
		for _, r := range c.rows("select value from measurement_values where metric_id=?", mid) {
			vs = append(vs, pyFloat(r[0]))
		}
		if len(vs) == 0 {
			shown = append(shown, "nothing (retracted)")
		} else {
			shown = append(shown, strings.Join(vs, ", "))
		}
	}
	s.K("the story has four inserts", strings.Join(rows, ",") == "1,2,3,4", steps)
	s.K("executed, the view shows what each state of the diagram says", eq(shown, states), shown, states)

	// ---- E
	stepSet := func(re string, text string) string {
		set := map[string]bool{}
		for _, m := range regexp.MustCompile(re).FindAllStringSubmatch(text, -1) {
			set[m[1]] = true
		}
		var out []string
		for k := range set {
			out = append(out, k)
		}
		return strings.Join(sorted(out), ",")
	}
	stSQL := stepSet(`(?m)^-- (0|1|2a|2b|3|4)\)`, s.d.Block("save-a-body"))
	stFig := stepSet(`\b(0|1|2a|2b|3|4)\. `, body("save-flow"))
	s.K("the save flow names steps 0, 1, 2a, 2b, 3, 4 exactly as cookbook/save-a-body does", stSQL == stFig && stSQL == "0,1,2a,2b,3,4", stSQL, stFig)
	has := func(text string, words ...string) bool {
		for _, w := range words {
			if !strings.Contains(text, w) {
				return false
			}
		}
		return true
	}
	s.K("the save flow has BEGIN IMMEDIATE, SAVEPOINT, RELEASE, ROLLBACK TO and COMMIT, as cookbook/save-a-body", has(body("save-flow"), "BEGIN IMMEDIATE", "SAVEPOINT target", "RELEASE target", "ROLLBACK TO target", "COMMIT"))
	s.K("the page diagram has the named state, promotion by type and the day page", has(body("page-life"), "Named", "entities.entity_type", "day page"))
	s.K("the writers diagram names BEGIN IMMEDIATE, WAL, mode=ro", has(body("writers"), "BEGIN IMMEDIATE", "WAL", "mode=ro"))
}

var mermaidBlock = regexp.MustCompile("(?s)```mermaid\n(.*?)\n```")

// pyFloat writes a REAL the way the diagrams print it: 71.2, and a whole number with its .0.
func pyFloat(v any) string {
	f, ok := v.(float64)
	if !ok {
		return val(v)
	}
	s := strconv.FormatFloat(f, 'f', -1, 64)
	if !strings.ContainsAny(s, ".eE") {
		s += ".0"
	}
	return s
}
