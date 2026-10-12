package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"lifelog/internal/api"
	"lifelog/internal/client"
	"lifelog/internal/core"
)

// keepFile is `lifelog file PATH`: the add-file action with the original streamed from disk (hashed by the API,
// never stored), its text read from --text, and a picture from --preview when the original is not one lifelog reads.
func keepFile(ctx context.Context, o opts, c *client.Client, args []string) error {
	if len(args) == 0 {
		return errors.New("file PATH... [--title T] [--text FILE] [--preview PICTURE] [--mime TYPE] [--day YYYY-MM-DD] [--at PLACE [--radius M]] [--dry-run]")
	}
	paths, err := mediaPaths(args)
	if err != nil {
		return err
	}
	one := len(args) == 1 && len(paths) == 1 && paths[0] == args[0]
	if !one && (o.title != "" || o.text != "" || o.preview != "" || o.mime != "") {
		return errors.New("--title, --text, --preview and --mime describe one file: keep it alone")
	}
	actions, err := c.Catalog(ctx)
	if err != nil {
		return err
	}
	var add api.Action
	for _, a := range actions {
		if a.Name == "add-file" {
			add = a
		}
	}
	if add.Name == "" {
		return errors.New("the API has no add-file action")
	}
	keep := func(p string) (*api.Entity, error) {
		vals := map[string]string{"title": o.title}
		if o.title == "" {
			vals["title"] = filepath.Base(p)
		}
		if o.text != "" {
			b, err := os.ReadFile(o.text)
			if err != nil {
				return nil, err
			}
			vals["body"] = string(b)
		}
		for k, v := range map[string]string{"mime": o.mime, "day": o.day, "at": o.at, "radius": o.radius} {
			if v != "" {
				vals[k] = v
			}
		}
		if o.dryRun {
			vals["dry_run"] = "1"
		}
		files := map[string]string{"original": p}
		if o.preview != "" {
			files["preview"] = o.preview
		}
		return c.DoFiles(ctx, add, vals, files)
	}
	if one {
		e, err := keep(paths[0])
		if err := show(o)(e, err); err != nil {
			return err
		}
		if u := unmatchedOf(e); u != nil {
			fmt.Fprintf(os.Stderr, "near no place: %v, %v (%v)\nname it: lifelog file %s --at PLACE [--radius METRES]\n", u["lat"], u["lon"], u["map"], paths[0])
		}
		return nil
	}
	var kept []keptFile
	failed := 0
	for _, p := range paths {
		if err := ctx.Err(); err != nil {
			return err
		}
		k := keptFile{Path: p}
		e, err := keep(p)
		if err != nil {
			k.Error = err.Error()
			failed++
		} else if r, ok := e.Result.(map[string]any); ok {
			k.Result = r
		}
		kept = append(kept, k)
	}
	s, err := summarise(kept, o.dryRun)
	if err != nil {
		return err
	}
	if o.human {
		s.print(os.Stdout)
	} else if err := printJSON(os.Stdout, s); err != nil {
		return err
	}
	if failed > 0 {
		return fmt.Errorf("%d of %d files could not be kept", failed, len(paths))
	}
	return nil
}

// mediaPaths are the files to keep: a path as it is, a folder as its photos and videos (not its sub-folders), sorted.
func mediaPaths(args []string) ([]string, error) {
	var out []string
	for _, a := range args {
		st, err := os.Stat(a)
		if err != nil {
			return nil, err
		}
		if !st.IsDir() {
			out = append(out, a)
			continue
		}
		entries, err := os.ReadDir(a)
		if err != nil {
			return nil, err
		}
		var found []string
		for _, e := range entries {
			t := api.MimeOf(e.Name(), nil)
			if !e.IsDir() && (strings.HasPrefix(t, "image/") || strings.HasPrefix(t, "video/")) {
				found = append(found, filepath.Join(a, e.Name()))
			}
		}
		if len(found) == 0 {
			return nil, fmt.Errorf("%s holds no photo or video", a)
		}
		sort.Strings(found)
		out = append(out, found...)
	}
	return out, nil
}

func unmatchedOf(e *api.Entity) map[string]any {
	if e == nil {
		return nil
	}
	r, _ := e.Result.(map[string]any)
	u, _ := r["unmatched"].(map[string]any)
	return u
}

// keptFile is what keeping one file of a batch did: the add-file result, or the refusal.
type keptFile struct {
	Path   string         `json:"path"`
	Error  string         `json:"error,omitempty"`
	Result map[string]any `json:"result,omitempty"`
}

// batch is the report of a batch: per day what was kept and linked, and the photos near no place in
// groups, each to be named once.
type batch struct {
	DryRun    bool              `json:"dry_run,omitempty"`
	Kept      int               `json:"kept"`
	Already   int               `json:"kept_already"`
	Failed    int               `json:"failed"`
	Days      map[string]*daySo `json:"days"`
	NoDay     []string          `json:"no_day_of_their_own,omitempty"`
	Unmatched []*group          `json:"near_no_place,omitempty"`
	Files     []keptFile        `json:"files"`
}

