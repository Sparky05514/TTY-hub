package msgtext

import (
	"testing"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"
)

func TestFormatSize(t *testing.T) {
	if got := FormatSize(245760); got != "240 KB" {
		t.Fatalf("size = %q", got)
	}
	if got := FormatSize(500); got != "500 B" {
		t.Fatalf("bytes = %q", got)
	}
}

func TestInspectImageAndVoice(t *testing.T) {
	text, att, _ := Inspect(&waE2E.Message{ImageMessage: &waE2E.ImageMessage{
		Caption:    proto.String("look"),
		FileLength: proto.Uint64(245760),
		Mimetype:   proto.String("image/jpeg"),
		DirectPath: proto.String("/v/t62/image"),
		MediaKey:   []byte{1, 2, 3},
	}}, false)
	if text != "image · 240 KB" || att == nil || !att.Eager || att.Caption != "look" || att.Ref == "" {
		t.Fatalf("image = %q %#v", text, att)
	}

	text, att, _ = Inspect(&waE2E.Message{AudioMessage: &waE2E.AudioMessage{
		PTT:        proto.Bool(true),
		DirectPath: proto.String("/v/voice"),
		MediaKey:   []byte{9},
	}}, false)
	if text != "voice message" || att == nil || att.Kind != "voice" || !att.Eager {
		t.Fatalf("voice = %q %#v", text, att)
	}

	text, att, _ = Inspect(&waE2E.Message{VideoMessage: &waE2E.VideoMessage{
		FileLength: proto.Uint64(2 << 20),
	}}, false)
	if att == nil || att.Eager || att.State() != "unavailable" || text != "video · 2.0 MB · unavailable" {
		t.Fatalf("video = %q %#v", text, att)
	}
}

func TestInspectViewOnceKeepsTheFile(t *testing.T) {
	text, att, _ := Inspect(&waE2E.Message{ViewOnceMessage: &waE2E.FutureProofMessage{
		Message: &waE2E.Message{ImageMessage: &waE2E.ImageMessage{
			DirectPath: proto.String("/v/secret"),
			MediaKey:   []byte{1},
		}},
	}}, false)
	if text != "view once photo" || att == nil || att.Eager || !att.ViewOnce || att.Kind != "image" || att.Ref == "" {
		t.Fatalf("wrapped = %q %#v", text, att)
	}
	text, att, _ = Inspect(&waE2E.Message{ImageMessage: &waE2E.ImageMessage{
		DirectPath: proto.String("/v/secret"),
		MediaKey:   []byte{1},
		FileLength: proto.Uint64(245760),
	}}, true)
	if text != "view once photo · 240 KB" || att == nil || att.Eager || att.Ref == "" {
		t.Fatalf("unwrapped = %q %#v", text, att)
	}
	text, att, _ = Inspect(&waE2E.Message{VideoMessage: &waE2E.VideoMessage{
		DirectPath: proto.String("/v/video"),
		MediaKey:   []byte{2},
	}}, true)
	if text != "view once video" || att == nil || att.Kind != "video" || att.Eager {
		t.Fatalf("video = %q %#v", text, att)
	}
	text, att, _ = Inspect(&waE2E.Message{ViewOnceMessageV2: &waE2E.FutureProofMessage{}}, false)
	if text != "view once" || att != nil {
		t.Fatalf("empty = %q %#v", text, att)
	}
}

func TestInspectQuote(t *testing.T) {
	text, att, quote := Inspect(&waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{
		Text: proto.String("on it"),
		ContextInfo: &waE2E.ContextInfo{
			StanzaID:    proto.String("m1"),
			Participant: proto.String("123@lid"),
			QuotedMessage: &waE2E.Message{ImageMessage: &waE2E.ImageMessage{
				Caption:    proto.String("look"),
				FileLength: proto.Uint64(245760),
			}},
		},
	}}, false)
	if text != "on it" || att != nil || quote == nil || quote.ID != "m1" || quote.Text != "image · 240 KB" {
		t.Fatalf("quote = %q %#v %#v", text, att, quote)
	}
}
