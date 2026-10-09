package importer

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestFactsJSONBoundary(t *testing.T) {
	const file = "Journal/2031-04-11.md"
	invalid := []struct{ name, data string }{
		{"closing brace", `{"file":"Journal/2031-04-11.md","writes":[]}}`},
		{"closing bracket", `{"file":"Journal/2031-04-11.md","writes":[]}]`},
		{"second object", `{"file":"Journal/2031-04-11.md","writes":[]} {}`},
		{"scalar", `{"file":"Journal/2031-04-11.md","writes":[]} 42`},
		{"garbage", `{"file":"Journal/2031-04-11.md","writes":[]} xyz`},
		{"incomplete", `{"file":`},
		{"array", `[]`},
		{"null", `null`},
		{"unknown field", `{"file":"Journal/2031-04-11.md","writes":[],"extra":true}`},
		{"absent writes", `{"file":"Journal/2031-04-11.md"}`},
		{"null writes", `{"file":"Journal/2031-04-11.md","writes":null}`},
	}
	for _, tc := range invalid {
		t.Run(tc.name, func(t *testing.T) {
			f := setup(t)
			f.approveRules(t, rulesBody)
			f.decideNames(t, dayNames...)
			if _, err := f.w.MakeLedger(); err != nil {
				t.Fatal(err)
			}
			f.metrics(t)
			if err := f.facts(t, file, dayFacts); err != nil {
				t.Fatal(err)
			}
			if _, err := f.w.Apply(ctx, f.s, file); err != nil {
				t.Fatal(err)
			}
			p, err := f.w.factsPath(file)
			if err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			ledger, _, err := f.w.Ledger()
			if err != nil {
				t.Fatal(err)
			}
			counts, err := f.s.Counts(ctx)
			if err != nil {
				t.Fatal(err)
			}
			entries, err := os.ReadDir(filepath.Dir(p))
			if err != nil {
				t.Fatal(err)
			}
			if err := f.w.WriteFacts(file, []byte(tc.data)); err == nil {
				t.Error("malformed replacement accepted")
			}
			after, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, after) {
				t.Error("replay evidence changed")
			}
			gotLedger, _, err := f.w.Ledger()
			if err != nil {
				t.Fatal(err)
			}
			gotCounts, err := f.s.Counts(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(ledger, gotLedger) || !reflect.DeepEqual(counts, gotCounts) {
				t.Error("ledger/database state changed")
			}
			gotEntries, err := os.ReadDir(filepath.Dir(p))
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != len(gotEntries) {
				t.Error("stray output after refusal")
			}
			if err := os.WriteFile(p, []byte(tc.data), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := f.w.LoadFacts(file); err == nil {
				t.Error("malformed stored input accepted")
			}
			if err := os.Remove(p); err != nil {
				t.Fatal(err)
			}
			if err := f.w.WriteFacts(file, []byte(tc.data)); err == nil {
				t.Error("malformed new file accepted")
			}
			if _, err := os.Stat(p); !os.IsNotExist(err) {
				t.Errorf("absent destination created: %v", err)
			}
			gotEntries, err = os.ReadDir(filepath.Dir(p))
			if err != nil {
				t.Fatal(err)
			}
			if len(gotEntries) != 0 {
				t.Error("stray temporary output")
			}
			if err := f.facts(t, file, dayFacts); err != nil {
				t.Fatal(err)
			}
			if _, err := f.w.Apply(ctx, f.s, file); err != nil {
				t.Fatal(err)
			}
			finalCounts, err := f.s.Counts(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(counts, finalCounts) {
				t.Error("repeat apply changed counts")
			}
		})
	}
	t.Run("empty writes and whitespace", func(t *testing.T) {
		f := setup(t)
		if _, err := f.w.MakeLedger(); err != nil {
			t.Fatal(err)
		}
		data := []byte("{\"file\":\"Journal/2031-04-11.md\",\"writes\":[]} \n\t\r")
		if err := f.w.WriteFacts(file, data); err != nil {
			t.Fatal(err)
		}
		got, err := f.w.LoadFacts(file)
		if err != nil {
			t.Fatal(err)
		}
		if got.Writes == nil || len(got.Writes) != 0 {
			t.Fatal("empty writes lost")
		}
	})
	t.Run("preserve raw semantics", func(t *testing.T) {
		f := setup(t)
		if _, err := f.w.MakeLedger(); err != nil {
			t.Fatal(err)
		}
		data := []byte(`{"file":"Journal/2031-04-11.md","writes":[{"event":{"number":9007199254740993,"string":"\u0061"},"quote":"synthetic"}]}`)
		if err := f.w.WriteFacts(file, data); err != nil {
			t.Fatal(err)
		}
		p, err := f.w.factsPath(file)
		if err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		var want bytes.Buffer
		if err := json.Indent(&want, data, "", "  "); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want.Bytes()) {
			t.Fatal("submitted numeric/string representation changed")
		}
	})
}
