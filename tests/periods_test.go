package tests

import (
	"context"
	"encoding/json"
	"fmt"
	"lifelog/internal/core"
	"lifelog/internal/db"
	"path/filepath"
	"strings"
)

func recordedPeriods(s *S) {
	page := strings.ReplaceAll(s.d.Page("contract/period-boundaries.md"), "\r\n", "\n")
	_, rest, ok := strings.Cut(page, "<!-- period-membership-vectors -->")
	if !ok {
		stop("period vectors missing")
	}
	_, rest, ok = strings.Cut(rest, "```json\n")
	if !ok {
		stop("period vector JSON missing")
	}
	encoded, _, ok := strings.Cut(rest, "```")
	if !ok {
		stop("period vector fence missing")
	}
	var vectors []struct {
		Start, End *string
		Day        string
		AsOf       string `json:"as_of"`
		Result     string
	}
	if err := json.Unmarshal([]byte(encoded), &vectors); err != nil {
		stop("period vector JSON: %v", err)
	}
	for i, v := range vectors {
		c := s.freshWith(F{Path: filepath.Join(s.dir, fmt.Sprintf("period-%d.db", i)), Hardened: true})
		id := c.identity("period", "ui", "Synthetic study", nil, "")
		if outcome := c.tryx("INSERT INTO periods(id,start_boundary,end_boundary) VALUES(?,?,?)", id, v.Start, v.End); outcome != "OK" {
			s.K(fmt.Sprintf("period membership vector %d", i), false, outcome) // a refused boundary is a failed vector, not the end of the suite
			continue
		}
		store := core.Store{DB: &db.DB{R: c.DB}}
		got, err := store.LifePeriods(context.Background(), v.Day, v.AsOf, false)
		s.K(fmt.Sprintf("period membership vector %d", i), err == nil && len(got) == 1 && got[0].Membership == v.Result, got, err)
	}

	_, storage, _ := strings.Cut(page, "<!-- period-storage-vectors -->")
	_, storage, _ = strings.Cut(storage, "```json\n")
	storage, _, _ = strings.Cut(storage, "```")
	var raw []struct {
		Column, Boundary string
		Accepted         bool
	}
	if err := json.Unmarshal([]byte(storage), &raw); err != nil {
		stop("period storage vectors: %v", err)
	}
	for i, v := range raw {
		if v.Column != "start_boundary" && v.Column != "end_boundary" {
			stop("invalid vector column")
		}
		c := s.fresh()
		id := c.identity("period", "ui", "Storage vector", nil, "")
		state := c.tab("SELECT * FROM entities ORDER BY id")
		outcome := c.tryx("INSERT INTO periods(id,"+v.Column+") VALUES(?,?)", id, v.Boundary)
		s.K(fmt.Sprintf("period raw insert vector %d", i), (outcome == "OK") == v.Accepted, outcome)
		if !v.Accepted {
			s.K(fmt.Sprintf("period raw insert unchanged %d", i), state == c.tab("SELECT * FROM entities ORDER BY id"))
			if c.n("SELECT count(*) FROM periods WHERE id=?", id) == 0 {
				c.must("INSERT INTO periods(id) VALUES(?)", id)
			}
		}
		state = c.tab("SELECT * FROM periods ORDER BY id") + c.tab("SELECT * FROM entities ORDER BY id")
		outcome = c.tryx("UPDATE periods SET "+v.Column+"=? WHERE id=?", v.Boundary, id)
		s.K(fmt.Sprintf("period raw update vector %d", i), (outcome == "OK") == v.Accepted, outcome)
		if !v.Accepted {
			s.K(fmt.Sprintf("period raw update unchanged %d", i), state == c.tab("SELECT * FROM periods ORDER BY id")+c.tab("SELECT * FROM entities ORDER BY id"))
		}
	}

	_, categories, _ := strings.Cut(page, "<!-- period-category-vectors -->")
	_, categories, _ = strings.Cut(categories, "```json")
	categories, _, _ = strings.Cut(categories, "```")
	var selections []struct{ Deleted, Accepted bool }
	if err := json.Unmarshal([]byte(categories), &selections); err != nil {
		stop("category vectors: %v", err)
	}
	for i, v := range selections {
		c := s.freshWith(F{Path: filepath.Join(s.dir, fmt.Sprintf("category-%d.db", i)), Hardened: true})
		period := c.identity("period", "ui", "Study", nil, "")
		c.must("INSERT INTO periods(id) VALUES(?)", period)
		category := c.page("Category")
		if v.Deleted {
			c.must("UPDATE entities SET deleted_at="+NOW+" WHERE id=?", category)
		}
		store := core.Store{DB: &db.DB{R: c.DB, W: c.DB}}
		err := store.Link(context.Background(), "cli", period, category, "part-of", "")
		s.K(fmt.Sprintf("period category selection vector %d", i), (err == nil) == v.Accepted, err)
	}
	c := s.fresh()
	person := c.named("person", "Wrong period type")
	c.must("SAVEPOINT period_type_probe")
	outcome := c.tryx("INSERT INTO periods(id,entity_type) VALUES(?,'person')", person)
	s.K("period discriminator refuses another valid owner type", outcome != "OK", outcome)
	c.must("ROLLBACK TO period_type_probe")
	c.must("RELEASE period_type_probe")
	plain := c.page("Not a recorded span")
	c.must("SAVEPOINT period_owner_probe")
	outcome = c.tryx("INSERT INTO periods(id) VALUES(?)", plain)
	s.K("period detail requires typed owner", outcome != "OK", outcome)
	c.must("ROLLBACK TO period_owner_probe")
	c.must("RELEASE period_owner_probe")
	id := c.identity("period", "ui", "Synthetic trip", nil, "")
	c.must("INSERT INTO periods(id) VALUES(?)", id)
	s.K("period detail insertion is part of creation: revision 1 and updated_at = created_at", c.tab("SELECT revision, updated_at = created_at FROM entities WHERE id=?", id) == "1|1")
	// Deliberately prepared owner without detail: a valid composite-FK target
	// makes this a real identity-reassignment witness, not an error-prose test.
	target := c.identity("period", "ui", "Alternate recorded trip", nil, "")
	for _, alias := range []string{"id", "rowid", "_rowid_", "oid"} {
		state := c.tab("SELECT id,revision FROM entities ORDER BY id") + c.tab("SELECT * FROM periods ORDER BY id")
		c.must("SAVEPOINT immutable_period_probe")
		outcome := c.tryx("UPDATE periods SET "+alias+"=?,start_boundary='2018' WHERE id=?", target, id)
		unchanged := state == c.tab("SELECT id,revision FROM entities ORDER BY id")+c.tab("SELECT * FROM periods ORDER BY id")
		c.must("ROLLBACK TO immutable_period_probe")
		c.must("RELEASE immutable_period_probe")
		s.K("period "+alias+" immutable", outcome != "OK" && unchanged, outcome)
	}
	s.K("period cannot be deleted", strings.Contains(c.tryx("DELETE FROM periods WHERE id=?", id), "tombstoned"))
	s.K("period refuses invalid start", strings.Contains(c.tryx("UPDATE periods SET start_boundary='2019-02-29?' WHERE id=?", id), "periods_start_boundary"))
	s.K("period refuses invalid end", strings.Contains(c.tryx("UPDATE periods SET end_boundary='2019-13' WHERE id=?", id), "periods_end_boundary"))
	s.K("period refuses reversed span", strings.Contains(c.tryx("UPDATE periods SET start_boundary='2019-10',end_boundary='2019-09' WHERE id=?", id), "periods_order"))
	before := c.one("SELECT revision FROM entities WHERE id=?", id)
	c.must("UPDATE periods SET start_boundary='2018' WHERE id=?", id)
	after := c.one("SELECT revision FROM entities WHERE id=?", id)
	s.K("period boundary edit advances revision", after.(int64) > before.(int64))
	c.must("UPDATE periods SET start_boundary='2018' WHERE id=?", id)
	s.K("period boundary no-op preserves revision", c.one("SELECT revision FROM entities WHERE id=?", id) == after)
	periodBoundaryGrammar(s)
}

