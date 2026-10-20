package api_test

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"lifelog/internal/api/apitest"
)

// TestTheSearchPageFoldsItsPagingFields: "Rows on a page" and "Rows skipped" are under More fields, not beside the
// words.
func TestTheSearchPageFoldsItsPagingFields(t *testing.T) {
	_, h, _ := apitest.Fresh(t, "cli")
	page := browse(t, h, "/search?q=ab")
	fold := regexp.MustCompile(`(?s)<details class="more-fields"><summary>[0-9]+ more fields</summary>(.*?)</details>`).FindStringSubmatch(page)
	if fold == nil || !strings.Contains(fold[1], `name="limit"`) || !strings.Contains(fold[1], `name="offset"`) {
		t.Fatalf("limit and offset are not under More fields:\n%s", page[strings.Index(page, "<main"):])
	}
}

// TestTheFileFormAsksForTheFileFirst: the file is the first field of New file, and it says what is kept.
func TestTheFileFormAsksForTheFileFirst(t *testing.T) {
	_, h, _ := apitest.Fresh(t, "cli")
	page := browse(t, h, "/browse?kind=media&new=add-file")
	form := regexp.MustCompile(`(?s)<form[^>]*action="/files".*?</form>`).FindString(page)
	file, title := strings.Index(form, `name="original"`), strings.Index(form, `name="title"`)
	if form == "" || file < 0 || title < 0 || file > title || !strings.Contains(form, "the original stays where it is") {
		t.Errorf("the file is not first, or the label does not say that the original stays:\n%.2500s", form)
	}
}

// TestTheCoverChooserLabelsTheNewPhotoOnce: the section says A new photo, so the field does not say it again.
func TestTheCoverChooserLabelsTheNewPhotoOnce(t *testing.T) {
	c, h, _ := apitest.Fresh(t, "cli")
	root := apitest.Must(t)(c.Get(t.Context(), "/"))
	p := apitest.Must(t)(c.Do(t.Context(), apitest.Action(t, root, "create-page"), map[string]string{"title": "Trip"}))
	page := browse(t, h, "/pages/"+fmt.Sprint(p.Properties.(map[string]any)["id"])+"/cover")
	if strings.Count(page, ">A new photo<") != 1 || strings.Contains(page, "never stored") {
		t.Errorf("the new photo is labelled twice, or the old label is back")
	}
}
