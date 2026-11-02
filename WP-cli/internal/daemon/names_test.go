package daemon

import (
	"testing"

	"go.mau.fi/whatsmeow/types"
)

func TestContactFieldsPreferSavedName(t *testing.T) {
	saved, push := contactFields(types.ContactInfo{
		Found:        true,
		FullName:     "Ada Lovelace",
		FirstName:    "Ada",
		BusinessName: "Analytical",
		PushName:     "ada",
	}, true)
	if saved != "Ada Lovelace" || push != "ada" {
		t.Fatalf("saved %q push %q", saved, push)
	}
	saved, push = contactFields(types.ContactInfo{FirstName: "Ada", PushName: "ada"}, true)
	if saved != "Ada" || push != "ada" {
		t.Fatalf("first saved %q push %q", saved, push)
	}
	saved, push = contactFields(types.ContactInfo{BusinessName: "Analytical", PushName: "ada"}, true)
	if saved != "Analytical" || push != "ada" {
		t.Fatalf("business saved %q push %q", saved, push)
	}
	saved, push = contactFields(types.ContactInfo{PushName: "ada"}, false)
	if saved != "" || push != "" {
		t.Fatalf("missing saved %q push %q", saved, push)
	}
}
