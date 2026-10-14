package api

import (
	"reflect"
	"testing"
	"time"
)

func TestParseTurns(t *testing.T) {
	for _, tc := range []struct {
		name, body, preamble, compacted string
		turns                           []Turn
	}{
		{name: "empty", body: ""},
		{name: "a compacted page: the trailer is no part of the last reply",
			body:      "**Owner** (09.05): Hi\n\n**lifelog:** Hello.\n\n**Compacted** into [[2031-05-13]]\n",
			compacted: "2031-05-13",
			turns:     []Turn{{At: "09.05", Question: "Hi", Steps: []Step{}, Reply: "Hello."}}},
		{name: "prose and the trailer", body: "Notes.\n\n**Compacted** into [[2031-05-13]]", preamble: "Notes.", compacted: "2031-05-13"},
		{name: "a trailer the owner moved into the middle",
			body:      "**Owner** (09.05): Hi\n**Compacted** into [[2031-05-13]]\n\n**lifelog:** Hello.\n",
			compacted: "2031-05-13",
			turns:     []Turn{{At: "09.05", Question: "Hi", Steps: []Step{}, Reply: "Hello."}}},
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
			preamble, turns, compacted := ParseTurns(tc.body)
			if preamble != tc.preamble || !reflect.DeepEqual(turns, tc.turns) || compacted != tc.compacted {
				t.Fatalf("ParseTurns =\n%q %+v %q\nwant\n%q %+v %q", preamble, turns, compacted, tc.preamble, tc.turns, tc.compacted)
			}
		})
	}
}

func TestFormatTurnReadsBack(t *testing.T) {
	at := time.Date(2031, 5, 14, 7, 3, 0, 0, time.UTC)
	steps := []Step{{Tool: "compact", Href: "/days/2031-05-13"}, {Tool: "compact", Error: "409: stale_version"},
		{Tool: "capture", Href: "/days/2031-05-14"}, {Tool: "link", Error: "404: no page\ntitled X"}}
	body := FormatTurn(at, " Write it ", steps, "Written.", "") + "\n" + FormatTurn(at, "Again", nil, "", "the model answered 502") +
		"\n" + FormatCompacted("2031-05-14")
	_, turns, compacted := ParseTurns(body)
	want := []Turn{
		{At: "07.03", Question: "Write it", Reply: "Written.", Steps: []Step{{Tool: "compact", Href: "/days/2031-05-13"}, {Tool: "compact", Error: "409: stale_version"},
			{Tool: "capture", Href: "/days/2031-05-14"}, {Tool: "link", Error: "404: no page"}}},
		{At: "07.03", Question: "Again", Steps: []Step{}, Error: "the model answered 502"},
	}
	if !reflect.DeepEqual(turns, want) || compacted != "2031-05-14" {
		t.Fatalf("turns =\n%+v %q\nwant\n%+v\nbody:\n%s", turns, compacted, want, body)
	}
}

func TestConversationDay(t *testing.T) {
	for title, want := range map[string]string{
		"Conversation 2031-05-13":          "2031-05-13",
		"Conversation 2031-05-13 09.12.33": "2031-05-13", // the title of a conversation before plan 106
		"Conversation notes":               "",
		"Conversation 2031-02-30":          "", // not a day
		"Conversations":                    "",
	} {
		if got := conversationDay(title); got != want {
			t.Errorf("conversationDay(%q) = %q, want %q", title, got, want)
		}
	}
}
