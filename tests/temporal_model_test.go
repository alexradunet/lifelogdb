package tests

import (
	"context"
	"fmt"
	"math/rand"
	"path/filepath"
	"sort"
	"time"
)

func temporalModelDB(s *S, name string) *C {
	c := s.freshWith(F{Path: filepath.Join(s.dir, name), Hardened: true})
	c.must("PRAGMA synchronous=FULL")
	c.must("BEGIN IMMEDIATE")
	return c
}

func temporalCookbookModels(s *S) {
	temporalContainmentLifecycle(s)
	temporalCategoryLifecycle(s)
	temporalCorrectionModel(s)
	temporalDeadlineModel(s)
	temporalPeriodStorageModel(s)
}

func temporalCategoryLifecycle(s *S) {
	c := temporalModelDB(s, "temporal-category-lifecycle.db")
	root, middle, leaf := c.page("Biomarkers"), c.page("Blood"), c.page("Lipids")
	metric := c.metric("LDL", "mg/dL")
	link := func(from, to int64) {
		c.must("INSERT INTO links(from_id,to_id,kind,created_at,source) VALUES(?,?,'part-of',"+NOW+",'ui')", from, to)
	}
	link(middle, root)
	link(leaf, middle)
	link(metric, leaf)
	query := statements(s.d.Block("metrics-by-category"))[2]
	read := func(id int64) string { return c.tab(query, P{"parent_id": id}) }
	s.K("category traversal reads live descendants", read(root) == "Lipids|LDL|mg/dL", read(root))
	c.must("UPDATE entities SET deleted_at="+NOW+" WHERE id=?", root)
	s.K("category tombstoned root hides live descendant metrics", read(root) == "", read(root))
	s.K("category root tombstone retains direct live category contents", read(leaf) == "Lipids|LDL|mg/dL")
	c.must("UPDATE entities SET deleted_at=NULL WHERE id=?", root)
	c.must("UPDATE entities SET deleted_at="+NOW+" WHERE id=?", middle)
	s.K("category tombstoned intermediate stops descendant traversal", read(root) == "", read(root))
	other := c.page("Other live route")
	link(leaf, other)
	link(other, root)
	s.K("category alternate live path retains descendant membership", read(root) == "Lipids|LDL|mg/dL", read(root))
	link(root, other)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	rows, err := c.queryCtx(ctx, query, P{"parent_id": root})
	s.K("category live cycle terminates without duplicate metric rows", err == nil && tab(rows) == "Lipids|LDL|mg/dL", err, tab(rows))
	c.must("UPDATE entities SET deleted_at="+NOW+" WHERE id=?", other)
	s.K("category all intermediate paths tombstoned hide descendant metrics", read(root) == "", read(root))
	c.must("UPDATE entities SET deleted_at=NULL WHERE id=?", middle)
	s.K("category intermediate revival restores retained membership", read(root) == "Lipids|LDL|mg/dL" && c.n("SELECT count(*) FROM links") == 6 && c.integrityOK(), read(root))
	c.must("COMMIT")
}

func temporalContainmentLifecycle(s *S) {
	c := temporalModelDB(s, "temporal-containment.db")
	root, middle, leaf := c.named("place", "Japan"), c.named("place", "Kanto"), c.named("place", "Tokyo")
	day := c.dayPage("2026-10-01", "A synthetic visit")
	for _, edge := range [][2]int64{{leaf, middle}, {middle, root}} {
		c.must("INSERT INTO links(from_id,to_id,kind,created_at,source) VALUES(?,?,'located-in',"+NOW+",'ui')", edge[0], edge[1])
	}
	c.must("INSERT INTO links(from_id,to_id,kind,created_at,source) VALUES(?,?,'at',"+NOW+",'ui')", day, leaf)
	query := s.d.Block("inside-a-place")
	read := func(id int64) string {
		return c.tab(query, P{"place_id": id, "from_day": "2026-10-01", "to_day": "2026-10-01"})
	}
	s.K("containment reads a live descendant through live ancestors", read(root) == "2026-10-01|Tokyo", read(root))
	c.must("UPDATE entities SET deleted_at="+NOW+" WHERE id=?", root)
	s.K("containment tombstoned root hides live descendant days", read(root) == "", read(root))
	s.K("containment root tombstone retains direct descendant history", read(leaf) == "2026-10-01|Tokyo" && c.n("SELECT count(*) FROM links") == 3)
	c.must("UPDATE entities SET deleted_at=NULL WHERE id=?", root)
	c.must("UPDATE entities SET deleted_at="+NOW+" WHERE id=?", middle)
	s.K("containment tombstoned intermediate stops descendant traversal", read(root) == "", read(root))
	c.must("UPDATE entities SET deleted_at=NULL WHERE id=?", middle)
	s.K("containment revival restores retained descendant days", read(root) == "2026-10-01|Tokyo", read(root))
	c.must("COMMIT")
}

