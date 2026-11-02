package direct

import (
	"testing"

	"go.mau.fi/whatsmeow/types"
)

func TestIs(t *testing.T) {
	if !Is(types.NewJID("15551212000", types.DefaultUserServer)) {
		t.Fatal("phone user should be a direct chat")
	}
	if !Is(types.NewJID("15551212000", types.LegacyUserServer)) {
		t.Fatal("legacy user should be a direct chat")
	}
	if !Is(types.NewJID("12345", types.HiddenUserServer)) {
		t.Fatal("lid should be a direct chat")
	}
	if Is(types.NewJID("120363", types.GroupServer)) {
		t.Fatal("group should be excluded")
	}
	if Is(types.NewJID("120363", types.NewsletterServer)) {
		t.Fatal("channel should be excluded")
	}
	if Is(types.StatusBroadcastJID) {
		t.Fatal("status broadcast should be excluded")
	}
	if Is(types.MetaAIJID) {
		t.Fatal("bot should be excluded")
	}
}

func TestAllowedIncludesGroups(t *testing.T) {
	group := types.NewJID("120363", types.GroupServer)
	if !Allowed(group) || !IsGroup(group) {
		t.Fatal("group should be stored")
	}
	if Allowed(types.NewJID("120363", types.NewsletterServer)) {
		t.Fatal("channel should stay excluded")
	}
	if Allowed(types.StatusBroadcastJID) {
		t.Fatal("status broadcast should stay excluded")
	}
	if Allowed(types.MetaAIJID) {
		t.Fatal("bot should stay excluded")
	}
	if !Allowed(types.NewJID("15551212000", types.DefaultUserServer)) {
		t.Fatal("direct chat should be stored")
	}
}

func TestCanonical(t *testing.T) {
	got := Canonical(types.NewADJID("15551212000", 0, 3))
	if got.Device != 0 || got.Server != types.DefaultUserServer || got.User != "15551212000" {
		t.Fatalf("canonical = %+v", got)
	}
	legacy := Canonical(types.NewJID("15551212000", types.LegacyUserServer))
	if legacy.Server != types.DefaultUserServer {
		t.Fatalf("legacy server = %s", legacy.Server)
	}
}

func TestPhone(t *testing.T) {
	if got := Phone(types.NewJID("15551212000", types.DefaultUserServer)); got != "15551212000" {
		t.Fatalf("phone = %q", got)
	}
	if got := Phone(types.NewJID("999", types.HiddenUserServer)); got != "" {
		t.Fatalf("lid phone = %q", got)
	}
}
