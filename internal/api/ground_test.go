package api_test

import (
	"slices"
	"strings"
	"testing"

	"lifelog/internal/api"
	"lifelog/internal/api/apitest"
)

// TestTheGroundIsOneOfFour: the field lists the four floors in order; a save is read back; a value outside them is 422
// and the setting stays; an empty value clears it.
func TestTheGroundIsOneOfFour(t *testing.T) {
	c, _, _ := apitest.Fresh(t, "cli")
	act := apitest.Action(t, apitest.Must(t)(c.Get(t.Context(), "/actions")), "settings")
	if got := act.Field("ground").Options; !slices.Equal(got, []string{"boards", "oak", "firelight", "embers"}) {
		t.Errorf("the options of ground: %v", got)
	}
	read := func() any {
		t.Helper()
		p, _ := apitest.Must(t)(c.Get(t.Context(), "/settings")).Properties.(map[string]any)
		return p["ground"]
	}
	if got := read(); got != "" {
		t.Errorf("ground with none set: %v, want the empty string", got)
	}
	if _, err := c.Do(t.Context(), act, map[string]string{"ground": "embers"}); err != nil {
		t.Fatal(err)
	}
	if got := read(); got != "embers" {
		t.Errorf("ground after a save: %v", got)
	}
	form := apitest.Action(t, apitest.Must(t)(c.Get(t.Context(), "/settings")), "settings")
	if got := form.Field("ground").Value; got != "embers" {
		t.Errorf("the form of GET /settings holds ground %v, want embers", got)
	}
	_, err := c.Do(t.Context(), act, map[string]string{"ground": "flagstones"})
	if apitest.Status(err) != 422 || !strings.Contains(err.Error(), "ground") {
		t.Errorf("ground flagstones: %v, want 422 that names the field", err)
	}
	if got := read(); got != "embers" {
		t.Errorf("ground after a refused save: %v, want it unchanged", got)
	}
	if _, err := c.Do(t.Context(), act, map[string]string{"ground": ""}); err != nil {
		t.Fatal(err)
	}
	if got := read(); got != "" {
		t.Errorf("ground after an empty save: %v, want the empty string", got)
	}
}

// TestThePageDrawsTheGroundOfTheSettings: a page with the shell names the floor on its body, boards when nothing is
// set; the login page has no shell and no attribute.
func TestThePageDrawsTheGroundOfTheSettings(t *testing.T) {
	c, h, _ := apitest.FreshOn(t, "cli", fixedToday)
	if body := browse(t, h, "/browse"); !strings.Contains(body, `<body data-ground="boards">`) {
		t.Errorf("a page with nothing set has no boards ground:\n%.400s", body)
	}
	act := apitest.Action(t, apitest.Must(t)(c.Get(t.Context(), "/actions")), "settings")
	apitest.Must(t)(c.Do(t.Context(), act, map[string]string{"ground": "embers"}))
	if body := browse(t, h, "/browse"); !strings.Contains(body, `<body data-ground="embers">`) {
		t.Errorf("a page after a save of embers:\n%.400s", body)
	}
	login, _ := publicHandler(t, api.PublicOptions{Token: testToken})
	body := request(login, "GET", "/login", "", map[string]string{"Accept": "text/html"}).Body.String()
	if !strings.Contains(body, "<body>") || strings.Contains(body, "<body data-ground") {
		t.Errorf("the login page carries a ground:\n%.400s", body)
	}
}