// This model owns chronological insertion order and chain membership as plain
// slices. It does not use the SQL leaf predicate, recursive CTE, ID order, or
// recording timestamp order to determine the latest eligible fact in a chain.
func temporalCorrectionModel(s *S) {
	const seed int64 = 713_029
	rng := rand.New(rand.NewSource(seed))
	c := temporalModelDB(s, "temporal-corrections.db")
	weight, other := c.metric("weight", "kg"), c.metric("Other metric", "kg")
	kind := c.page("Workout")
	sessions := []int64{sessionFixture(c, kind, nil), sessionFixture(c, kind, nil)}
	ids := rng.Perm(1000)
	used := 0
	type fact struct {
		id, metric int64
		day, at    string
		value      any
		session    any
	}
	var chains [][]fact
	for chain := range 40 {
		length := 3 + rng.Intn(9)
		var prior any
		var members []fact
		metric := weight
		if chain%5 == 0 {
			metric = other
		}
		var session any
		if chain%3 != 0 {
			session = sessions[chain%2]
		}
		for depth := range length {
			id := int64(ids[used] + 10)
			used++
			var value any = float64(chain*20+depth) + 0.25
			if depth > 0 && rng.Intn(3) == 0 {
				value = nil
			}
			day := fmt.Sprintf("2026-10-%02d", chain%28+1)
			at := fmt.Sprintf("2026-11-01T%02d:00:00.000Z", rng.Intn(16))
			c.must("INSERT INTO measurements(id,metric_id,session_id,day,value,source,created_at,supersedes_id) VALUES(?,?,?,?,?,'ui',?,?)", id, metric, session, day, value, at, prior)
			members = append(members, fact{id, metric, day, at, value, session})
			prior = id
		}
		chains = append(chains, members)
	}
	c.must("UPDATE sessions SET deleted_at='2026-11-02T00:00:00.000Z' WHERE id=?", sessions[1])
	c.must("UPDATE entities SET deleted_at='2026-11-03T00:00:00.000Z' WHERE id=?", weight)
	query := sqlBlocks(s.d.Page("cookbook/metric-series.md"))[1]
	for hour := range 17 {
		cutoff := fmt.Sprintf("2026-11-01T%02d:00:00.000Z", hour)
		var want []fact
		for _, chain := range chains {
			for i := len(chain) - 1; i >= 0; i-- {
				candidate := chain[i]
				if candidate.at > cutoff || candidate.metric != weight {
					continue
				}
				if candidate.value != nil {
					want = append(want, candidate)
				}
				break
			}
		}
		sort.Slice(want, func(i, j int) bool {
			if want[i].day != want[j].day {
				return want[i].day < want[j].day
			}
			return want[i].id < want[j].id
		})
		var wantRows [][]any
		for _, f := range want {
			var sessionDeleted any
			if f.session == sessions[1] {
				sessionDeleted = "2026-11-02T00:00:00.000Z"
			}
			wantRows = append(wantRows, []any{f.id, f.day, f.value, f.session, "2026-11-03T00:00:00.000Z", sessionDeleted})
		}
		got := c.rows(query, P{"metric": "weight", "as_of": cutoff})
		s.K(fmt.Sprintf("recorded-time model seed %d cutoff %s", seed, cutoff), tab(got) == tab(wantRows), "got", tab(got), "want", tab(wantRows))
	}
	var expectedLeaves []int64
	for _, chain := range chains {
		leaf := chain[len(chain)-1]
		if leaf.value != nil {
			expectedLeaves = append(expectedLeaves, leaf.id)
		}
	}
	sort.Slice(expectedLeaves, func(i, j int) bool { return expectedLeaves[i] < expectedLeaves[j] })
	var gotLeaves []int64
	for _, row := range c.rows("SELECT id FROM measurement_values ORDER BY id") {
		gotLeaves = append(gotLeaves, row[0].(int64))
	}
	s.K("current measurement leaves match insertion-chain model across all lifecycles", fmt.Sprint(gotLeaves) == fmt.Sprint(expectedLeaves), gotLeaves, expectedLeaves)
	s.K("correction-chain model retains file integrity", c.integrityOK())
	c.must("COMMIT")
}

