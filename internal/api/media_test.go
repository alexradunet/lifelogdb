package api_test

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"lifelog/internal/api"
)

// TestMediaListsPicturesVideosAndRecordings: the media list is the files of the three kinds, newest day first, with
// a kind, a picture where one is kept and a day; a file of another kind keeps its page and is not in it. One kind
// alone is read by its name, and any other name is refused.
func TestMediaListsPicturesVideosAndRecordings(t *testing.T) {
	c, h := freshOn(t, fixedToday)
	ctx := t.Context()
	root := must(c.Get(ctx, "/"))
	add := find(root, "add-file")
	jpg := func(name string, w, h int) string { p, _ := jpegFile(t, name, w, h); return p }
	photo := must(c.DoFiles(ctx, add, map[string]string{"title": "Lake.jpg", "day": "2031-05-10"}, map[string]string{"original": jpg("lake.jpg", 40, 30)}))
	must(c.Do(ctx, add, map[string]string{"title": "Walk.mp4", "sha256": strings.Repeat("ab", 32), "mime": "video/mp4", "day": "2031-05-12"}))
	must(c.Do(ctx, find(root, "capture"), map[string]string{"text": "Walked. [[Walk.mp4]]"}))
	for i, title := range []string{"Memo 1.m4a", "Memo 2.m4a"} {
		must(c.Do(ctx, add, map[string]string{"title": title, "sha256": strings.Repeat(string(rune('c'+i))+"d", 32), "mime": "audio/mp4", "day": "2031-05-0" + string(rune('1'+i))}))
	}
	// A photo with no EXIF and no day has no day of its own: the day page that shows it gives it one, or it has none.
	must(c.DoFiles(ctx, add, map[string]string{"title": "Dawn.jpg"}, map[string]string{"original": jpg("dawn.jpg", 32, 24)}))
	must(c.Do(ctx, find(root, "capture"), map[string]string{"text": "![[Dawn.jpg]]"}))
	must(c.DoFiles(ctx, add, map[string]string{"title": "Blank.jpg"}, map[string]string{"original": jpg("blank.jpg", 24, 16)}))
	must(c.Do(ctx, add, map[string]string{"title": "Scan.pdf", "sha256": strings.Repeat("ef", 32), "mime": "application/pdf"}))

	e := must(c.Get(ctx, "/media"))
	rows, _ := props(e)["rows"].([]any)
	var got []string
	for _, r := range rows {
		m := r.(map[string]any)
		got = append(got, m["title"].(string)+" "+m["kind"].(string)+" "+m["mime"].(string)+" day="+str(m["day"])+" picture="+str(m["picture"])+" mentions="+str(m["mentions"]))
	}
	want := []string{
		"Dawn.jpg image image/jpeg day=" + fixedToday + " picture=/pages/8/preview mentions=1",
		"Walk.mp4 video video/mp4 day=2031-05-12 picture= mentions=1",
		"Lake.jpg image image/jpeg day=2031-05-10 picture=" + hrefOf(photo, "preview") + " mentions=1", // its day page shows it
		"Memo 2.m4a audio audio/mp4 day=2031-05-02 picture= mentions=0",
		"Memo 1.m4a audio audio/mp4 day=2031-05-01 picture= mentions=0",
		"Blank.jpg image image/jpeg day= picture=/pages/9/preview mentions=0",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("the rows:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if counts, _ := props(e)["counts"].(map[string]any); str(counts["image"]) != "3" || str(counts["video"]) != "1" || str(counts["audio"]) != "2" {
		t.Errorf("the counts: %v", props(e)["counts"])
	}
	if len(e.Entities) != 6 || e.Entities[0].Title != "Dawn.jpg" || e.Entities[0].Class[0] != "file" || e.Title != "Media" || e.Class[0] != "media" {
		t.Errorf("the items: %+v", e.Entities)
	}
	if hrefOf(e, "pictures") != "/media?kind=image" || hrefOf(e, "videos") != "/media?kind=video" || hrefOf(e, "recordings") != "/media?kind=audio" || hrefOf(root, "media") != "/media" {
		t.Errorf("the links: %+v", e.Links)
	}
	// The list's add-file takes the kinds it shows; the catalog's keeps a file of any kind.
	accept := func(a api.Action) string {
		for _, f := range a.Fields {
			if f.Name == "original" {
				return f.Accept
			}
		}
		return "?"
	}
	if accept(find(e, "add-file")) != "image/*,video/*,audio/*" || accept(add) != "" || find(e, "tombstone-pages").Name == "" {
		t.Errorf("the actions: %+v", e.Actions)
	}

	// One kind alone, and its paging keeps the kind.
	audio := must(c.Get(ctx, "/media?kind=audio&limit=1"))
	rows, _ = props(audio)["rows"].([]any)
	if len(rows) != 1 || rows[0].(map[string]any)["title"] != "Memo 2.m4a" || props(audio)["kind"] != "audio" || hrefOf(audio, "next") != "/media?kind=audio&limit=1&offset=1" || hrefOf(audio, "self") != "/media?kind=audio" {
		t.Errorf("the recordings: %v %v %q", rows, props(audio)["kind"], hrefOf(audio, "next"))
	}
	if rows, _ := props(must(c.Get(ctx, "/media?kind=image")))["rows"].([]any); len(rows) != 3 || rows[1].(map[string]any)["title"] != "Lake.jpg" {
		t.Errorf("the pictures: %v", rows)
	}
	if _, err := c.Get(ctx, "/media?kind=document"); !clientStatus(err, 422) {
		t.Errorf("an unknown kind: %v", err)
	}

	// The browser: the counts in words, the kinds as links, a tile a file with its picture or the icon of its kind.
	body := browse(t, h, "/media")
	for _, want := range []string{"<h1>Media</h1>", `<p class="sub">3 pictures, 1 video, 2 recordings</p>`,
		`<nav class="kinds" aria-label="Kind"><a href="/media" aria-current="page">All</a><a href="/media?kind=image">Pictures</a><a href="/media?kind=video">Videos</a><a href="/media?kind=audio">Recordings</a></nav>`,
		`<ul class="plain tiles">`, `<img src="` + hrefOf(photo, "preview") + `" alt="" loading="lazy"><span class="first">Lake.jpg</span><span class="meta">picture · Sat 10 May</span>`,
		`<span class="tile-blank" aria-hidden="true"><svg class="icon"`, `<span class="first">Walk.mp4</span><span class="meta">video · Mon 12 May</span>`, `<span class="first">Dawn.jpg</span><span class="meta">picture · Wed 14 May</span>`,
		`<span class="first">Memo 1.m4a</span><span class="meta">recording · Thu 1 May</span>`, `<span class="first">Blank.jpg</span><span class="meta">picture · no day yet</span>`, `accept="image/*,video/*,audio/*"`,
		`<label class="pick-box"><input type="checkbox" name="ids" value="`} {
		if !strings.Contains(body, want) {
			t.Errorf("the media page lacks %s", want)
		}
	}
	if strings.Contains(body, "Scan.pdf") {
		t.Error("the media page shows the PDF")
	}
	// Files is every other file, as rows, with the same words.
	must(c.Do(ctx, add, map[string]string{"title": "Notes.txt", "sha256": strings.Repeat("0f", 32), "mime": "text/plain", "day": "2031-05-03"}))
	files := must(c.Get(ctx, "/files"))
	got = nil
	for _, r := range props(files)["rows"].([]any) {
		m := r.(map[string]any)
		got = append(got, m["title"].(string)+" "+m["kind"].(string)+" "+m["mime"].(string)+" day="+str(m["day"]))
	}
	if want := "Notes.txt text text/plain day=2031-05-03\nScan.pdf application application/pdf day=" + str(props(files)["rows"].([]any)[1].(map[string]any)["day"]); strings.Join(got, "\n") != want ||
		len(files.Entities) != 2 || files.Class[0] != "files" || hrefOf(files, "media") != "/media" || hrefOf(root, "files") != "/files" ||
		accept(find(files, "add-file")) != "" || find(files, "tombstone-pages").Name == "" || props(files)["kind"] != nil || props(files)["counts"] != nil {
		t.Errorf("the files: %v\n%+v %+v", got, files.Links, props(files))
	}
	body = browse(t, h, "/files")
	for _, want := range []string{"<h1>Files</h1>", `2 on this page`, `<a href="/media">Pictures, videos and recordings are in Media</a>`,
		`<input type="hidden" name="back" value="/files">`, `aria-label="Select Notes.txt">`,
		`<span class="first">Notes.txt</span><span class="meta">text/plain · Sat 3 May</span>`, `<ul class="plain rows">`} {
		if !strings.Contains(body, want) {
			t.Errorf("the files page lacks %s", want)
		}
	}
	if strings.Contains(body, "Lake.jpg") || strings.Contains(body, "Memo 1.m4a") {
		t.Error("the files page shows media")
	}
	if !strings.Contains(browse(t, h, "/"), `href="/files"`) {
		t.Error("the sidebar does not lead to Files")
	}
	if body := browse(t, h, "/media?kind=video"); !strings.Contains(body, `<a href="/media?kind=video" aria-current="page">Videos</a>`) || strings.Contains(body, "Memo 1.m4a") || !strings.Contains(body, `value="/media?kind=video">`) {
		t.Errorf("the videos page: %.800s", body)
	}
	if body := browse(t, h, "/media?kind=video&offset=1"); !strings.Contains(body, `<p class="empty">None of this kind yet. New file adds one.</p>`) {
		t.Errorf("an empty kind: %.800s", body)
	}
	if !strings.Contains(browse(t, h, "/"), `href="/media"`) {
		t.Error("the sidebar does not lead to Media")
	}
}

// TestMediaIsEmptyUntilAFileOfItsKindsIsKept: a PDF alone leaves the media list empty, with its own words, and a
// photo alone the files list.
func TestMediaIsEmptyUntilAFileOfItsKindsIsKept(t *testing.T) {
	c, h := fresh(t)
	ctx := t.Context()
	must(c.Do(ctx, find(must(c.Get(ctx, "/")), "add-file"), map[string]string{"title": "Scan.pdf", "sha256": strings.Repeat("ef", 32), "mime": "application/pdf"}))
	e := must(c.Get(ctx, "/media"))
	if rows, _ := props(e)["rows"].([]any); len(rows) != 0 || len(e.Entities) != 0 {
		t.Errorf("the rows: %v", props(e)["rows"])
	}
	if body := browse(t, h, "/media"); !strings.Contains(body, `<p class="sub">no pictures, no videos, no recordings</p>`) || !strings.Contains(body, `<p class="empty">No media yet. New file adds a picture, a video or a recording.</p>`) {
		t.Errorf("the empty page: %.800s", body)
	}
	// And the other way round: a photo alone leaves the files list empty.
	c, h = fresh(t)
	path, _ := jpegFile(t, "lake.jpg", 40, 30)
	must(c.DoFiles(ctx, find(must(c.Get(ctx, "/")), "add-file"), map[string]string{"title": "Lake.jpg"}, map[string]string{"original": path}))
	if e := must(c.Get(ctx, "/files")); len(e.Entities) != 0 {
		t.Errorf("the files: %+v", e.Entities)
	}
	if body := browse(t, h, "/files"); !strings.Contains(body, `<p class="empty">No other file yet. New file adds one; a picture, a video or a recording goes to Media.</p>`) {
		t.Errorf("the empty files page: %.800s", body)
	}
}

// str is a property as a string: a JSON number without its exponent, nothing as "".
func str(x any) string {
	switch v := x.(type) {
	case nil:
		return ""
	case string:
		return v
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case json.Number:
		return v.String()
	}
	return "?"
}
