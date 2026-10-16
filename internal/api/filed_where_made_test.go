package api_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"lifelog/internal/api"
	"lifelog/internal/api/apitest"
	"lifelog/internal/client"
)

// A new person, place or file is filed where it is made (docs/plans/117-one-browse-view.md, rule 6).

// partOfLinks are the titles the entity links part-of, in order.
func partOfLinks(e *api.Entity) []string {
	var out []string
	for _, l := range e.Entities {
		if len(l.Rel) == 1 && l.Rel[0] == "part-of" {
			out = append(out, l.Title)
		}
	}
	return out
}

func TestTheMakersTakeACategory(t *testing.T) {
	c, _, _ := apitest.Fresh(t, "cli")
	root := apitest.Must(t)(c.Get(t.Context(), "/"))
	for _, name := range []string{"create-page", "create-person", "create-place", "add-file"} {
		a := apitest.Action(t, root, name)
		found := false
		for _, f := range a.Fields {
			if f.Name == "category" {
				found = true
				if f.Required || !f.Pages || f.Title != "Category (title)" {
					t.Errorf("%s: category is %+v, want an optional page title", name, f)
				}
			}
		}
		if !found {
			t.Errorf("%s has no field category", name)
		}
	}
}

func TestANewThingIsFiledWhereItIsMadeOverHTTP(t *testing.T) {
	c, _, _ := apitest.Fresh(t, "cli")
	ctx := t.Context()
	root := apitest.Must(t)(c.Get(ctx, "/"))

	person := apitest.Must(t)(c.Do(ctx, apitest.Action(t, root, "create-person"), map[string]string{"title": "Ana", "name": "Ana Example", "category": "Family"}))
	if got := partOfLinks(person); strings.Join(got, ",") != "Family" {
		t.Errorf("Ana is part-of %v, want Family", got)
	}
	place := apitest.Must(t)(c.Do(ctx, apitest.Action(t, root, "create-place"), map[string]string{"title": "Lakeside", "category": "family"}))
	if got := partOfLinks(place); strings.Join(got, ",") != "Family" {
		t.Errorf("Lakeside is part-of %v, want Family (the category that exists, by any case)", got)
	}
	file := apitest.Must(t)(c.Do(ctx, apitest.Action(t, root, "add-file"), map[string]string{"title": "scan.pdf", "sha256": strings.Repeat("ab", 32),
		"mime": "application/pdf", "day": "2031-05-01", "category": "Papers"}))
	if got := partOfLinks(file); strings.Join(got, ",") != "Papers" {
		t.Errorf("scan.pdf is part-of %v, want Papers", got)
	}
	if f, _ := file.Result.(map[string]any)["filed"].(map[string]any); f["created"] != true || f["added"] != json.Number("1") {
		t.Errorf("add-file's result filed = %v, want Papers made and 1 link added", f)
	}

	// The browser sends the original as a multipart form: the category is a field of it too.
	path := filepath.Join(t.TempDir(), "memo.txt")
	if err := os.WriteFile(path, []byte("a synthetic memo"), 0o644); err != nil {
		t.Fatal(err)
	}
	memo := apitest.Must(t)(c.DoFiles(ctx, apitest.Action(t, root, "add-file"), map[string]string{"title": "memo.txt", "category": "Papers"}, map[string]string{"original": path}))
	if got := partOfLinks(memo); strings.Join(got, ",") != "Papers" {
		t.Errorf("memo.txt (multipart) is part-of %v, want Papers", got)
	}

	// The tree: Family with Ana and Lakeside, Papers with the two files.
	tree := map[string]any{}
	for _, x := range props(apitest.Must(t)(c.Get(ctx, "/browse")))["categories"].([]any) {
		cat := x.(map[string]any)
		tree[cat["title"].(string)] = cat["count"]
	}
	if fmt.Sprint(tree) != "map[Family:2 Papers:2]" {
		t.Errorf("the tree: %v", tree)
	}
}

// A day is never a category: 422, and neither the thing nor its link is written. A day with no page yet is refused by
// entities_day_page, and no day page is written; a day page that exists is refused by links_endpoint_types (D26).
func TestADayAsTheCategoryWritesNothingOverHTTP(t *testing.T) {
	for _, exists := range []bool{false, true} {
		t.Run(fmt.Sprint("day page exists ", exists), func(t *testing.T) {
			c, _, _ := apitest.Fresh(t, "cli")
			ctx := t.Context()
			root := apitest.Must(t)(c.Get(ctx, "/"))
			if exists {
				apitest.Must(t)(c.Do(ctx, apitest.Action(t, apitest.Must(t)(c.Get(ctx, "/days/2031-05-02")), "capture"), map[string]string{"text": "a synthetic entry"}))
			}
			for _, tc := range []struct {
				action string
				values map[string]string
			}{
				{"create-person", map[string]string{"title": "Nobody"}},
				{"create-place", map[string]string{"title": "Nobody"}},
				{"add-file", map[string]string{"title": "Nobody", "sha256": strings.Repeat("cd", 32), "mime": "application/pdf"}},
			} {
				tc.values["category"] = "2031-05-02"
				if _, err := c.Do(ctx, apitest.Action(t, root, tc.action), tc.values); apitest.Status(err) != http.StatusUnprocessableEntity {
					t.Errorf("%s with a day as the category: %v, want 422", tc.action, err)
				}
				if _, err := c.Get(ctx, "/pages?title=Nobody"); apitest.Status(err) != http.StatusNotFound {
					t.Errorf("%s: Nobody was written although the write was refused: %v", tc.action, err)
				}
				_, err := c.Get(ctx, "/pages?title=2031-05-02")
				if !exists && apitest.Status(err) != http.StatusNotFound {
					t.Errorf("%s: the day page was written although the write was refused: %v", tc.action, err)
				}
				if exists && err != nil {
					t.Errorf("%s: the day page that exists: %v", tc.action, err)
				}
			}
		})
	}
}

