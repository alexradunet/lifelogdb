// Command lifelog-agentic-init is the extension behind `lifelog agentic-init`: it makes the folder of the lifelog
// program ready for a coding agent opened in it (plan 090). It reads LIFELOG_EXE and LIFELOG_DB, which `lifelog` sets
// for an extension (extensions/README.md). It opens no database and calls no API.
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"lifelog-agentic/agentic"
)

func main() {
	if err := run(os.Args[1:], os.Getenv, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "lifelog-agentic-init:", err)
		os.Exit(1)
	}
}

func run(args []string, getenv func(string) string, stdout io.Writer) error {
	fs := flag.NewFlagSet("lifelog-agentic-init", flag.ContinueOnError)
	fs.SetOutput(io.Discard) // the error carries the message; main prints it once
	force := fs.Bool("force", false, "replace a file that has another text")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected argument %q: the only flag is --force", fs.Arg(0))
	}
	exe, dbPath := getenv("LIFELOG_EXE"), getenv("LIFELOG_DB")
	if exe == "" || dbPath == "" {
		return errors.New("run it as lifelog agentic-init: it needs LIFELOG_EXE and LIFELOG_DB")
	}
	dbPath, err := filepath.Abs(dbPath)
	if err != nil {
		return err
	}
	dir := filepath.Dir(exe)
	var out bytes.Buffer
	res, err := agentic.Init(agentic.Paths{Dir: dir, Exe: exe, DB: dbPath}, *force)
	for _, r := range res {
		fmt.Fprintf(&out, "%-9s %s\n", r.State, filepath.Join(dir, filepath.FromSlash(r.Path)))
	}
	if err == nil {
		addHints(&out, res, dbPath)
	}
	// The lines of the files written before a failure are shown, as they were in the core.
	if _, werr := stdout.Write(out.Bytes()); err == nil {
		err = werr
	}
	return err
}

// addHints adds the lines that say what to do next: replace a kept file, make the database.
func addHints(out *bytes.Buffer, res []agentic.Result, dbPath string) {
	for _, r := range res {
		if r.State == agentic.Kept {
			fmt.Fprintln(out, "a kept file has another text; lifelog agentic-init --force replaces it")
			break
		}
	}
	if _, err := os.Stat(dbPath); err != nil {
		fmt.Fprintf(out, "no life.db yet: lifelog init %s makes it\n", dbPath)
	}
}