// boundaryVectors is the grammar of contract/period-boundaries.md written out, one raw string at a time, with what
// each column must say: YYYY, YYYY-MM or YYYY-MM-DD in ASCII, a valid calendar value, at most one trailing ?, ~ or %,
// and ".." only as an end. The answers are hand-checked against that grammar, not computed by the CHECKs: they were
// recorded against the long CASE form of periods_start_boundary and periods_end_boundary first and are the
// regression for the compact rtrim form that replaced it.
var boundaryVectors = []struct {
	boundary   string
	start, end bool // accepted as start_boundary, as end_boundary
}{
	// accepted: the three precisions, each qualifier, the calendar's edges
	{"2020", true, true}, {"2020?", true, true}, {"2020~", true, true}, {"2020%", true, true},
	{"0000", true, true}, {"9999", true, true}, {"0000-01-01", true, true}, {"9999-12-31", true, true},
	{"2020-01", true, true}, {"2020-12", true, true}, {"2020-06~", true, true}, {"2020-12?", true, true},
	{"2020-12-31", true, true}, {"2020-12-31~", true, true}, {"2020-12-31%", true, true}, {"2020-12-31?", true, true},
	{"2020-02-29", true, true}, {"2020-02-29?", true, true}, {"2000-02-29", true, true}, {"2019-02-28%", true, true},
	{"2020-04-30", true, true}, {"2020-01-31", true, true},
	// refused: a nominal value that is not a calendar value, qualified or not
	{"2020-13", false, false}, {"2020-00", false, false}, {"2020-13?", false, false}, {"2020-00~", false, false},
	{"2020-02-30", false, false}, {"2019-02-29", false, false}, {"2019-02-29?", false, false},
	{"1900-02-29", false, false}, {"2100-02-29%", false, false}, {"2020-04-31", false, false},
	{"2020-00-10", false, false}, {"2020-01-00", false, false}, {"2020-01-32", false, false}, {"2020-13-01", false, false},
	// refused: not exactly four, seven or ten ASCII characters of the grammar
	{"", false, false}, {"2020-", false, false}, {"-2020", false, false}, {"2020-1", false, false}, {"202", false, false},
	{"20201", false, false}, {"2020-1-01", false, false}, {"2020-01-1", false, false}, {"2020-12-31 ", false, false},
	{" 2020", false, false}, {"2020 ", false, false}, {"2020 ?", false, false}, {"2020-12-31T", false, false},
	{"2020/01/01", false, false}, {"20200101", false, false}, {"2020-W01", false, false}, {"2020-12-311", false, false},
	{"12020", false, false}, {"+2020", false, false}, {"-0001", false, false}, {"２０２０", false, false}, {"2020-１２", false, false},
	{"2020é", false, false}, {"abcd", false, false}, {"20x0", false, false}, {"2020x12", false, false}, {"2020/12", false, false},
	{"2020-1x", false, false}, {"2020-12-3x", false, false},
	// refused: a qualifier is one character, last, and nothing else
	{"?", false, false}, {"~", false, false}, {"%", false, false}, {"??", false, false}, {"2020??", false, false},
	{"2020?~", false, false}, {"2020~?", false, false}, {"2020%%", false, false}, {"2020-12-31%%", false, false},
	{"2020-12-31?~", false, false}, {"?2020", false, false}, {"20?20", false, false}, {"2020-?1", false, false},
	{"2020?-01", false, false}, {"2020-01?-01", false, false}, {"2020-12*", false, false}, {"2020!", false, false},
	// ".." is ongoing, an end only, and unqualified
	{"..", false, true}, {"..?", false, false}, {"..~", false, false}, {"...", false, false}, {".", false, false},
	{"2020..", false, false}, {"..2020", false, false}, {".. ", false, false},
	// a NUL is not canonical ASCII, wherever it is. GLOB, length() and date() stop reading at it, so "2018\x00hidden"
	// looks like the year 2018 to them: only the NUL guard refuses it ("2018?\x00hidden" the shape rule refuses too)
	{"\x00", false, false}, {"2018\x00", false, false}, {"2018\x00hidden", false, false}, {"2018?\x00hidden", false, false},
	{"2018-09\x00", false, false}, {"2018-09\x00hidden", false, false}, {"2018-09-15\x00x", false, false},
	{"2018-09-15%\x00hidden", false, false}, {"\x002018", false, false}, {"2018\x00?", false, false}, {"..\x00hidden", false, false},
}

