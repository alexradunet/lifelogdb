package agent

import (
	"strings"
	"testing"

	"lifelog-whatsapp/siren"
)

func TestToolsRefuseNormalizedDuplicates(t *testing.T) {
	for _, tc := range []struct {
		name    string
		actions []siren.Action
		want    string
	}{
		{"catalog collision", []siren.Action{{Name: "same-name"}, {Name: "same_name"}}, `"same_name"`},
		{"built-in get", []siren.Action{{Name: "get"}}, `"get"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tools(tc.actions)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("tools() = %v, want a collision containing %s", err, tc.want)
			}
		})
	}
	list, err := tools([]siren.Action{{Name: "get", Owner: true}, {Name: "find"}, {Name: "import-find"}, {Name: "open-day", Method: "GET"}})
	if err != nil {
		t.Fatalf("owner-only duplicate and distinct public tools were rejected: %v", err)
	}
	var names []string
	for _, x := range list {
		names = append(names, x.name)
	}
	if got, want := strings.Join(names, " "), "find import_find open_day get"; got != want {
		t.Fatalf("tools = %s, want %s", got, want)
	}
}

func TestArgumentsFlattenToFormValues(t *testing.T) {
	got, err := arguments([]byte(`{"day":"2031-05-14","mood":3,"value":72.50,"note":null}`))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"day": "2031-05-14", "mood": "3", "value": "72.50"}
	if len(got) != len(want) {
		t.Fatalf("arguments = %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("arguments[%s] = %q, want %q", k, got[k], v)
		}
	}
	if _, err := arguments([]byte(`{"list":[1]}`)); err == nil {
		t.Fatal("a list argument was accepted")
	}
	if _, err := arguments([]byte(`{"day":`)); err == nil {
		t.Fatal("malformed JSON was accepted")
	}
	if got, err := arguments(nil); err != nil || len(got) != 0 {
		t.Fatalf("arguments(nil) = %v, %v", got, err)
	}
}

// TestAnActionWithAFileFieldIsNotATool: the MCP server offers a file field as the path of a local file, which it
// reads and sends. This agent acts on text that any surface wrote to the page, so a path would let that text name
// the owner's token or the session store: an action with a file field is left out.
func TestAnActionWithAFileFieldIsNotATool(t *testing.T) {
	list, err := tools([]siren.Action{
		{Name: "add-file", Method: "POST", Href: "/files", Fields: []siren.Field{{Name: "original", Type: "file", Title: "The file"}, {Name: "day", Type: "date"}}},
		{Name: "set-cover", Method: "POST", Href: "/pages/{id}/cover", Fields: []siren.Field{{Name: "id", In: "path", Required: true}, {Name: "picture", Type: "file"}}},
		{Name: "find", Method: "GET", Href: "/search", Fields: []siren.Field{{Name: "q", Type: "text"}}},
		{Name: "say", Method: "POST", Href: "/days/{day}/say", Fields: []siren.Field{{Name: "day", In: "path", Required: true}, {Name: "text", Type: "textarea", Required: true}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tl := range list {
		names = append(names, tl.name)
	}
	if got := strings.Join(names, " "); got != "find get" {
		t.Errorf("tools %q, want find and get only (no file field, no say)", got)
	}
}
