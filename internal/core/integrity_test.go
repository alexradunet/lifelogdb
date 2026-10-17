package core

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"reflect"
	"slices"
	"strings"
	"testing"

	"lifelog/internal/db"
)

// A local driver exercises database/sql's actual Scan, EOF and automatic Close paths.
// No SQLite corruption is implied by these execution-failure fixtures.
type integrityConnector struct {
	check, fault           string
	closed, queries, execs int
}

func (c *integrityConnector) Connect(context.Context) (driver.Conn, error) {
	return &integrityConn{c}, nil
}
func (c *integrityConnector) Driver() driver.Driver { return integrityDriver{} }

type integrityDriver struct{}

func (integrityDriver) Open(string) (driver.Conn, error) { return nil, errors.New("use connector") }

type integrityConn struct{ c *integrityConnector }

func (*integrityConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (*integrityConn) Begin() (driver.Tx, error) { return integrityTx{}, nil }
func (*integrityConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return integrityTx{}, nil
}

type integrityTx struct{}

func (integrityTx) Commit() error   { return nil }
func (integrityTx) Rollback() error { return nil }
func (c *integrityConn) Close() error {
	if c.c.fault == "conn-close" {
		return errIntegrityFault
	}
	return nil
}

var errIntegrityFault = errors.New("synthetic integrity execution failure")

func (c *integrityConn) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	c.c.queries++
	check := "orphan entities"
	if strings.Contains(query, "PRAGMA integrity_check") {
		check = "integrity_check"
	} else if strings.Contains(query, "PRAGMA foreign_key_check") {
		check = "foreign_key_check"
	} else if strings.Contains(query, "WITH RECURSIVE rooted") {
		check = "measurement correction chains"
	} else if strings.Contains(query, "SELECT n.id FROM entity_names") {
		check = "journal name ownership"
	} else if strings.Contains(query, "JOIN link_kinds") {
		check = "typed link endpoints"
	} else if strings.Contains(query, "SELECT l.id FROM links") {
		check = "link day-page rules"
	} else if strings.Contains(query, "JOIN metrics x") {
		check = "scale ranges"
	} else if strings.Contains(query, "SELECT m.id FROM measurements") {
		check = "measurement corrections"
	} else if strings.Contains(query, "FROM entity_names ORDER BY id") {
		check = "name keys"
	} else if strings.Contains(query, "FROM sqlite_schema") {
		check = "schema drift"
	}
	fault := ""
	if check == c.c.check {
		fault = c.c.fault
	}
	if fault == "query" {
		return nil, errIntegrityFault
	}
	return &integrityRows{c: c.c, check: check, fault: fault}, nil
}
func (c *integrityConn) ExecContext(context.Context, string, []driver.NamedValue) (driver.Result, error) {
	c.c.execs++
	if c.c.fault == "fts" {
		return nil, errIntegrityFault
	}
	return driver.RowsAffected(0), nil
}

type integrityRows struct {
	c            *integrityConnector
	check, fault string
	index        int
}

func (r *integrityRows) Columns() []string {
	if r.check == "foreign_key_check" {
		return []string{"table", "rowid", "parent", "fkid"}
	}
	if r.check == "name keys" {
		return []string{"id", "title", "name_key"}
	}
	if r.check == "schema drift" {
		return []string{"type", "name", "tbl_name", "sql"}
	}
	return []string{"value"}
}
func (r *integrityRows) Close() error {
	r.c.closed++
	if r.fault == "close" || r.fault == "scan-close" {
		return errIntegrityFault
	}
	return nil
}
func (r *integrityRows) Next(dest []driver.Value) error {
	count := 0
	if r.check == "integrity_check" || r.fault != "" {
		count = 1
	}
	if r.fault == "scan" || r.fault == "scan-close" {
		count = 2
	}
	if r.index >= count {
		if r.fault == "late" {
			return errIntegrityFault
		}
		return io.EOF
	}
	if r.check == "foreign_key_check" {
		copy(dest, []driver.Value{"measurements", int64(7), "metrics", int64(0)})
	} else if r.check == "name keys" {
		copy(dest, []driver.Value{int64(42), "Title", "not the key of Title"})
	} else if r.check == "schema drift" {
		copy(dest, []driver.Value{"table", "stray", "stray", "CREATE TABLE stray(a)"})
	} else if r.check == "integrity_check" {
		dest[0] = "ok"
	} else {
		dest[0] = int64(42)
	}
	if r.fault == "diagnostic" && r.check == "integrity_check" {
		dest[0] = "synthetic structural diagnostic"
	}
	if r.fault == "diagnostic" && r.check == "foreign_key_check" {
		dest[1] = nil
	}
	if r.index == 1 {
		if r.check == "integrity_check" {
			dest[0] = nil
		} else if r.check == "foreign_key_check" {
			dest[1] = "not an integer"
		} else if r.check == "name keys" {
			dest[0] = "not an integer"
		} else if r.check == "schema drift" {
			dest[0] = nil
		} else {
			dest[0] = "not an integer"
		}
	}
	r.index++
	return nil
}
func faultIntegrityStore(t *testing.T, c *integrityConnector) *Store {
	t.Helper()
	d := sql.OpenDB(c)
	d.SetMaxOpenConns(1)
	t.Cleanup(func() { d.Close() })
	return &Store{DB: &db.DB{R: d, W: d}}
}

