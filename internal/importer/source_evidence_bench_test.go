package importer

import (
	"fmt"
	"strings"
	"testing"
)

func syntheticEvidenceCheck(n int) (*Workspace, *Facts, string, map[string]Metric) {
	var source strings.Builder
	source.WriteString("| day | value | unit |\n|---|---|---|\n")
	facts := &Facts{File: "Synthetic.md"}
	for i := 0; i < n; i++ {
		value := fmt.Sprint(i + 10)
		quote := "| 2031-06-12 | " + value + " | mg |"
		source.WriteString(quote + "\n")
		facts.Writes = append(facts.Writes, Write{Quote: quote, Reading: &ReadingW{Metric: "dose", Day: "2031-06-12", Value: value, Unit: "mg"}})
	}
	return &Workspace{}, facts, source.String(), map[string]Metric{"dose": {Name: "dose", Unit: "mg"}}
}

func BenchmarkReadingSourceCheck(b *testing.B) {
	for _, n := range []int{10, 100, 500} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			w, f, source, approved := syntheticEvidenceCheck(n)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, errs := w.checkStatic(f, source, &Rules{}, approved)
				if len(errs) > 0 {
					b.Fatal(errs)
				}
			}
		})
	}
}
