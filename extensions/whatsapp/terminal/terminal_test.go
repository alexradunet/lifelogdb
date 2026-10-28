package terminal

import (
	"bytes"
	"io"
	"strings"
	"testing"
	"time"
)

func TestLinesAreMessagesOfTheOwnerAndRepliesAreLines(t *testing.T) {
	at := time.Date(2031, 5, 20, 9, 30, 0, 0, time.UTC)
	var out bytes.Buffer
	c := New(t.Context(), io.NopCloser(strings.NewReader("one\n\n  two  \n")), &out, "123", func() time.Time { return at })
	var got []string
	for m := range c.Messages() {
		if m.Sender != "123" || m.Kind != "text" || !m.At.Equal(at) {
			t.Errorf("message %+v", m)
		}
		got = append(got, m.Text)
	}
	if strings.Join(got, "|") != "one|two" {
		t.Errorf("texts %v", got)
	}
	if err := c.Send(t.Context(), "reply"); err != nil || out.String() != "reply\n" {
		t.Errorf("Send: %q, %v", out.String(), err)
	}
	if err := c.Typing(t.Context(), true); err != nil {
		t.Error(err)
	}
	if err := c.Close(); err != nil {
		t.Error(err)
	}
}
