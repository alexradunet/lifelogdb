package whatsapp

import (
	"time"

	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	"lifelog-whatsapp/agent"
)

// Filter turns an event into a message of the chat, or says that the agent must not see it. A message is passed
// on when it is not from this device, not from a history sync, not an edit, not a group message and newer than
// after (the newest turn of the transcript, so that a restart does not answer again what it answered). A reaction
// and a protocol message (a delete, a read marker) are not messages. A message that is not text is passed on with
// its kind, so that the agent can refuse it with a line.
func Filter(evt *events.Message, after time.Time) (agent.Message, bool) {
	info := evt.Info
	switch {
	case info.IsFromMe, info.IsGroup, evt.SourceWebMsg != nil, evt.IsEdit:
		return agent.Message{}, false
	case !info.Timestamp.After(after):
		return agent.Message{}, false
	}
	m := evt.Message
	if m.GetReactionMessage() != nil || m.GetProtocolMessage() != nil {
		return agent.Message{}, false
	}
	out := agent.Message{Sender: senderNumber(info), At: info.Timestamp, Kind: agent.KindOther}
	switch {
	case m.GetConversation() != "":
		out.Kind, out.Text = agent.KindText, m.GetConversation()
	case m.GetExtendedTextMessage() != nil:
		out.Kind, out.Text = agent.KindText, m.GetExtendedTextMessage().GetText()
	case m.GetImageMessage() != nil:
		out.Kind = agent.KindImage
	case m.GetAudioMessage() != nil:
		out.Kind = agent.KindAudio
	case m.GetDocumentMessage() != nil:
		out.Kind = agent.KindDocument
	}
	return out, true
}

// senderNumber is the number of the sender, digits only. A sender can be a hidden id (a lid): then the number is
// the alternative address, when WhatsApp gave one.
func senderNumber(info types.MessageInfo) string {
	if info.Sender.Server == types.HiddenUserServer && info.SenderAlt.User != "" {
		return info.SenderAlt.User
	}
	return info.Sender.User
}
