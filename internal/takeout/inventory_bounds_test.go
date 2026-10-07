package takeout

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestInventoryApplicationBoundsAndCancellation(t *testing.T) {
	cases := []struct{ name, path, body string }{
		{"depth", "Google Health/Sleep/private.json", strings.Repeat("[", 65) + "0" + strings.Repeat("]", 65)},
		{"bytes", "Google Health/Sleep/private.json", strings.Repeat(" ", inventoryBytes+1)},
		{"columns", "Google Health/Weight/private.csv", strings.Repeat("column,", inventoryColumns) + "last\n1\n"},
		{"header", "Google Health/Weight/private.csv", strings.Repeat("x", 257) + "\n1\n"},
		{"records", "Google Health/Weight/private.csv", "timestamp\n" + strings.Repeat("2020-01-01\n", inventoryRecords+1)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeFile(t, root, tc.path, tc.body)
			if _, err := Inventory(root); err == nil {
				t.Fatal("application bound accepted")
			} else if strings.Contains(err.Error(), "private") {
				t.Fatal("private path leaked")
			}
		})
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := InventoryContext(canceled, t.TempDir()); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation %v", err)
	}
}
