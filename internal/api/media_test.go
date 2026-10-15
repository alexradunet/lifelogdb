package api_test

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"lifelog/internal/api"
)

// TestMediaListsPicturesVideosAndRecordings: the media list of Browse is the files of the three kinds, newest day
// first, with a kind, a picture where one is kept and a day; a file of another kind keeps its page and is a document.
// One kind alone is read by media=, and any other word is refused.
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

	e := must(c.Get(ctx, "/browse?kind=media"))
	rows, _ := props(e)["rows"].([]any)
	var got []string
	for _, r := range rows {
		m := r.(map[string]any)
		got = append(got, m["title"].(string)+" "+m["kind"].(string)+" "+m["file_kind"].(string)+" "+m["mime"].(string)+" day="+str(m["day"])+" picture="+str(m["picture"])+" mentions="+str(m["mentions"]))
	}
	want := []string{
		"Dawn.jpg media image image/jpeg day=" + fixedToday + " picture=/pages/8/preview mentions=1",
		"Walk.mp4 media video video/mp4 day=2031-05-12 picture= mentions=1",
		"Lake.jpg media image image/jpeg day=2031-05-10 picture=" + hrefOf(photo, "preview") + " mentions=1", // its day page shows it
		"Memo 2.m4a media audio audio/mp4 day=2031-05-02 picture= mentions=0",
		"Memo 1.m4a media audio audio/mp4 day=2031-05-01 picture= mentions=0",
		"Blank.jpg media image image/jpeg day= picture=/pages/9/preview mentions=0",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("the rows:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if counts, _ := props(e)["media_counts"].(map[string]any); str(counts["image"]) != "3" || str(counts["video"]) != "1" || str(counts["audio"]) != "2" {
		t.Errorf("the media counts: %v", props(e)["media_counts"])
	}
	if len(e.Entities) != 6 || e.Entities[0].Title != "Dawn.jpg" || e.Entities[0].Class[0] != "file" {
		t.Errorf("the items: %+v", e.Entities)
	}
	if hrefOf(e, "pictures") != "/browse?kind=media&media=image" || hrefOf(e, "videos") != "/browse?kind=media&media=video" ||
		hrefOf(e, "recordings") != "/browse?kind=media&media=audio" || hrefOf(root, "media") != "/browse?kind=media" {
		t.Errorf("the links: %+v", e.Links)
	}
	// The list's add-file takes a file of any kind, as the catalog's does.
	accept := func(a api.Action) string {
		for _, f := range a.Fields {
			if f.Name == "original" {
				return f.Accept
			}
		}
		return "?"
	}
	if accept(find(e, "add-file")) != "" || accept(add) != "" || find(e, "tombstone-pages").Name == "" {
		t.Errorf("the actions: %+v", e.Actions)
	}

	// One kind alone, and its paging keeps the kind; the media counts stay those of every kind.
	audio := must(c.Get(ctx, "/browse?kind=media&media=audio&limit=1"))
	rows, _ = props(audio)["rows"].([]any)
	if len(rows) != 1 || rows[0].(map[string]any)["title"] != "Memo 2.m4a" || props(audio)["media"] != "audio" ||
		hrefOf(audio, "next") != "/browse?kind=media&limit=1&media=audio&offset=1" || hrefOf(audio, "self") != "/browse?kind=media&media=audio" {
		t.Errorf("the recordings: %v %v %q", rows, props(audio)["media"], hrefOf(audio, "next"))
	}
	if counts, _ := props(audio)["media_counts"].(map[string]any); str(counts["image"]) != "3" {
		t.Errorf("the media counts of the recordings: %v", props(audio)["media_counts"])
	}
	if rows, _ := props(must(c.Get(ctx, "/browse?kind=media&media=image")))["rows"].([]any); len(rows) != 3 || rows[1].(map[string]any)["title"] != "Lake.jpg" {
		t.Errorf("the pictures: %v", rows)
	}
	if _, err := c.Get(ctx, "/browse?kind=media&media=document"); !clientStatus(err, 422) {
		t.Errorf("an unknown kind: %v", err)
	}

	// The browser: the counts in words, the kinds as links, a tile a file with its picture or the icon of its kind.
	body := browse(t, h, "/browse?kind=media")
	for _, want := range []string{"<span>3 pictures, 1 video, 2 recordings</span>",
		`<nav class="kinds" aria-label="Media kind"><a href="/browse?kind=media" aria-current="page">All media</a><a href="/browse?kind=media&amp;media=image">Pictures</a><a href="/browse?kind=media&amp;media=video">Videos</a><a href="/browse?kind=media&amp;media=audio">Recordings</a></nav>`,
		`<ul class="plain tiles">`, `<img src="` + hrefOf(photo, "preview") + `" alt="" loading="lazy"><span class="first">Lake.jpg</span><span class="meta">picture · Sat 10 May</span>`,
		`<span class="tile-blank" aria-hidden="true"><svg class="icon"`, `<span class="first">Walk.mp4</span><span class="meta">video · Mon 12 May</span>`, `<span class="first">Dawn.jpg</span><span class="meta">picture · Wed 14 May</span>`,
		`<span class="first">Memo 1.m4a</span><span class="meta">recording · Thu 1 May</span>`, `<span class="first">Blank.jpg</span><span class="meta">picture · no day yet</span>`,
		`<label class="pick-box"><input type="checkbox" name="ids" value="`} {
		if !strings.Contains(body, want) {
			t.Errorf("the media page lacks %s", want)
		}
	}
	if strings.Contains(body, "Scan.pdf") {
		t.Error("the media page shows the PDF")
	}
	// The documents are every other file, as rows, with the same words.
	must(c.Do(ctx, add, map[string]string{"title": "Notes.txt", "sha256": strings.Repeat("0f", 32), "mime": "text/plain", "day": "2031-05-03"}))
	files := must(c.Get(ctx, "/browse?kind=document"))
	got = nil
	for _, r := range props(files)["rows"].([]any) {
		m := r.(map[string]any)
		got = append(got, m["title"].(string)+" "+m["kind"].(string)+" "+m["file_kind"].(string)+" "+m["mime"].(string)+" day="+str(m["day"]))
	}
	if want := "Notes.txt document text text/plain day=2031-05-03\nScan.pdf document application application/pdf day=" + str(props(files)["rows"].([]any)[1].(map[string]any)["day"]); strings.Join(got, "\n") != want ||
		len(files.Entities) != 2 || hrefOf(root, "files") != "/browse?kind=document" ||
		accept(find(files, "add-file")) != "" || find(files, "tombstone-pages").Name == "" || props(files)["media_counts"] != nil {
		t.Errorf("the documents: %v\n%+v %+v", got, files.Links, props(files))
	}
	body = browse(t, h, "/browse?kind=document")
	for _, want := range []string{`<a href="/browse?kind=document" aria-current="page">Documents <small>2</small></a>`,
		`<input type="hidden" name="back" value="/browse?kind=document">`, `aria-label="Select Notes.txt">`,
		`<span class="first">Notes.txt</span><span class="meta">text/plain · Sat 3 May</span>`, `<ul class="plain rows">`} {
		if !strings.Contains(body, want) {
			t.Errorf("the documents page lacks %s", want)
		}
	}
	if strings.Contains(body, "Lake.jpg") || strings.Contains(body, "Memo 1.m4a") || strings.Contains(body, " pictures, ") {
		t.Error("the documents page shows media")
	}
	if body := browse(t, h, "/browse?kind=media&media=video"); !strings.Contains(body, `<a href="/browse?kind=media&amp;media=video" aria-current="page">Videos</a>`) ||
		strings.Contains(body, "Memo 1.m4a") || !strings.Contains(body, `value="/browse?kind=media&amp;media=video">`) {
		t.Errorf("the videos page: %.800s", body)
	}
	if body := browse(t, h, "/browse?kind=media&media=video&offset=1"); !strings.Contains(body, `<p class="empty">Nothing of this kind yet. New, above, makes the first one.</p>`) {
		t.Errorf("an empty kind: %.800s", body)
	}
}