// Generate candidate dates forward from the original anchor, using calendar
// constructors rather than the SQL's day-by-day modulo membership predicate.
func temporalCalendarKeys(anchor time.Time, unit string, every int64, through time.Time) []string {
	var keys []string
	for k := int64(0); ; k++ {
		// No admitted cadence advances by four million units within years 0000–9999.
		// This bound also prevents overflowing k*every for huge stored intervals.
		if k > 0 && every > 4_000_000/k {
			break
		}
		n := int(k * every)
		var candidate time.Time
		switch unit {
		case "day":
			candidate = anchor.AddDate(0, 0, n)
		case "week":
			candidate = anchor.AddDate(0, 0, 7*n)
		case "month", "year":
			year, month := anchor.Year(), anchor.Month()
			if unit == "month" {
				month += time.Month(n)
			} else {
				year += n
			}
			first := time.Date(year, month, 1, 0, 0, 0, 0, time.UTC)
			lastDay := first.AddDate(0, 1, -1).Day()
			candidate = first.AddDate(0, 0, min(anchor.Day(), lastDay)-1)
		default:
			panic("unsupported reference cadence")
		}
		if candidate.Year() > 9999 || candidate.After(through) {
			break
		}
		keys = append(keys, candidate.Format("2006-01-02"))
	}
	return keys
}

func temporalDeadlineModel(s *S) {
	c := temporalModelDB(s, "temporal-deadlines.db")
	query := sqlBlocks(s.d.Page("cookbook/tasks.md"))[5]
	for scenario, tc := range []struct {
		anchor, unit string
		every        int64
	}{
		{"0000-02-28", "day", 3},
		{"2026-10-05", "day", 7},
		{"2000-02-21", "week", 2},
		{"9999-11-01", "week", 1},
		{"0000-01-31", "month", 1},
		{"1900-01-31", "month", 1},
		{"2000-01-31", "month", 2},
		{"9999-10-31", "month", 1},
		{"1896-02-29", "year", 1},
		{"1996-02-29", "year", 1},
		{"9998-02-28", "year", 1},
		{"2026-01-01", "year", 9223372036854775807},
	} {
		anchor, err := time.Parse("2006-01-02", tc.anchor)
		if err != nil {
			stop("model anchor: %v", err)
		}
		span := 70
		if tc.unit == "month" {
			span = 500
		} else if tc.unit == "year" {
			span = 6 * 366
		}
		from, through := anchor.AddDate(0, 0, 20), anchor.AddDate(0, 0, span)
		if through.Year() > 9999 {
			through = time.Date(9999, 12, 31, 0, 0, 0, 0, time.UTC)
		}
		fromDay, throughDay := from.Format("2006-01-02"), through.Format("2006-01-02")
		keys := temporalCalendarKeys(anchor, tc.unit, tc.every, through)
		task := planningTask(c, M{"repeat_unit": tc.unit, "repeat_every": tc.every, "anchor_day": tc.anchor})
		type occurrence struct {
			id              int64
			key, due, state string
			deleted         bool
		}
		persisted := map[string]occurrence{}
		for i, index := range []int{0, 1, len(keys) / 4, len(keys) / 2, len(keys) * 3 / 4, len(keys) - 2, len(keys) - 1} {
			if index < 0 || index >= len(keys) {
				continue
			}
			key := keys[index]
			if _, exists := persisted[key]; exists {
				continue
			}
			item := occurrence{key: key, due: key, state: "open"}
			fields := M{}
			switch i {
			case 0, 5:
				item.due = throughDay // an earlier key can move beyond a later recurrence cutoff
			case 1:
				item.due = ""
			case 2:
				item.deleted = true
				fields["deleted_at"] = "2026-01-01T00:00:00.000Z"
			case 3:
				item.state = "skipped"
			case 4:
				item.state = "done"
			case 6:
				item.due = fromDay // a later key can move before the cutoff, but still gets stopped
			}
			fields["state"] = item.state
			if item.due == "" {
				fields["due_day"] = nil
			} else {
				fields["due_day"] = item.due
			}
			item.id = planningOccurrence(c, task, key, fields)
			persisted[key] = item
		}
		params := P{"planning_task_id": task, "planning_from": fromDay, "planning_through": throughDay}
		check := func(stage, until string, deleted bool) {
			var expected []occurrence
			if !deleted {
				for _, key := range keys {
					if _, exists := persisted[key]; !exists && key >= fromDay && (until == "" || key <= until) {
						expected = append(expected, occurrence{key: key, due: key, state: "open"})
					}
				}
				for _, item := range persisted {
					if item.state == "open" && !item.deleted && item.due >= fromDay && item.due <= throughDay {
						expected = append(expected, item)
					}
				}
			}
			sort.Slice(expected, func(i, j int) bool {
				if expected[i].due != expected[j].due {
					return expected[i].due < expected[j].due
				}
				return expected[i].key < expected[j].key
			})
			var want, got [][]any
			for _, item := range expected {
				var id any
				if item.id != 0 {
					id = item.id
				}
				want = append(want, []any{id, task, item.key, item.due, "inherit", nil})
			}
			for _, row := range c.rows(query, params) {
				got = append(got, row[:6])
			}
			s.K(fmt.Sprintf("deadline model scenario %d %s %s every %d %s", scenario, tc.anchor, tc.unit, tc.every, stage), tab(got) == tab(want), "got", tab(got), "want", tab(want))
			s.K(fmt.Sprintf("deadline model scenario %d %s read never materializes", scenario, stage), c.n("SELECT count(*) FROM task_occurrences WHERE task_id=?", task) == int64(len(persisted)))
		}
		check("initial", "", false)
		until := from.AddDate(0, 0, int(through.Sub(from).Hours()/48)).Format("2006-01-02")
		c.must("UPDATE tasks SET repeat_until_day=? WHERE id=?", until, task)
		for key, item := range persisted {
			if key > until && item.state == "open" {
				item.state = "skipped"
				persisted[key] = item
			}
		}
		check("shortened", until, false)
		c.must("UPDATE tasks SET deleted_at="+NOW+" WHERE id=?", task)
		check("tombstoned", until, true)
		c.must("UPDATE tasks SET deleted_at=NULL WHERE id=?", task)
		check("revived", until, false)
	}
	s.K("deadline model retains file integrity", c.integrityOK())
	c.must("COMMIT")
}

