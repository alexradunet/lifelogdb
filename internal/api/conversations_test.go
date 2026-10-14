package api

import (
	"reflect"
	"testing"
	"time"
)

func TestParseTurns(t *testing.T) {
	for _, tc := range []struct {
		name, body, preamble string
		turns                []Turn
	}{
		{name: "empty", body: ""},
		{name: "prose only", body: "Notes the owner wrote.\n", preamble: "Notes the owner wrote."},
		{name: "a turn with tools",
			body: "**Owner** (09.05): What did I eat?\n\n- `get_day` → [/days/2031-05-14](/days/2031-05-14)\n- `record`: 422: no metric weight\n- `search`\n\n**lifelog:** Soup.\n\nAnd bread.\n",
			turns: []Turn{{At: "09.05", Question: "What did I eat?", Reply: "Soup.\n\nAnd bread.", Steps: []Step{
				{Tool: "get_day", Href: "/days/2031-05-14"}, {Tool: "record", Error: "422: no metric weight"}, {Tool: "search"}}}}},
		{name: "a failed turn, then one more",
			body:     "Before.\n\n**Owner** (10.00): Loop\n\n- `get`\n\n**lifelog** did not answer: the model answered 500\n\n**Owner** (10.01): Again\nwith two lines\n\n**lifelog:** Done.",
			preamble: "Before.",
			turns: []Turn{{At: "10.00", Question: "Loop", Steps: []Step{{Tool: "get"}}, Error: "the model answered 500"},
				{At: "10.01", Question: "Again\nwith two lines", Steps: []Step{}, Reply: "Done."}}},
		{name: "a line the owner added stays in the question",
			body:  "**Owner** (11.11): Hi\n- not a tool line\n\n**lifelog:** Hello.",
			turns: []Turn{{At: "11.11", Question: "Hi\n- not a tool line", Steps: []Step{}, Reply: "Hello."}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			preamble, turns := ParseTurns(tc.body)
			if preamble != tc.preamble || !reflect.DeepEqual(turns, tc.turns) {
				t.Fatalf("ParseTurns =\n%q %+v\nwant\n%q %+v", preamble, turns, tc.preamble, tc.turns)
			}
		})
	}
}

func TestFormatTurnReadsBack(t *testing.T) {
	at := time.Date(2031, 5, 14, 7, 3, 0, 0, time.UTC)
	steps := []Step{{Tool: "capture", Href: "/days/2031-05-14"}, {Tool: "link", Error: "404: no page\ntitled X"}}
	body := FormatTurn(at, " Write it ", steps, "Written.", "") + "\n" + FormatTurn(at, "Again", nil, "", "the model answered 502")
	_, turns := ParseTurns(body)
	want := []Turn{
		{At: "07.03", Question: "Write it", Reply: "Written.", Steps: []Step{{Tool: "capture", Href: "/days/2031-05-14"}, {Tool: "link", Error: "404: no page"}}},
		{At: "07.03", Question: "Again", Steps: []Step{}, Error: "the model answered 502"},
	}
	if !reflect.DeepEqual(turns, want) {
		t.Fatalf("turns =\n%+v\nwant\n%+v\nbody:\n%s", turns, want, body)
	}
}
