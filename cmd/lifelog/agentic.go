package main

import (
	"fmt"
	"os"
	"path/filepath"

	"lifelog/internal/agentic"
)

// agenticInit is `lifelog agentic-init`: the folder of this program made ready for a coding agent opened in it.
func agenticInit(o opts) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("the path of this program: %w", err)
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	dbPath, err := dbPath(o, "")
	if err != nil {
		return err
	}
	if dbPath, err = filepath.Abs(dbPath); err != nil {
		return err
	}
	dir := filepath.Dir(exe)
	res, err := agentic.Init(agentic.Paths{Dir: dir, Exe: exe, DB: dbPath}, o.force)
	for _, r := range res {
		fmt.Printf("%-9s %s\n", r.State, filepath.Join(dir, filepath.FromSlash(r.Path)))
	}
	if err != nil {
		return err
	}
	for _, r := range res {
		if r.State == agentic.Kept {
			fmt.Println("a kept file has another text; lifelog agentic-init --force replaces it")
			break
		}
	}
	if _, err := os.Stat(dbPath); err != nil {
		fmt.Printf("no life.db yet: lifelog init %s makes it\n", dbPath)
	}
	return nil
}
