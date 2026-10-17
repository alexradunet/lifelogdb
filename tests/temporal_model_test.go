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
	temporalCategoryLifecycle(s)
	temporalCorrectionModel(s)
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

// This model owns chronological insertion order and chain membership as plain
// slices. It does not use the SQL leaf predicate, recursive CTE, ID order, or
// recording timestamp order to determine the latest eligible fact in a chain.
func temporalCorrectionModel(s *S) {
	const seed int64 = 713_029
	rng := rand.New(rand.NewSource(seed))
	c := temporalModelDB(s, "temporal-corrections.db")
	weight, other := c.metric("weight", "kg"), c.metric("Other metric", "kg")
	ids := rng.Perm(1000)
	used := 0
	type fact struct {
		id, metric int64
		day, at    string
		value      any
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
		for depth := range length {
			id := int64(ids[used] + 10)
			used++
			var value any = float64(chain*20+depth) + 0.25
			if depth > 0 && rng.Intn(3) == 0 {
				value = nil
			}
			day := fmt.Sprintf("2026-10-%02d", chain%28+1)
			at := fmt.Sprintf("2026-11-01T%02d:00:00.000Z", rng.Intn(16))
			c.must("INSERT INTO measurements(id,metric_id,day,value,source,created_at,supersedes_id) VALUES(?,?,?,?,'ui',?,?)", id, metric, day, value, at, prior)
			members = append(members, fact{id, metric, day, at, value})
			prior = id
		}
		chains = append(chains, members)
	}
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
			wantRows = append(wantRows, []any{f.id, f.day, f.value, "2026-11-03T00:00:00.000Z"})
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