type daySo struct {
	Photos int      `json:"photos"`
	At     []string `json:"at,omitempty"`         // the places linked
	Known  []string `json:"recognised,omitempty"` // places matched and never linked (home, work)
	Near   int      `json:"near_no_place,omitempty"`
}

type group struct {
	Lat, Lon float64  `json:"-"`
	Count    int      `json:"photos"`
	Days     []string `json:"days"`
	Map      string   `json:"map"`
	NameWith string   `json:"name_it_with"`
}

// Coordinates are a floating-point domain; leave all other response numbers (IDs
// in particular) in their lossless client representation.
func coordinate(v any) (float64, error) {
	var n float64
	var err error
	switch v := v.(type) {
	case json.Number:
		n, err = v.Float64()
	case float64:
		n = v
	default:
		return 0, fmt.Errorf("invalid photo coordinate %v", v)
	}
	if err != nil {
		return 0, fmt.Errorf("invalid photo coordinate: %w", err)
	}
	if math.IsNaN(n) || math.IsInf(n, 0) {
		return 0, errors.New("photo coordinate must be finite")
	}
	return n, nil
}

func summarise(files []keptFile, dry bool) (*batch, error) {
	b := &batch{DryRun: dry, Days: map[string]*daySo{}, Files: files}
	add := func(list []string, s string) []string {
		for _, x := range list {
			if x == s {
				return list
			}
		}
		return append(list, s)
	}
	for _, f := range files {
		r := f.Result
		switch {
		case f.Error != "":
			b.Failed++
			continue
		case r["existing"] == true:
			b.Already++
		default:
			b.Kept++
		}
		day, _ := r["day"].(string)
		u, _ := r["unmatched"].(map[string]any)
		if day == "" {
			b.NoDay = append(b.NoDay, filepath.Base(f.Path))
		} else {
			d := b.Days[day]
			if d == nil {
				d = &daySo{}
				b.Days[day] = d
			}
			d.Photos++
			if place, _ := r["place"].(string); place != "" {
				if r["linked"] == true {
					d.At = add(d.At, place)
				} else {
					d.Known = add(d.Known, place)
				}
			}
			if u != nil {
				d.Near++
			}
		}
		if u == nil {
			continue
		}
		lat, err := coordinate(u["lat"])
		if err != nil {
			return nil, err
		}
		lon, err := coordinate(u["lon"])
		if err != nil {
			return nil, err
		}
		var g *group // the photos a place of the default radius at the first one's position would hold
		for _, x := range b.Unmatched {
			if core.Near(x.Lat, x.Lon, lat, lon, float64(core.DefaultRadius)) {
				g = x
				break
			}
		}
		if g == nil {
			m, _ := u["map"].(string)
			g = &group{Lat: lat, Lon: lon, Map: m,
				NameWith: fmt.Sprintf("lifelog file \"%s\" --at PLACE [--radius METRES], then keep them all again", f.Path)}
			b.Unmatched = append(b.Unmatched, g)
		}
		g.Count++
		if day != "" {
			g.Days = add(g.Days, day)
		}
	}
	for _, g := range b.Unmatched {
		sort.Strings(g.Days)
	}
	return b, nil
}

func (b *batch) print(w io.Writer) {
	if b.DryRun {
		fmt.Fprintln(w, "dry run: nothing written")
	}
	fmt.Fprintf(w, "%d files: %d kept, %d kept already, %d failed\n", b.Kept+b.Already+b.Failed, b.Kept, b.Already, b.Failed)
	days := make([]string, 0, len(b.Days))
	for d := range b.Days {
		days = append(days, d)
	}
	sort.Strings(days)
	for _, d := range days {
		x := b.Days[d]
		line := fmt.Sprintf("%s: %d photo(s)", d, x.Photos)
		if len(x.At) > 0 {
			line += " · at " + strings.Join(x.At, ", ")
		}
		if len(x.Known) > 0 {
			line += " · recognised, not linked: " + strings.Join(x.Known, ", ")
		}
		if x.Near > 0 {
			line += fmt.Sprintf(" · %d near no place", x.Near)
		}
		fmt.Fprintln(w, line)
	}
	if len(b.NoDay) > 0 {
		fmt.Fprintf(w, "no day of their own (no EXIF date): %s\n", strings.Join(b.NoDay, ", "))
	}
	if len(b.Unmatched) > 0 {
		fmt.Fprintf(w, "near no place, %d group(s):\n", len(b.Unmatched))
		for i, g := range b.Unmatched {
			fmt.Fprintf(w, "  %d. %d photo(s), %s, %s\n     name it: %s\n", i+1, g.Count, strings.Join(g.Days, " "), g.Map, g.NameWith)
		}
	}
	for _, f := range b.Files {
		if f.Error != "" {
			fmt.Fprintf(w, "failed: %s: %s\n", f.Path, f.Error)
		}
	}
}
