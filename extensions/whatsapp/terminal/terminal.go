// Package terminal is a chat on a terminal: a line of input is a message of the owner, a reply is written to the
// output. The owner tries the agent without a phone, and the end-to-end test drives the real programs with it.
package terminal

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"lifelog-whatsapp/agent"
)

// maxLine is the longest line of input.
const maxLine = 1 << 20

// Sender is the sender of every line, and the owner that the agent answers on a terminal: no number is needed.
const Sender = "terminal"

// Chat is the chat of one owner on a terminal.
type Chat struct {
	in   io.ReadCloser
	out  io.Writer
	mu   sync.Mutex // one reply at a time on out
	msgs chan agent.Message
}

// New starts to read lines from in. Each line that is not blank is a text of owner, at the time now gives.
// The channel of Messages closes when in ends. The reading goroutine is the one goroutine of the program that is
// not awaited: a read of a terminal or a pipe blocks until a line comes, and a close of the file does not end it
// (the file is not in the poller of Go). It ends with the process, which is what an interrupt means; the lines it
// could still read go to a channel that nobody reads then.
func New(ctx context.Context, in io.ReadCloser, out io.Writer, owner string, now func() time.Time) *Chat {
	c := &Chat{in: in, out: out, msgs: make(chan agent.Message)}
	go func() {
		defer close(c.msgs)
		sc := bufio.NewScanner(in)
		sc.Buffer(nil, maxLine)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" {
				continue
			}
			select {
			case c.msgs <- agent.Message{Sender: owner, Text: line, Kind: agent.KindText, At: now()}:
			case <-ctx.Done():
				return
			}
		}
	}()
	return c
}

// Messages gives the lines of input.
func (c *Chat) Messages() <-chan agent.Message { return c.msgs }

// Send writes a reply and a newline.
func (c *Chat) Send(_ context.Context, text string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, err := fmt.Fprintln(c.out, text)
	return err
}

// Typing does nothing: a terminal has no typing mark.
func (c *Chat) Typing(context.Context, bool) error { return nil }

// Close closes the input and returns the error of the close. It does not wait for the reading (New says why).
func (c *Chat) Close() error { return c.in.Close() }