// periodBoundaryGrammar runs every vector through both boundary columns, on insert and on update, and checks that a
// refused value leaves nothing behind. NULL is unknown and is accepted by both.
func periodBoundaryGrammar(s *S) {
	c := s.fresh()
	for _, col := range []string{"start_boundary", "end_boundary"} {
		for _, v := range boundaryVectors {
			want := v.start
			if col == "end_boundary" {
				want = v.end
			}
			c.must("SAVEPOINT boundary_probe")
			id := c.identity("period", "ui", "Boundary grammar", nil, "")
			outcome := c.tryx("INSERT INTO periods(id,"+col+") VALUES(?,?)", id, v.boundary)
			s.K(fmt.Sprintf("%s insert %q accepted=%v", col, v.boundary, want), (outcome == "OK") == want, outcome)
			c.must("ROLLBACK TO boundary_probe")
			c.must("RELEASE boundary_probe")
		}
	}
	c.must("SAVEPOINT boundary_probe")
	id := c.identity("period", "ui", "Boundary grammar update", nil, "")
	c.must("INSERT INTO periods(id) VALUES(?)", id)
	for _, col := range []string{"start_boundary", "end_boundary"} {
		for _, v := range boundaryVectors {
			want := v.start
			if col == "end_boundary" {
				want = v.end
			}
			outcome := c.tryx("UPDATE periods SET "+col+"=? WHERE id=?", v.boundary, id)
			s.K(fmt.Sprintf("%s update %q accepted=%v", col, v.boundary, want), (outcome == "OK") == want, outcome)
			c.must("UPDATE periods SET " + col + "=NULL")
		}
	}
	c.must("ROLLBACK TO boundary_probe")
	c.must("RELEASE boundary_probe")
	for _, col := range []string{"start_boundary", "end_boundary"} {
		c.must("SAVEPOINT boundary_probe")
		id := c.identity("period", "ui", "Boundary unknown", nil, "")
		s.K(col+" NULL is unknown and accepted", c.tryx("INSERT INTO periods(id,"+col+") VALUES(?,NULL)", id) == "OK")
		c.must("ROLLBACK TO boundary_probe")
		c.must("RELEASE boundary_probe")
	}
}