func TestAddFileFilesTheFileThatExistsAndADryRunWritesNothing(t *testing.T) {
	c, _, _ := apitest.Fresh(t, "cli")
	ctx := t.Context()
	add := apitest.Action(t, apitest.Must(t)(c.Get(ctx, "/")), "add-file")
	sent := map[string]string{"title": "scan.pdf", "sha256": strings.Repeat("ef", 32), "mime": "application/pdf", "day": "2031-05-01"}
	kept := apitest.Must(t)(c.Do(ctx, add, sent))
	if got := partOfLinks(kept); len(got) != 0 {
		t.Fatalf("no category sent, but part-of %v", got)
	}

	// A dry run with a category: it says it would file the page that exists, and writes nothing.
	sent["category"], sent["dry_run"] = "Papers", "1"
	dry := apitest.Must(t)(c.Do(ctx, add, sent))
	r, _ := dry.Result.(map[string]any)
	f, _ := r["filed"].(map[string]any)
	cat, _ := f["category"].(map[string]any)
	if dry.Class[1] != "dry-run" || r["existing"] != true || f["created"] != true || f["added"] != json.Number("1") || cat["title"] != "Papers" || cat["id"] != json.Number("0") {
		t.Errorf("the dry run: %v %v, want the page that exists, filed in Papers it would make", dry.Class, r)
	}
	if _, err := c.Get(ctx, "/pages?title=Papers"); apitest.Status(err) != http.StatusNotFound {
		t.Errorf("the dry run made Papers: %v", err)
	}
	if got := partOfLinks(apitest.Must(t)(c.Get(ctx, fmt.Sprintf("/pages/%v", props(kept)["id"])))); len(got) != 0 {
		t.Errorf("the dry run filed the page in %v", got)
	}

	// The write: the same original again files its page.
	delete(sent, "dry_run")
	sent["title"] = "another title.pdf"
	again := apitest.Must(t)(c.Do(ctx, add, sent))
	if props(again)["id"] != props(kept)["id"] || strings.Join(partOfLinks(again), ",") != "Papers" {
		t.Errorf("the same original again: page %v part-of %v, want page %v part-of Papers", props(again)["id"], partOfLinks(again), props(kept)["id"])
	}
}

// Browse inside a category fills category in the New form of each kind; outside one the field is empty.
func TestBrowseNewFormsInACategoryCarryIt(t *testing.T) {
	c, h, s := apitest.Fresh(t, "cli")
	browseFixture(t, s)
	ctx := t.Context()
	for _, tc := range []struct{ kind, action string }{
		{"page", "create-page"}, {"person", "create-person"}, {"place", "create-place"}, {"media", "add-file"}, {"document", "add-file"},
	} {
		made := apitest.Action(t, apitest.Must(t)(c.Get(ctx, "/browse?in=family&kind="+tc.kind)), tc.action)
		if v := fieldOf(made, "category"); v != "Family" {
			t.Errorf("%s in Family: %s has category %q, want Family", tc.kind, tc.action, v)
		}
		if v := fieldOf(apitest.Action(t, apitest.Must(t)(c.Get(ctx, "/browse?kind="+tc.kind)), tc.action), "category"); v != "" {
			t.Errorf("%s everywhere: %s has category %q, want none", tc.kind, tc.action, v)
		}
		body := browse(t, h, "/browse?in=Family&kind="+tc.kind)
		form := regexp.MustCompile(`(?s)<form method="POST" action="` + regexp.QuoteMeta(made.Href) + `"[ >].*?</form>`).FindString(body)
		if !regexp.MustCompile(`<input[^>]*name="category"[^>]*value="Family"`).MatchString(form) {
			t.Errorf("%s in Family: the page's %s form has no category Family: %s", tc.kind, tc.action, form)
		}
	}

	// The form, sent as it is filled, files the new person in Family.
	ana := apitest.Must(t)(c.Do(ctx, apitest.Action(t, apitest.Must(t)(c.Get(ctx, "/browse?in=Family&kind=person")), "create-person"), map[string]string{"title": "Cy"}))
	if got := partOfLinks(ana); strings.Join(got, ",") != "Family" {
		t.Errorf("Cy made from Browse in Family is part-of %v", got)
	}
}

// The same write in process and over HTTP, by the owner's CLI and an agent: the place and its link.
func TestFilingWhereMadeTransportParity(t *testing.T) {
	for _, remote := range []bool{false, true} {
		for _, source := range []string{"cli", "agent:browse"} {
			t.Run(fmt.Sprint(remote, source), func(t *testing.T) {
				_, h, _ := apitest.Fresh(t, "cli")
				c := client.InProcess(h, source)
				if remote {
					_, c = apitest.Clients(t, h, source)
				}
				actions, err := c.Catalog(t.Context())
				if err != nil {
					t.Fatal(err)
				}
				var place api.Action
				for _, a := range actions {
					if a.Name == "create-place" {
						place = a
					}
				}
				e := apitest.Must(t)(c.Do(t.Context(), place, map[string]string{"title": "Lakeside", "category": "Trips"}))
				if got := partOfLinks(e); strings.Join(got, ",") != "Trips" {
					t.Errorf("Lakeside is part-of %v, want Trips", got)
				}
			})
		}
	}
}
