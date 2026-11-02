package msgtext

import (
	"testing"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"
)

func TestBody(t *testing.T) {
	cases := []struct {
		name string
		msg  *waE2E.Message
		want string
	}{
		{name: "text", msg: &waE2E.Message{Conversation: proto.String("hello")}, want: "hello"},
		{
			name: "image",
			msg:  &waE2E.Message{ImageMessage: &waE2E.ImageMessage{Caption: proto.String("look")}},
			want: "[image] look",
		},
		{
			name: "voice",
			msg:  &waE2E.Message{AudioMessage: &waE2E.AudioMessage{PTT: proto.Bool(true)}},
			want: "[voice message]",
		},
		{
			name: "reaction",
			msg:  &waE2E.Message{ReactionMessage: &waE2E.ReactionMessage{Text: proto.String("👍")}},
			want: "",
		},
		{
			name: "ephemeral",
			msg: &waE2E.Message{EphemeralMessage: &waE2E.FutureProofMessage{
				Message: &waE2E.Message{Conversation: proto.String("secret")},
			}},
			want: "secret",
		},
		{
			name: "view once",
			msg: &waE2E.Message{ViewOnceMessage: &waE2E.FutureProofMessage{
				Message: &waE2E.Message{ImageMessage: &waE2E.ImageMessage{}},
			}},
			want: "[view once] [image]",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Body(tc.msg); got != tc.want {
				t.Fatalf("Body() = %q, want %q", got, tc.want)
			}
		})
	}
	if Body(nil) != "" {
		t.Fatal("nil message should be empty")
	}
	if _, ok := RevokeID(&waE2E.Message{ProtocolMessage: &waE2E.ProtocolMessage{}}); ok {
		t.Fatal("a protocol message with no type is not a revoke")
	}
}
