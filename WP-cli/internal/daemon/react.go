package daemon

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waWeb"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	"wp/internal/direct"
	"wp/internal/ipc"
	"wp/internal/store"
)

func (d *Daemon) react(query, msgID, emoji string) ipc.Response {
	if err := d.requireLinked(); err != nil {
		return fail(err)
	}
	if !d.client.IsConnected() {
		return fail(fmt.Errorf("not connected"))
	}
	msgID = strings.TrimSpace(msgID)
	if msgID == "" {
		return fail(fmt.Errorf("message id is required"))
	}
	chat, err := d.resolve(query)
	if err != nil {
		return fail(err)
	}
	msg, err := d.app.Media(chat.JID, msgID)
	if err != nil {
		return fail(err)
	}
	chatJID, err := types.ParseJID(chat.JID)
	if err != nil {
		return fail(err)
	}
	sender, err := d.reactionTargetSender(msg)
	if err != nil {
		return fail(err)
	}
	emoji = strings.TrimSpace(emoji)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	resp, err := d.client.SendMessage(ctx, chatJID, d.client.BuildReaction(chatJID, sender, types.MessageID(msg.ID), emoji))
	if err != nil {
		return fail(err)
	}
	at := resp.Timestamp
	if at.IsZero() {
		at = time.Now()
	}
	reactor := ownJID(d.client)
	changed, err := d.app.UpsertReaction(chat.JID, msg.ID, reactor, true, emoji, at)
	if err != nil {
		return fail(err)
	}
	list := d.reactionList(chat.JID, msg.ID)
	if changed {
		d.broadcast(ipc.Live{
			ID:        msg.ID,
			Chat:      chat.JID,
			Name:      chat.Label(),
			FromMe:    true,
			Text:      emoji,
			At:        at,
			Kind:      "reaction",
			Reactions: list,
		})
	}
	return ipc.Response{OK: true, Chat: &chat, Messages: []store.Message{{
		ID: msg.ID, Chat: chat.JID, Reactions: list,
	}}}
}

func (d *Daemon) reactionTargetSender(msg store.Message) (types.JID, error) {
	if msg.FromMe {
		if d.client == nil || d.client.Store == nil || d.client.Store.ID == nil {
			return types.EmptyJID, fmt.Errorf("not linked")
		}
		return direct.Canonical(*d.client.Store.ID), nil
	}
	if msg.Sender == "" {
		return types.EmptyJID, fmt.Errorf("message sender unknown")
	}
	j, err := types.ParseJID(msg.Sender)
	if err != nil {
		return types.EmptyJID, err
	}
	return direct.Canonical(j), nil
}

func (d *Daemon) onReaction(v *events.Message) {
	if v == nil || v.Message == nil || !direct.Allowed(v.Info.Chat) {
		return
	}
	rm := v.Message.GetReactionMessage()
	if rm == nil {
		return
	}
	targetID, ok := reactionTargetID(rm.GetKey())
	if !ok {
		return
	}
	chat := direct.Canonical(v.Info.Chat)
	reactor := direct.Canonical(v.Info.Sender).String()
	if reactor == "" {
		reactor = ownJID(d.client)
	}
	at := v.Info.Timestamp
	if ts := rm.GetSenderTimestampMS(); ts > 0 {
		at = time.UnixMilli(ts)
	}
	if at.IsZero() {
		at = time.Now()
	}
	emoji := rm.GetText()
	changed, err := d.app.UpsertReaction(chat.String(), targetID, reactor, v.Info.IsFromMe, emoji, at)
	if err != nil {
		fmt.Fprintf(os.Stderr, "reaction: %v\n", err)
		return
	}
	if !changed {
		return
	}
	name := chat.User
	if c, ok, _ := d.app.Chat(chat.String()); ok {
		name = c.Label()
	}
	d.broadcast(ipc.Live{
		ID:        targetID,
		Chat:      chat.String(),
		Name:      name,
		FromMe:    v.Info.IsFromMe,
		Text:      emoji,
		At:        at,
		Kind:      "reaction",
		Reactions: d.reactionList(chat.String(), targetID),
	})
}

func (d *Daemon) reactionList(chat, id string) []store.Reaction {
	byID, err := d.app.ReactionsFor(chat, []string{id})
	if err != nil {
		return []store.Reaction{}
	}
	list := byID[id]
	if list == nil {
		return []store.Reaction{}
	}
	return list
}

func reactionTargetID(key *waCommon.MessageKey) (string, bool) {
	if key == nil {
		return "", false
	}
	id := strings.TrimSpace(key.GetID())
	return id, id != ""
}

func applyHistoryReactions(app *store.Store, chat, own string, info *waWeb.WebMessageInfo) {
	if app == nil || info == nil || chat == "" {
		return
	}
	targetID := ""
	if key := info.GetKey(); key != nil {
		targetID = strings.TrimSpace(key.GetID())
	}
	if targetID == "" {
		return
	}
	for _, r := range info.GetReactions() {
		if r == nil {
			continue
		}
		emoji := r.GetText()
		reactor := ""
		fromMe := false
		if key := r.GetKey(); key != nil {
			fromMe = key.GetFromMe()
			if p := strings.TrimSpace(key.GetParticipant()); p != "" {
				if j, err := types.ParseJID(p); err == nil {
					reactor = direct.Canonical(j).String()
				}
			}
			if reactor == "" && fromMe && own != "" {
				reactor = own
			}
			if reactor == "" && !fromMe {
				if remote := strings.TrimSpace(key.GetRemoteJID()); remote != "" {
					if j, err := types.ParseJID(remote); err == nil && direct.Is(j) {
						reactor = direct.Canonical(j).String()
					}
				}
			}
		}
		if reactor == "" {
			continue
		}
		at := time.UnixMilli(r.GetSenderTimestampMS())
		if at.IsZero() {
			at = time.Now()
		}
		_, _ = app.UpsertReaction(chat, targetID, reactor, fromMe, emoji, at)
	}
}
