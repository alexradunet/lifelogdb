package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
)

// keepFile is `lifelog file PATH`: the add-file action with the original streamed from disk (hashed by the API,
// never stored), its text read from --text, a picture from --preview when the original is not one lifelog reads, and
// --category to file its page in a category in the same write.
func keepFile(ctx context.Context, e env, args []string) error {
	o, c := e.o, e.c
	if len(args) != 1 {
		return errors.New("file PATH [--title T] [--text FILE] [--preview PICTURE] [--mime TYPE] [--day YYYY-MM-DD] [--category TITLE] [--dry-run]")
	}
	add, err := actionNamed(ctx, c, "add-file")
	if err != nil {
		return err
	}
	vals := map[string]string{"title": o.file.title}
	if o.file.title == "" {
		vals["title"] = filepath.Base(args[0])
	}
	if o.file.text != "" {
		b, err := os.ReadFile(o.file.text)
		if err != nil {
			return err
		}
		vals["body"] = string(b)
	}
	for k, v := range map[string]string{"mime": o.file.mime, "day": o.day, "category": o.file.category} {
		if v != "" {
			vals[k] = v
		}
	}
	if o.file.dryRun {
		vals["dry_run"] = "1"
	}
	files := map[string]string{"original": args[0]}
	if o.file.preview != "" {
		files["preview"] = o.file.preview
	}
	return show(e)(c.DoFiles(ctx, add, vals, files))
}

// setCover is `lifelog cover PAGE-ID [PHOTO]`: the set-cover action, with a new photo streamed from disk (hashed by
// the API, never stored) or, with no PHOTO, the kept picture that --title names.
func setCover(ctx context.Context, e env, args []string) error {
	o, c := e.o, e.c
	usage := errors.New("cover PAGE-ID [PHOTO] [--title T] [--preview PICTURE]: a new PHOTO, or --title of a kept picture")
	if len(args) < 1 || len(args) > 2 || len(args) == 1 && o.file.title == "" {
		return usage
	}
	set, err := actionNamed(ctx, c, "set-cover")
	if err != nil {
		return err
	}
	vals := map[string]string{"id": args[0]}
	if o.file.title != "" {
		vals["title"] = o.file.title
	}
	if len(args) == 1 {
		return show(e)(c.Do(ctx, set, vals))
	}
	files := map[string]string{"original": args[1]}
	if o.file.preview != "" {
		files["preview"] = o.file.preview
	}
	return show(e)(c.DoFiles(ctx, set, vals, files))
}
