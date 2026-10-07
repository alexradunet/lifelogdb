package tests

import (
	"fmt"
	"testing"
)

// The suites, grouped by subject (tests/README.md). Each states its expectation in every label.
var suites = []struct {
	name string
	run  func(*S)
}{
	{"dates", dates},
	{"recorded-periods", recordedPeriods},
	{"recorded-sessions", recordedSessions},
	{"measurement-scopes", measurementScopes},
	{"planning", planning},
	{"planning-boundaries", planningBoundaries},
	{"planning-cookbook", planningCookbook},
	{"identity", identity},
	{"id-guards", idGuards},
	{"schema-safeguards", schemaSafeguards},
	{"schema-adversarial", schemaAdversarial},
	{"named", named},
	{"names", names},
	{"name-grammar", nameGrammar},
	{"name-ownership", nameOwnership},
	{"files", files},
	{"places", places},
	{"pages", pages},
	{"renames", renames},
	{"links", links},
	{"graph-model", graphModel},
	{"facts", facts},
	{"measurement-query-plan", measurementQueryPlan},
	{"habits", habits},
	{"journal", journal},
	{"writers", writers},
	{"storage-resilience", storageResilience},
	{"integrity", integrity},
	{"physical-integrity", physicalIntegrity},
	{"imports", imports},
	{"snapshots", snapshots},
	{"evolution", evolution},
	{"cookbook", cookbook},
	{"temporal-model", temporalCookbookModels},
	{"document", document},
	{"diagrams", diagrams},
	{"title-fuzz", titleFuzz},
	{"save-contract", saveContract},
	{"doc-save-contract", docSaveContract},
}

func suiteFunc(name string) func(*S) {
	for _, s := range suites {
		if s.name == name {
			return s.run
		}
	}
	panic("no suite " + name)
}

// runSuite runs one suite on a docs tree. A suite that stops (a broken document made a later step impossible)
// reports that as one failed expectation; stopped says why.
func runSuite(name string, d *Docs, dir string) (s *S, stopped string) {
	s = &S{name: name, d: d, ddl: d.DDL(), dir: dir}
	defer s.close()
	func() {
		defer func() {
			if r := recover(); r != nil {
				stopped = fmt.Sprint(r)
				s.K("the suite stopped: "+clip(stopped, 300), false)
			}
		}()
		suiteFunc(name)(s)
	}()
	return
}

// TestSuites runs every suite against docs/ as it is: every expectation must be met.
func TestSuites(t *testing.T) {
	for _, su := range suites {
		t.Run(su.name, func(t *testing.T) {
			t.Parallel()
			s, _ := runSuite(su.name, realDocs(), t.TempDir())
			for _, f := range s.fails {
				t.Error("FAIL " + f)
			}
			if s.n == 0 {
				t.Error("the suite recorded no expectation")
			}
			t.Logf("%s: %d/%d met expectations", su.name, s.ok, s.n)
		})
	}
}
