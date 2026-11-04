package daemon

import (
	"context"
	"fmt"
	"strings"
	"time"

	"google.golang.org/protobuf/proto"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"

	"wp/internal/ipc"
	"wp/internal/store"
)

func (d *Daemon) edit(query, msgID, text string) ipc.Response {
	if err := d.requireLinked(); err != nil {
		return fail(err)
	}
	if d.client == nil || !d.client.IsConnected() {
		return fail(fmt.Errorf("not connected"))
	}
	msgID = strings.TrimSpace(msgID)
	if msgID == "" {
		return fail(fmt.Errorf("no message"))
	}
	if strings.TrimSpace(text) == "" {
		return fail(fmt.Errorf("message is empty"))
	}
	if len(text) > 65536 {
		return fail(fmt.Errorf("message is too long"))
	}
	chat, err := d.resolve(query)
	if err != nil {
		return fail(err)
	}
	msg, err := d.app.Media(chat.JID, msgID)
	if err != nil {
		return fail(err)
	}
	if !msg.FromMe {
		return fail(fmt.Errorf("not your message"))
	}
	if msg.Deleted {
		return fail(fmt.Errorf("already deleted"))
	}
	if msg.Kind != "" {
		return fail(fmt.Errorf("only text messages"))
	}
	jid, err := types.ParseJID(chat.JID)
	if err != nil {
		return fail(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	payload := d.client.BuildEdit(jid, types.MessageID(msg.ID), &waE2E.Message{
		Conversation: proto.String(text),
	})
	if _, err := d.client.SendMessage(ctx, jid, payload); err != nil {
		return fail(err)
	}
	in := store.Incoming{
		Message: store.Message{
			ID:   msg.ID,
			Chat: chat.JID,
			Text: text,
			At:   msg.At,
		},
		Edit: true,
	}
	saved, changed, err := d.app.SaveLive(in)
	if err != nil {
		return fail(err)
	}
	if changed {
		d.broadcast(ipc.Live{
			ID:     msg.ID,
			Chat:   saved.JID,
			Name:   saved.Label(),
			FromMe: true,
			Text:   text,
			At:     msg.At,
			Edited: true,
		})
	}
	out := msg
	out.Text = text
	out.Edited = true
	return ipc.Response{OK: true, Chat: &saved, Sent: &out}
}
