package core

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
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
func (*integrityConn) Begin() (driver.Tx, error) { return nil, errors.New("unexpected transaction") }
func (*integrityConn) Close() error              { return nil }

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
	} else if strings.Contains(query, "SELECT l.id FROM links") {
		check = "typed link endpoints"
	} else if strings.Contains(query, "SELECT m.id FROM measurements") {
		check = "measurement corrections"
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
func TestIntegrityReadFailures(t *testing.T) {
	for i, check := range []string{"integrity_check", "foreign_key_check", "orphan entities", "typed link endpoints", "measurement corrections", "measurement correction chains", "journal name ownership"} {
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
			if c.closed != 7 || c.execs != 1 {
				t.Errorf("closed=%d execs=%d", c.closed, c.execs)
			}
		})
	}
}

func TestIntegrityDiagnostics(t *testing.T) {
	for _, check := range []string{"integrity_check", "foreign_key_check", "orphan entities", "typed link endpoints", "measurement corrections", "measurement correction chains", "journal name ownership"} {
		t.Run(check, func(t *testing.T) {
			c := &integrityConnector{check: check, fault: "diagnostic"}
			r, err := faultIntegrityStore(t, c).Integrity(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if r.OK || !r.FullTextIndexOK {
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
			}
			if c.closed != 7 || c.execs != 1 {
				t.Errorf("closed=%d execs=%d", c.closed, c.execs)
			}
		})
	}
}
