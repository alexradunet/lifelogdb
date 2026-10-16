package core

import (
	"fmt"
	"strings"
	"testing"
)

func TestNewDatabaseReadingLookupUsesIndexes(t *testing.T) {
	s := fresh(t)
	if err := s.Do(t.Context(), "cli", func(tx *Tx) error {
		for i := range 200 {
			if _, _, _, err := tx.CreatePage(fmt.Sprintf("Synthetic lookup page %03d", i), "", "2031-02-03", ""); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// Keep the writer open: closing it optimizes the grown database and would
	// conceal initialization statistics that still describe only the seeded Mood.
	// This is prepareReading's lookup, planned on that same writer connection.
	rows, err := s.DB.W.QueryContext(t.Context(), `EXPLAIN QUERY PLAN `+metricLookup, "mood")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	searched := map[string]bool{"m": false, "entity_names": false}
	var plan []string
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		plan = append(plan, detail)
		fields := strings.Fields(detail)
		if len(fields) >= 2 && fields[0] == "SEARCH" {
			if _, relevant := searched[fields[1]]; relevant {
				searched[fields[1]] = true
			}
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	for _, alias := range []string{"m", "entity_names"} {
		if !searched[alias] {
			t.Errorf("reading lookup does not search %s by an index: %v", alias, plan)
		}
	}
}
