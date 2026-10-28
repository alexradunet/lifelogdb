package api

import (
	"reflect"
	"testing"
)

// TestParseTurnsReadsAHandWrittenBody: the vectors are written by hand, as an agent in another language would write
// them, and not made by the program's own formatter. They hold an owner turn, a lifelog turn with two tool lines, a
// reply of two paragraphs (the second is a free paragraph), a free paragraph that the owner added, tool
// lines that no lifelog turn follows, and CRLF line ends.
func TestParseTurnsReadsAHandWrittenBody(t *testing.T) {
	const day = "2031-05-14"
	for _, tc := range []struct {
		name, body string
		want       []Turn
	}{
		{"an empty body", "", []Turn{}},
		{"the first line only", "[[2031-05-14]]", []Turn{}},
		{"the whole page", "[[2031-05-14]]\n\n**Owner** (09.07): How did I sleep?\n\n" +
			"- `get`\n- `search`: no hits\n\n**lifelog** (09.08): Seven hours.\n\nA second paragraph.\n\n" +
			"A note the owner typed.\n\n**Owner** (10.00): Thanks.\nOn two lines.",
			[]Turn{
				{Who: "owner", At: "09.07", Text: "How did I sleep?"},
				{Who: "lifelog", At: "09.08", Text: "Seven hours.", Tools: []string{"- `get`", "- `search`: no hits"}},
				{Who: "text", Text: "A second paragraph."},
				{Who: "text", Text: "A note the owner typed."},
				{Who: "owner", At: "10.00", Text: "Thanks.\nOn two lines."},
			}},
		{"CRLF line ends", "[[2031-05-14]]\r\n\r\n**Owner** (09.07): Hello\r\n\r\n- `get`\r\n\r\n**lifelog** (09.08): Hi\r\n",
			[]Turn{
				{Who: "owner", At: "09.07", Text: "Hello"},
				{Who: "lifelog", At: "09.08", Text: "Hi", Tools: []string{"- `get`"}},
			}},
		{"tool lines before an owner turn are a free paragraph", "- `get`\n\n**Owner** (09.07): Hello",
			[]Turn{{Who: "text", Text: "- `get`"}, {Who: "owner", At: "09.07", Text: "Hello"}}},
		{"a mark that is not a turn", "**Owner** 09.07: no brackets\n\n**lifelog** (9.7): bad time",
			[]Turn{{Who: "text", Text: "**Owner** 09.07: no brackets"}, {Who: "text", Text: "**lifelog** (9.7): bad time"}}},
		{"a first line that names another day is a paragraph", "[[2031-05-13]]", []Turn{{Who: "text", Text: "[[2031-05-13]]"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ParseTurns(day, tc.body); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %#v\nwant %#v", got, tc.want)
			}
		})
	}
}

func TestConversationDayIsReadFromTheTitle(t *testing.T) {
	for title, want := range map[string]string{
		"Conversation 2031-05-14": "2031-05-14", "Conversation 2031-05-14 09.07.00": "", "Conversation 2031-13-14": "",
		"conversation 2031-05-14": "", "Conversation": "", "Notes 2031-05-14": "",
	} {
		if got := conversationDay(title); got != want {
			t.Errorf("conversationDay(%q) = %q, want %q", title, got, want)
		}
	}
}
