package msgtext

import (
	"strings"

	"go.mau.fi/whatsmeow/proto/waE2E"
)

// RevokeID returns the id of a message this payload deletes for everyone.
func RevokeID(m *waE2E.Message) (string, bool) {
	if m == nil {
		return "", false
	}
	p := m.GetProtocolMessage()
	if p == nil || p.Type == nil || *p.Type != waE2E.ProtocolMessage_REVOKE {
		return "", false
	}
	id := p.GetKey().GetID()
	if id == "" {
		return "", false
	}
	return id, true
}

// Body returns the text worth showing for a direct message.
// Empty means the event is not a user-visible message.
func Body(m *waE2E.Message) string {
	if m == nil {
		return ""
	}
	viewOnce := m.GetViewOnceMessage() != nil || m.GetViewOnceMessageV2() != nil || m.GetViewOnceMessageV2Extension() != nil
	m = unwrap(m)
	if m == nil || m.GetProtocolMessage() != nil || m.GetReactionMessage() != nil {
		return ""
	}
	text := content(m)
	if text == "" {
		return ""
	}
	if viewOnce {
		return "[view once] " + text
	}
	return text
}

func unwrap(m *waE2E.Message) *waE2E.Message {
	for i := 0; i < 8 && m != nil; i++ {
		switch {
		case m.GetDeviceSentMessage().GetMessage() != nil:
			m = m.GetDeviceSentMessage().GetMessage()
		case m.GetEphemeralMessage().GetMessage() != nil:
			m = m.GetEphemeralMessage().GetMessage()
		case m.GetViewOnceMessage().GetMessage() != nil:
			m = m.GetViewOnceMessage().GetMessage()
		case m.GetViewOnceMessageV2().GetMessage() != nil:
			m = m.GetViewOnceMessageV2().GetMessage()
		case m.GetViewOnceMessageV2Extension().GetMessage() != nil:
			m = m.GetViewOnceMessageV2Extension().GetMessage()
		case m.GetLottieStickerMessage().GetMessage() != nil:
			m = m.GetLottieStickerMessage().GetMessage()
		case m.GetDocumentWithCaptionMessage().GetMessage() != nil:
			m = m.GetDocumentWithCaptionMessage().GetMessage()
		case m.GetEditedMessage().GetMessage() != nil:
			m = m.GetEditedMessage().GetMessage()
		default:
			return m
		}
	}
	return m
}

func content(m *waE2E.Message) string {
	if s := strings.TrimSpace(m.GetConversation()); s != "" {
		return m.GetConversation()
	}
	if s := m.GetExtendedTextMessage().GetText(); strings.TrimSpace(s) != "" {
		return s
	}
	if im := m.GetImageMessage(); im != nil {
		return prefix("[image]", im.GetCaption())
	}
	if vm := m.GetVideoMessage(); vm != nil {
		return prefix("[video]", vm.GetCaption())
	}
	if vm := m.GetPtvMessage(); vm != nil {
		return prefix("[video]", vm.GetCaption())
	}
	if dm := m.GetDocumentMessage(); dm != nil {
		label := dm.GetFileName()
		if label == "" {
			label = dm.GetCaption()
		}
		return prefix("[file]", label)
	}
	if a := m.GetAudioMessage(); a != nil {
		if a.GetPTT() {
			return "[voice message]"
		}
		return "[audio]"
	}
	if m.GetStickerMessage() != nil {
		return "[sticker]"
	}
	if c := m.GetContactMessage(); c != nil {
		return prefix("[contact]", c.GetDisplayName())
	}
	if m.GetLocationMessage() != nil || m.GetLiveLocationMessage() != nil {
		return "[location]"
	}
	if p := m.GetPollCreationMessage(); p != nil {
		return prefix("[poll]", p.GetName())
	}
	if p := m.GetPollCreationMessageV2(); p != nil {
		return prefix("[poll]", p.GetName())
	}
	if p := m.GetPollCreationMessageV3(); p != nil {
		return prefix("[poll]", p.GetName())
	}
	if b := m.GetButtonsResponseMessage(); b != nil {
		if s := b.GetSelectedDisplayText(); strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}

func prefix(kind, extra string) string {
	extra = strings.TrimSpace(extra)
	if extra == "" {
		return kind
	}
	return kind + " " + extra
}
