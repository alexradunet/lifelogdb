package main

import (
	"fmt"
	"os"
	"path/filepath"

	"lifelog/internal/agentic"
)

// agenticInit is `lifelog agentic-init`: the folder of this program made ready for a coding agent opened in it.
func agenticInit(e env) error {
	dbPath, err := dbPath(e)
	if err != nil {
		return err
	}
	if dbPath, err = filepath.Abs(dbPath); err != nil {
		return err
	}
	exe, err := program(e)
	if err != nil {
		return err
	}
	dir := filepath.Dir(exe)
	out := &errWriter{w: e.stdout}
	res, err := agentic.Init(agentic.Paths{Dir: dir, Exe: exe, DB: dbPath}, e.o.force)
	for _, r := range res {
		fmt.Fprintf(out, "%-9s %s\n", r.State, filepath.Join(dir, filepath.FromSlash(r.Path)))
	}
	if err != nil {
		return err
	}
	for _, r := range res {
		if r.State == agentic.Kept {
			fmt.Fprintln(out, "a kept file has another text; lifelog agentic-init --force replaces it")
			break
		}
	}
	if _, err := os.Stat(dbPath); err != nil {
		fmt.Fprintf(out, "no life.db yet: lifelog init %s makes it\n", dbPath)
	}
	return out.err
}
