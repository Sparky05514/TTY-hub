package daemon

import (
	"testing"

	"go.mau.fi/whatsmeow/proto/waCommon"
	"google.golang.org/protobuf/proto"
)

func TestReactionTargetID(t *testing.T) {
	id, ok := reactionTargetID(nil)
	if ok || id != "" {
		t.Fatalf("nil key = %q %v", id, ok)
	}
	id, ok = reactionTargetID(&waCommon.MessageKey{ID: proto.String("  ")})
	if ok {
		t.Fatalf("blank id accepted")
	}
	id, ok = reactionTargetID(&waCommon.MessageKey{ID: proto.String("ABC123")})
	if !ok || id != "ABC123" {
		t.Fatalf("got %q %v", id, ok)
	}
}
