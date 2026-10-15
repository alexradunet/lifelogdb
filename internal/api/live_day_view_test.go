package api_test

import (
	"fmt"
	"strings"
	"testing"
)

func TestLiveDayView(t *testing.T) {
	c, _ := fresh(t)
	root := must(c.Get(t.Context(), "/"))
	must(c.Do(t.Context(), find(root, "register-metric"), map[string]string{"name": "Walk"}))
	must(c.Do(t.Context(), find(root, "record"), map[string]string{"metric": "Walk", "day": "2026-09-01", "value": "1"}))
	page := must(c.Get(t.Context(), "/pages?title=Walk"))
	for i, hidden := range []bool{false, true, false} {
		if hidden {
			page = must(c.Do(t.Context(), find(page, "tombstone"), nil))
		} else if i > 0 {
			page = must(c.Do(t.Context(), find(page, "revive"), nil))
		}
		day := must(c.Get(t.Context(), "/days/2026-09-01"))
		if hasOption(find(day, "record"), "metric", "Walk") == hidden {
			t.Errorf("record options hidden=%v: %+v", hidden, day.Actions)
		}
		props := day.Properties.(map[string]any)
		listed := false
		for _, v := range props["view"].([]any) {
			row := v.(map[string]any)
			listed = listed || row["what"] == "Walk" && strings.HasPrefix(fmt.Sprint(row["detail"]), "1")
		}
		if listed == hidden {
			t.Errorf("reading visibility hidden=%v: %v", hidden, props["view"])
		}
		if len(props["readings"].([]any)) != 1 {
			t.Error("historical reading lost")
		}
	}
}
