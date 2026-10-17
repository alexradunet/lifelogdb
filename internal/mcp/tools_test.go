package mcp

import (
	"strings"
	"testing"

	"lifelog/internal/api/siren"
)

func TestToolsRefuseNormalizedDuplicates(t *testing.T) {
	for _, tc := range []struct {
		name    string
		actions []siren.Action
		want    string
	}{
		{"catalog collision", []siren.Action{{Name: "same-name"}, {Name: "same_name"}}, `"same_name"`},
		{"built-in get", []siren.Action{{Name: "get"}}, `"get"`},
		{"built-in get day", []siren.Action{{Name: "get-day"}}, `"get_day"`},
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
	if got, want := strings.Join(names, " "), "find import_find open_day get_day get"; got != want {
		t.Fatalf("tools = %s, want %s", got, want)
	}
}

// TestGetDayIsOpenDay: get_day is the action open-day under the name of a read, so there is no get_day when open-day
// is not a tool: it is absent, or it is the owner's.
func TestGetDayIsOpenDay(t *testing.T) {
	for _, actions := range [][]siren.Action{{{Name: "find"}}, {{Name: "find"}, {Name: "open-day", Owner: true}}} {
		list, err := tools(actions)
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, x := range list {
			names = append(names, x.name)
		}
		if got := strings.Join(names, " "); got != "find get" {
			t.Errorf("tools(%v) = %s, want find get", actions, got)
		}
	}
}

func TestToolsRefuseAnInvalidNumericOption(t *testing.T) {
	for _, option := range []string{"oops", "null", "\"1\""} {
		if _, err := inputSchema([]siren.Field{{Name: "done", Type: "number", Options: []string{option}}}); err == nil {
			t.Fatalf("accepted %q", option)
		}
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

// TestToolsCarryNoFileField is the one allowed difference of the MCP surface: a file (a recording, a picture) is
// sent by the CLI or a browser; the tool keeps the action's other fields.
func TestToolsCarryNoFileField(t *testing.T) {
	list, err := tools([]siren.Action{{Name: "capture", Method: "POST", Href: "/days/{day}/capture", Fields: []siren.Field{
		{Name: "day", Type: "date", In: "path", Required: true}, {Name: "text", Type: "textarea"},
		{Name: "audio", Type: "file", Accept: "audio/*"}}}})
	if err != nil {
		t.Fatal(err)
	}
	props, _ := list[0].schema["properties"].(map[string]any)
	if _, ok := props["audio"]; ok || props["text"] == nil || props["day"] == nil {
		t.Errorf("the tool's properties: %v", props)
	}
}
