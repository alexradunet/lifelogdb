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
	rows, err := s.DB.W.QueryContext(t.Context(), `EXPLAIN QUERY PLAN
		SELECT m.id, m_name.title, EXISTS (SELECT 1 FROM habit_periods h WHERE h.metric_id = m.id)
		FROM entities m JOIN entity_names m_name
		  ON m_name.entity_id = m.id AND m_name.name_key = m.preferred_name_key AND m.deleted_at IS NULL
		WHERE m.id = (SELECT entity_id FROM entity_names WHERE name_key = ?) AND m.entity_type = 'metric'`, "mood")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	searched := map[string]bool{"m": false, "m_name": false}
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
	for _, alias := range []string{"m", "m_name"} {
		if !searched[alias] {
			t.Errorf("reading lookup does not search %s by an index: %v", alias, plan)
		}
	}
}