func temporalPeriodStorageModel(s *S) {
	c := temporalModelDB(s, "temporal-period-boundaries.db")
	id := c.identity("period", "ui", "Boundary combinations", nil, "")
	c.must("INSERT INTO periods(id) VALUES(?)", id)
	type boundary struct {
		value                  any
		first, last            string // hand-checked extrema, independent of SQLite date modifiers
		unconstrained, invalid bool
	}
	finite := []boundary{
		{"0000", "0000-01-01", "0000-12-31", false, false},
		{"0000-02", "0000-02-01", "0000-02-29", false, false},
		{"0000-02-29", "0000-02-29", "0000-02-29", false, false},
		{"1900-02", "1900-02-01", "1900-02-28", false, false},
		{"2000-02", "2000-02-01", "2000-02-29", false, false},
		{"2026-10-15", "2026-10-15", "2026-10-15", false, false},
		{"9999", "9999-01-01", "9999-12-31", false, false},
		{"9999-12", "9999-12-01", "9999-12-31", false, false},
		{"9999-12-31", "9999-12-31", "9999-12-31", false, false},
	}
	starts := append(append([]boundary{}, finite...), boundary{nil, "", "", true, false}, boundary{"9999?", "", "", true, false}, boundary{"1900-02-29~", "", "", true, true})
	ends := append(append([]boundary{}, finite...), boundary{nil, "", "", true, false}, boundary{"..", "", "", true, false}, boundary{"0000%", "", "", true, false}, boundary{"9999-13?", "", "", true, true})
	for _, start := range starts {
		for _, end := range ends {
			accepted := !start.invalid && !end.invalid && (start.unconstrained || end.unconstrained || start.first <= end.last)
			c.must("SAVEPOINT period_combination")
			before := c.tab("SELECT p.start_boundary,p.end_boundary,e.revision,e.updated_at FROM periods p JOIN entities e USING(id) WHERE p.id=?", id)
			result := c.tryx("UPDATE periods SET start_boundary=?,end_boundary=? WHERE id=?", start.value, end.value, id)
			unchanged := before == c.tab("SELECT p.start_boundary,p.end_boundary,e.revision,e.updated_at FROM periods p JOIN entities e USING(id) WHERE p.id=?", id)
			stored := c.tab("SELECT start_boundary,end_boundary FROM periods WHERE id=?", id)
			want := tab([][]any{{start.value, end.value}})
			s.K(fmt.Sprintf("period storage model %v through %v", start.value, end.value), (result == "OK") == accepted && (accepted && stored == want || !accepted && unchanged), result, "expected accepted", accepted, "stored", stored, "want", want, "refusal unchanged", unchanged)
			c.must("ROLLBACK TO period_combination")
			c.must("RELEASE period_combination")
		}
	}
	s.K("period storage model retains file integrity", c.integrityOK())
	c.must("COMMIT")
}
