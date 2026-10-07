// lifescale runs the opt-in synthetic workload described in tests/README.md.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"

	"lifelog/internal/scaletest"
)

func main() {
	var options scaletest.Options
	flag.StringVar(&options.Profile, "profile", "small", "small, lifetime (50 years), or stress")
	flag.Int64Var(&options.Seed, "seed", 2075, "synthetic generator seed")
	flag.IntVar(&options.Samples, "samples", 5, "warm samples per workflow (1-20); snapshot/restore run once")
	flag.BoolVar(&options.Previews, "previews", false, "generate unique realistic-sized JPEG previews; lifetime requires tens of GB")
	flag.StringVar(&options.Directory, "dir", "", "parent scratch directory (default: system temp)")
	flag.BoolVar(&options.Keep, "keep", false, "retain the generated scratch directory")
	flag.StringVar(&options.Storage, "storage", "", "hardware/filesystem description to record with results")
	flag.Parse()
	if flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "lifescale accepts flags only")
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	options.Progress = func(message string) { fmt.Fprintln(os.Stderr, message) }
	report, err := scaletest.Run(ctx, options)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
