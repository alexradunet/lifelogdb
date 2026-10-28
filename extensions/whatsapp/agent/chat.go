// Package agent answers the owner's messages with a model that calls the actions of the lifelog API. A chat (WhatsApp
// or the terminal) brings the messages. The agent answers one message at a time, in order.
package agent

import (
	"context"
	"time"
)

// Message kinds. Kind is KindText, or what the message was when it was not text.
const (
	KindText     = "text"
	KindImage    = "image"
	KindAudio    = "audio"
	KindDocument = "document"
	KindOther    = "other"
)

// Message is one message that arrived in a chat.
type Message struct {
	Sender string    // the number, digits only
	Text   string    // the text; "" when Kind is not KindText
	Kind   string    // KindText, or what it was
	At     time.Time // when the sender sent it
}

// Chat is the place where the owner talks to the agent.
type Chat interface {
	// Messages gives the messages that arrive. The channel closes when no more can arrive.
	Messages() <-chan Message
	// Send sends a reply to the owner.
	Send(ctx context.Context, text string) error
	// Typing shows, or hides, that the agent is writing.
	Typing(ctx context.Context, on bool) error
}