// TestTheMediaCountsFollowTheScope: inside a category the media list counts what is filed there, and Not filed what
// is in no category.
func TestTheMediaCountsFollowTheScope(t *testing.T) {
	c, h, s := freshStore(t)
	browseFixture(t, s) // Family: photo.jpg and scan.pdf; nowhere: memo.m4a
	ctx := t.Context()
	for path, want := range map[string]string{
		"/browse?kind=media":            "image=1 video=0 audio=1",
		"/browse?kind=media&in=Family":  "image=1 video=0 audio=0",
		"/browse?kind=media&unfiled=1":  "image=0 video=0 audio=1",
		"/browse?kind=media&in=Cousins": "image=0 video=0 audio=0",
	} {
		m := props(must(c.Get(ctx, path)))["media_counts"].(map[string]any)
		if got := "image=" + str(m["image"]) + " video=" + str(m["video"]) + " audio=" + str(m["audio"]); got != want {
			t.Errorf("%s: %s, want %s", path, got, want)
		}
	}
	if body := browse(t, h, "/browse?kind=media&in=Family"); !strings.Contains(body, "<span>1 picture, no videos, no recordings</span>") {
		t.Error("the media list in Family does not say its counts")
	}
}

// TestMediaIsEmptyUntilAFileOfItsKindsIsKept: a PDF alone leaves the media list empty, with its counts and its
// empty words, and a photo alone the documents.
func TestMediaIsEmptyUntilAFileOfItsKindsIsKept(t *testing.T) {
	c, h := fresh(t)
	ctx := t.Context()
	must(c.Do(ctx, find(must(c.Get(ctx, "/")), "add-file"), map[string]string{"title": "Scan.pdf", "sha256": strings.Repeat("ef", 32), "mime": "application/pdf"}))
	e := must(c.Get(ctx, "/browse?kind=media"))
	if rows, _ := props(e)["rows"].([]any); len(rows) != 0 || len(e.Entities) != 0 {
		t.Errorf("the rows: %v", props(e)["rows"])
	}
	if body := browse(t, h, "/browse?kind=media"); !strings.Contains(body, `<span>no pictures, no videos, no recordings</span>`) ||
		!strings.Contains(body, `<p class="empty">Nothing of this kind yet. New, above, makes the first one.</p>`) {
		t.Errorf("the empty page: %.800s", body)
	}
	// And the other way round: a photo alone leaves the documents empty.
	c, h = fresh(t)
	path, _ := jpegFile(t, "lake.jpg", 40, 30)
	must(c.DoFiles(ctx, find(must(c.Get(ctx, "/")), "add-file"), map[string]string{"title": "Lake.jpg"}, map[string]string{"original": path}))
	if e := must(c.Get(ctx, "/browse?kind=document")); len(e.Entities) != 0 {
		t.Errorf("the documents: %+v", e.Entities)
	}
	if body := browse(t, h, "/browse?kind=document"); !strings.Contains(body, `<p class="empty">Nothing of this kind yet. New, above, makes the first one.</p>`) {
		t.Errorf("the empty documents page: %.800s", body)
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
