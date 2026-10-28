package whatsapp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"strings"
	"sync"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"

	"lifelog-whatsapp/agent"
)

// libraryLog is the log of whatsmeow: its warnings and errors only.
func libraryLog() waLog.Logger { return waLog.Stdout("whatsmeow", "WARN", false) }

// Login pairs the device of s as a linked device of the agent's number. It draws each QR code on out and returns
// when the owner of the phone scans it (nil), when the codes time out, or when ctx ends. A store that is paired
// already is an error.
func Login(ctx context.Context, s *Session, out io.Writer) error {
	if n := s.Number(); n != "" {
		return fmt.Errorf("already paired as %s; delete %s to pair again", n, s.Path())
	}
	cl := whatsmeow.NewClient(s.dev, libraryLog())
	codes, err := cl.GetQRChannel(ctx)
	if err != nil {
		return fmt.Errorf("pairing: %w", err)
	}
	if err := cl.Connect(); err != nil {
		return fmt.Errorf("pairing: %w", err)
	}
	defer cl.Disconnect()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case item, ok := <-codes:
			if !ok {
				return errors.New("pairing: the channel of codes closed with no result")
			}
			switch item.Event {
			case "code":
				if err := drawCode(out, item.Code); err != nil {
					return err
				}
			case "success":
				return nil
			case "timeout":
				return errors.New("pairing: nobody scanned the code in time")
			default:
				return fmt.Errorf("pairing: %s %v", item.Event, item.Error)
			}
		}
	}
}

// drawCode writes the instruction and the code, black on white on any terminal theme.
func drawCode(out io.Writer, code string) error {
	lines, err := Render(code)
	if err != nil {
		return err
	}
	var b strings.Builder
	b.WriteString("\nOpen WhatsApp on the agent's phone, Linked devices, Link a device, and scan this.\n\n")
	for _, l := range lines {
		b.WriteString("\x1b[30;107m" + l + "\x1b[0m\n")
	}
	_, err = io.WriteString(out, b.String())
	return err
}

// Chat is the chat of the owner on WhatsApp.
type Chat struct {
	cl       *whatsmeow.Client
	owner    types.JID
	after    time.Time
	log      *log.Logger
	msgs     chan agent.Message
	handler  uint32
	done     chan struct{}
	stop     sync.Once
	loggedMu sync.Once
	out      chan struct{}
	// ctx is the context of the run. whatsmeow calls the handler with no context, so the handler takes this one.
	ctx context.Context
}

// Connect connects the paired device of s and starts to pass the messages of owner (digits) that are newer than
// after. Close ends it. The device must be paired.
func Connect(ctx context.Context, s *Session, owner string, after time.Time, l *log.Logger) (*Chat, error) {
	if s.Number() == "" {
		return nil, ErrNotPaired
	}
	c := &Chat{
		cl: whatsmeow.NewClient(s.dev, libraryLog()), owner: types.NewJID(owner, types.DefaultUserServer), after: after, log: l,
		msgs: make(chan agent.Message, 32), done: make(chan struct{}), out: make(chan struct{}), ctx: ctx,
	}
	c.handler = c.cl.AddEventHandler(c.handle)
	if err := c.cl.Connect(); err != nil {
		c.cl.RemoveEventHandler(c.handler)
		return nil, fmt.Errorf("connect: %w", err)
	}
	return c, nil
}

// LoggedOut is closed when the owner unlinks the device on the phone.
func (c *Chat) LoggedOut() <-chan struct{} { return c.out }

// Close disconnects. The channel of Messages stays open: the agent ends with its context.
func (c *Chat) Close() {
	c.stop.Do(func() {
		c.cl.RemoveEventHandler(c.handler)
		c.cl.Disconnect()
		close(c.done)
	})
}

// Messages gives the messages that Filter lets through.
func (c *Chat) Messages() <-chan agent.Message { return c.msgs }

// Send sends a text to the owner.
func (c *Chat) Send(ctx context.Context, text string) error {
	_, err := c.cl.SendMessage(ctx, c.owner, &waE2E.Message{Conversation: proto.String(text)})
	return err
}

// Typing shows, or hides, that the agent is writing.
func (c *Chat) Typing(ctx context.Context, on bool) error {
	state := types.ChatPresencePaused
	if on {
		state = types.ChatPresenceComposing
	}
	return c.cl.SendChatPresence(ctx, c.owner, state, types.ChatPresenceMediaText)
}

// handle is the handler of the events of whatsmeow.
func (c *Chat) handle(evt any) {
	switch e := evt.(type) {
	case *events.LoggedOut:
		c.loggedMu.Do(func() { close(c.out) })
	case *events.Message:
		if e.Info.Sender.Server == types.HiddenUserServer && e.Info.SenderAlt.User == "" {
			if pn, err := c.cl.Store.LIDs.GetPNForLID(c.ctx, e.Info.Sender); err == nil {
				e.Info.SenderAlt = pn
			}
		}
		m, ok := Filter(e, c.after)
		if !ok {
			return
		}
		if m.Sender == c.owner.User { // the owner sees that the message arrived
			rctx, cancel := context.WithTimeout(c.ctx, 10*time.Second)
			if err := c.cl.MarkRead(rctx, []types.MessageID{e.Info.ID}, time.Now(), e.Info.Chat, e.Info.Sender); err != nil {
				c.log.Printf("mark read failed: %v", err)
			}
			cancel()
		}
		select {
		case c.msgs <- m:
		case <-c.done:
		case <-c.ctx.Done():
		}
	}
}