// integrityCheckNames are the reads of Integrity in the order it runs them.
var integrityCheckNames = []string{"integrity_check", "foreign_key_check", "orphan entities", "typed link endpoints", "measurement corrections",
	"measurement correction chains", "journal name ownership", "link day-page rules", "scale ranges", "name keys", "schema drift"}

func TestIntegrityReadFailures(t *testing.T) {
	for i, check := range integrityCheckNames {
		for _, fault := range []string{"query", "late", "scan", "close", "scan-close"} {
			t.Run(check+"/"+fault, func(t *testing.T) {
				c := &integrityConnector{check: check, fault: fault}
				result, err := faultIntegrityStore(t, c).Integrity(context.Background())
				if err == nil || !strings.Contains(err.Error(), check) {
					t.Errorf("result=%+v error=%v; want identified execution failure", result, err)
				}
				if fault != "scan" && !errors.Is(err, errIntegrityFault) {
					t.Errorf("lost fault: %v", err)
				}
				wantClosed := i + 1
				if fault == "query" {
					wantClosed--
				}
				if c.closed != wantClosed || c.queries != i+1 || c.execs != 0 {
					t.Errorf("closed=%d queries=%d execs=%d; want %d/%d/0", c.closed, c.queries, c.execs, wantClosed, i+1)
				}
			})
		}
	}
}
func TestIntegrityResultSemantics(t *testing.T) {
	for _, fault := range []string{"", "fts"} {
		t.Run("clean-read-streams/"+fault, func(t *testing.T) {
			c := &integrityConnector{fault: fault}
			r, err := faultIntegrityStore(t, c).Integrity(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if r.OK != (fault == "") || r.FullTextIndexOK != (fault == "") || (r.FullTextError != "") != (fault == "fts") || len(r.IntegrityCheck) != 1 || r.IntegrityCheck[0] != "ok" || r.ForeignKeys != 0 || len(r.OrphanEntities) != 0 {
				t.Errorf("unexpected result: %+v", r)
			}
			if c.closed != len(integrityCheckNames) || c.execs != 1 {
				t.Errorf("closed=%d execs=%d", c.closed, c.execs)
			}
		})
	}
}

func TestIntegrityDiagnostics(t *testing.T) {
	for _, check := range integrityCheckNames {
		t.Run(check, func(t *testing.T) {
			c := &integrityConnector{check: check, fault: "diagnostic"}
			r, err := faultIntegrityStore(t, c).Integrity(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if r.OK == (check != "schema drift") || !r.FullTextIndexOK { // a drift is outside ok
				t.Errorf("diagnostic semantics: %+v", r)
			}
			switch check {
			case "integrity_check":
				if len(r.IntegrityCheck) != 1 || r.IntegrityCheck[0] != "synthetic structural diagnostic" {
					t.Errorf("diagnostic lost: %+v", r)
				}
			case "foreign_key_check":
				if r.ForeignKeys != 1 {
					t.Errorf("foreign key diagnostic lost: %+v", r)
				}
			case "typed link endpoints":
				if len(r.InvalidTypedLinks) != 1 || r.InvalidTypedLinks[0] != 42 {
					t.Errorf("typed-link diagnostic lost: %+v", r)
				}
			case "measurement corrections":
				if len(r.InvalidMeasurementCorrections) != 1 || r.InvalidMeasurementCorrections[0] != 42 {
					t.Errorf("correction diagnostic lost: %+v", r)
				}
			case "measurement correction chains":
				if len(r.InvalidMeasurementChains) != 1 || r.InvalidMeasurementChains[0] != 42 {
					t.Errorf("semantic diagnostic lost: %+v", r)
				}
			case "journal name ownership":
				if len(r.InvalidJournalNames) != 1 || r.InvalidJournalNames[0] != 42 {
					t.Errorf("semantic diagnostic lost: %+v", r)
				}
			case "orphan entities":
				if len(r.OrphanEntities) != 1 || r.OrphanEntities[0] != 42 {
					t.Errorf("orphan diagnostic lost: %+v", r)
				}
			case "link day-page rules":
				if len(r.InvalidLinkDayRules) != 1 || r.InvalidLinkDayRules[0] != 42 {
					t.Errorf("link day-page diagnostic lost: %+v", r)
				}
			case "scale ranges":
				if len(r.InvalidScaleValues) != 1 || r.InvalidScaleValues[0] != 42 {
					t.Errorf("scale diagnostic lost: %+v", r)
				}
			case "schema drift":
				if !slices.Contains(r.SchemaDrift, "extra table stray") {
					t.Errorf("schema diagnostic lost: %+v", r)
				}
			case "name keys":
				if len(r.NameKeyMismatches) != 1 || r.NameKeyMismatches[0] != 42 {
					t.Errorf("name key diagnostic lost: %+v", r)
				}
			}
			if c.closed != len(integrityCheckNames) || c.execs != 1 {
				t.Errorf("closed=%d execs=%d", c.closed, c.execs)
			}
		})
	}
}

func TestIntegrityDetectsDeliberateTypedEdgeDamage(t *testing.T) {
	s := fresh(t)
	category, _, err := s.CreatePage(ctx, "cli", "Damaged category", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateMetric(ctx, "cli", NewMetric{Name: "Weight", Unit: "kg"}); err != nil {
		t.Fatal(err)
	}
	metric, err := s.PageID(ctx, "Weight")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Link(ctx, "cli", metric, category, "part-of", ""); err != nil {
		t.Fatal(err)
	}
	var edge int64
	if err := s.DB.R.QueryRow(`SELECT id FROM links WHERE from_id=? AND to_id=? AND kind='part-of'`, metric, category).Scan(&edge); err != nil {
		t.Fatal(err)
	}
	clean, err := s.Integrity(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !clean.OK {
		t.Fatalf("clean setup: %+v", clean)
	}
	// Deliberate damage bypasses only the guard in this disposable fixture.
	if _, err := s.DB.W.Exec(`DROP TRIGGER entities_endpoint_types; UPDATE entities SET entity_type='place' WHERE id=?`, category); err != nil {
		t.Fatal(err)
	}
	damaged, err := s.Integrity(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if damaged.OK || damaged.ForeignKeys != 0 || len(damaged.OrphanEntities) != 0 || !damaged.FullTextIndexOK || len(damaged.InvalidTypedLinks) != 1 || damaged.InvalidTypedLinks[0] != edge {
		t.Fatalf("damage diagnostics: %+v", damaged)
	}
}

func TestIntegrityDetectsDeliberatePreferredOwnerDamage(t *testing.T) {
	s := fresh(t)
	id, _, err := s.CreatePage(ctx, "cli", "Owner", "prose", "")
	if err != nil {
		t.Fatal(err)
	}
	// Deliberate external damage only: same pooled connection, FK disabled for this write.
	if _, err := s.DB.W.Exec(`PRAGMA foreign_keys=OFF; UPDATE entities SET preferred_name_key='mood' WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.W.Exec(`PRAGMA foreign_keys=ON`); err != nil {
		t.Fatal(err)
	}
	r, err := s.Integrity(ctx)
	if err != nil || r.OK || !reflect.DeepEqual(r.OrphanEntities, []int64{id}) {
		t.Fatalf("independent preferred-owner diagnostic: %+v %v", r, err)
	}
}

func TestIntegrityDetectsRestoredGuardSemanticDamage(t *testing.T) {
	for _, damage := range []string{"correction cycle", "journal alias", "journal owner"} {
		t.Run(damage, func(t *testing.T) {
			s := fresh(t)
			exec := func(q string, args ...any) {
				t.Helper()
				if _, err := s.DB.W.ExecContext(ctx, q, args...); err != nil {
					t.Fatal(err)
				}
			}
			bypass := func(trigger string, change func()) {
				t.Helper()
				var definition string
				if err := s.DB.R.QueryRowContext(ctx, "SELECT sql FROM sqlite_schema WHERE type='trigger' AND name=?", trigger).Scan(&definition); err != nil {
					t.Fatal(err)
				}
				exec("DROP TRIGGER " + trigger)
				change()
				exec(definition)
			}
			page := func(name string) int64 {
				t.Helper()
				id, _, err := s.CreatePage(ctx, "cli", name, "", "")
				if err != nil {
					t.Fatal(err)
				}
				return id
			}
			switch damage {
			case "correction cycle":
				root, err := s.Record(ctx, "cli", Reading{Metric: "Mood", Day: "2026-10-01", Value: 3})
				if err != nil {
					t.Fatal(err)
				}
				value := 4.0
				child, err := s.Correct(ctx, "cli", root, &value)
				if err != nil {
					t.Fatal(err)
				}
				bypass("measurements_no_update", func() { exec("UPDATE measurements SET supersedes_id=? WHERE id=?", child, root) })
			case "journal alias":
				id := page("2026-10-01")
				bypass("entity_names_day_insert", func() { exec("INSERT INTO entity_names(entity_id,title,name_key) VALUES(?,'Extra','extra')", id) })
			case "journal owner":
				id := page("Ordinary")
				bypass("entity_names_day_insert", func() {
					exec("INSERT INTO entity_names(entity_id,title,name_key) VALUES(?,'2026-10-01','2026-10-01')", id)
				})
			}
			got, err := s.Integrity(ctx)
			if err != nil || got.OK || got.ForeignKeys != 0 || !got.FullTextIndexOK || len(got.IntegrityCheck) != 1 || got.IntegrityCheck[0] != "ok" {
				t.Fatalf("semantic damage with its guards restored must be reported independently of structural checks: %+v, %v", got, err)
			}
			var diagnostic []int64
			want := 1
			switch damage {
			case "correction cycle":
				diagnostic, want = got.InvalidMeasurementChains, 2
			case "journal alias", "journal owner":
				diagnostic = got.InvalidJournalNames
			}
			if len(diagnostic) != want {
				t.Fatalf("%s diagnostic IDs=%v; want %d", damage, diagnostic, want)
			}
		})
	}
}

// The measurement-corrections check finds a correction of another metric even when the trigger that refuses it is
// gone: the semantic query does not trust the triggers of the file it checks.
func TestIntegrityDetectsACorrectionOfAnotherMetric(t *testing.T) {
	s := fresh(t)
	for _, name := range []string{"Steps", "Floors"} {
		if _, err := s.CreateMetric(ctx, "cli", NewMetric{Name: name, Unit: "count"}); err != nil {
			t.Fatal(err)
		}
	}
	root, err := s.Record(ctx, "cli", Reading{Metric: "Steps", Day: "2020-01-02", Value: 4000})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.W.Exec("DROP TRIGGER measurements_supersede_metric"); err != nil {
		t.Fatal(err)
	}
	var damaged int64
	if err := s.DB.W.QueryRow(`INSERT INTO measurements(metric_id,day,value,supersedes_id,source,created_at) SELECT (SELECT entity_id FROM entity_names WHERE name_key='floors'),'2020-01-02',3,?,'ui',`+nowSQL+` RETURNING id`, root).Scan(&damaged); err != nil {
		t.Fatal(err)
	}
	result, err := s.Integrity(ctx)
	if err != nil || result.OK || result.ForeignKeys != 0 || !result.FullTextIndexOK || len(result.InvalidMeasurementCorrections) != 1 || result.InvalidMeasurementCorrections[0] != damaged {
		t.Fatalf("correction damage: %+v %v", result, err)
	}
}
