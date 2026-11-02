package daemon

import (
	"strings"

	"google.golang.org/protobuf/proto"

	"go.mau.fi/whatsmeow/proto/waE2E"

	"wp/internal/msgtext"
	"wp/internal/store"
)

func (d *Daemon) quoted(chat, id string) (*store.Message, error) {
	if strings.TrimSpace(id) == "" {
		return nil, nil
	}
	msg, err := d.app.Media(chat, id)
	if err != nil {
		return nil, err
	}
	return &msg, nil
}

func textPayload(text string, quoted *store.Message) *waE2E.Message {
	if quoted == nil {
		return &waE2E.Message{Conversation: proto.String(text)}
	}
	return &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{
		Text:        proto.String(text),
		ContextInfo: quoteContext(quoted),
	}}
}

func quoteContext(q *store.Message) *waE2E.ContextInfo {
	snippet := replySnippet(q)
	participant := q.Sender
	if participant == "" {
		participant = q.Chat
	}
	return &waE2E.ContextInfo{
		StanzaID:      proto.String(q.ID),
		Participant:   proto.String(participant),
		QuotedMessage: &waE2E.Message{Conversation: proto.String(snippet)},
	}
}

func replyFields(q *store.Message) (string, string) {
	if q == nil {
		return "", ""
	}
	return q.ID, replySnippet(q)
}

func replySnippet(q *store.Message) string {
	snippet := strings.TrimSpace(q.Preview())
	if snippet == "" {
		snippet = "message"
	}
	return msgtext.Snip(strings.Join(strings.Fields(snippet), " "), 80)
}
