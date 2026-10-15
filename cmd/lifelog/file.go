package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"lifelog/internal/client"
)

// keepFile is `lifelog file PATH`: the add-file action with the original streamed from disk (hashed by the API,
// never stored), its text read from --text, and a picture from --preview when the original is not one lifelog reads.
func keepFile(ctx context.Context, o opts, c *client.Client, args []string) error {
	if len(args) != 1 {
		return errors.New("file PATH [--title T] [--text FILE] [--preview PICTURE] [--mime TYPE] [--day YYYY-MM-DD] [--dry-run]")
	}
	add, err := actionNamed(ctx, c, "add-file")
	if err != nil {
		return err
	}
	vals := map[string]string{"title": o.title}
	if o.title == "" {
		vals["title"] = filepath.Base(args[0])
	}
	if o.text != "" {
		b, err := os.ReadFile(o.text)
		if err != nil {
			return err
		}
		vals["body"] = string(b)
	}
	for k, v := range map[string]string{"mime": o.mime, "day": o.day} {
		if v != "" {
			vals[k] = v
		}
	}
	if o.dryRun {
		vals["dry_run"] = "1"
	}
	files := map[string]string{"original": args[0]}
	if o.preview != "" {
		files["preview"] = o.preview
	}
	return show(o)(c.DoFiles(ctx, add, vals, files))
}
