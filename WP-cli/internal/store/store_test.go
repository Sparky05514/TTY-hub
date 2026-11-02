package store

import (
	"path/filepath"
	"testing"
	"time"
)

func TestSaveAndResolve(t *testing.T) {
	s := openTest(t)
	at := time.Date(2026, 11, 1, 18, 0, 0, 0, time.UTC)
	chat, changed, err := s.SaveLive(Incoming{
		Message: Message{
			ID:     "m1",
			Chat:   "123@lid",
			Sender: "123@lid",
			Text:   "hello",
			At:     at,
		},
		Name:   "Ada Lovelace",
		Phone:  "15551212000",
		AltJID: "15551212000@s.whatsapp.net",
		Unread: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !changed || chat.Label() != "Ada Lovelace" || chat.Unread != 1 {
		t.Fatalf("saved chat = %+v changed=%v", chat, changed)
	}
	again, changed, err := s.SaveLive(Incoming{
		Message: Message{ID: "m1", Chat: "123@lid", Text: "hello", At: at},
	})
	if err != nil {
		t.Fatal(err)
	}
	if changed || again.Unread != 1 {
		t.Fatalf("duplicate should not count as new: %+v changed=%v", again, changed)
	}

	for _, query := range []string{"Ada Lovelace", "ada", "+1 555 121 2000", "15551212000@s.whatsapp.net"} {
		got, err := s.Resolve(query)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 || got[0].JID != "123@lid" {
			t.Fatalf("resolve %q = %+v", query, got)
		}
	}

	msgs, err := s.Messages("123@lid", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 || msgs[0].Text != "hello" {
		t.Fatalf("messages = %+v", msgs)
	}
	if err := s.MarkRead("123@lid"); err != nil {
		t.Fatal(err)
	}
	got, _, err := s.Chat("123@lid")
	if err != nil {
		t.Fatal(err)
	}
	if got.Unread != 0 {
		t.Fatalf("unread = %d", got.Unread)
	}
}

func TestHistoryUnreadAndOrder(t *testing.T) {
	s := openTest(t)
	base := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	err := s.SaveHistory("999@lid", "", "Grace", "", "", 4, base, "", []Message{
		{ID: "a", Text: "first", At: base},
		{ID: "b", Text: "second", At: base.Add(time.Minute)},
	})
	if err != nil {
		t.Fatal(err)
	}
	msgs, err := s.Messages("999@lid", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 || msgs[0].Text != "first" || msgs[1].Text != "second" {
		t.Fatalf("order = %+v", msgs)
	}
	chat, _, err := s.Chat("999@lid")
	if err != nil {
		t.Fatal(err)
	}
	if chat.Unread != 4 || chat.LastText != "second" {
		t.Fatalf("chat = %+v", chat)
	}
	listed, err := s.Chats(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 {
		t.Fatalf("listed = %+v", listed)
	}
}

func TestAmbiguousName(t *testing.T) {
	s := openTest(t)
	at := time.Now()
	for _, in := range []Incoming{
		{Message: Message{ID: "1", Chat: "1@lid", Text: "a", At: at}, Name: "Sam North"},
		{Message: Message{ID: "2", Chat: "2@lid", Text: "b", At: at.Add(time.Second)}, Name: "Sam South"},
	} {
		if _, _, err := s.SaveLive(in); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.Resolve("sam")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("matches = %d", len(got))
	}
	exact, err := s.Resolve("Sam North")
	if err != nil {
		t.Fatal(err)
	}
	if len(exact) != 1 || exact[0].JID != "1@lid" {
		t.Fatalf("exact = %+v", exact)
	}
}

func TestContactWithoutMessagesIsHidden(t *testing.T) {
	s := openTest(t)
	if err := s.Remember("5@lid", "", "Hidden", "", ""); err != nil {
		t.Fatal(err)
	}
	listed, err := s.Chats(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 0 {
		t.Fatalf("listed = %+v", listed)
	}
	got, err := s.Resolve("Hidden")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("resolve = %+v", got)
	}
}

func TestFoldPhoneAndLID(t *testing.T) {
	s := openTest(t)
	at := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)
	if _, _, err := s.SaveLive(Incoming{
		Message: Message{ID: "p", Chat: "15551212000@s.whatsapp.net", Text: "from phone", At: at},
		Name:    "Ada Lovelace",
		Phone:   "15551212000",
	}); err != nil {
		t.Fatal(err)
	}
	chat, _, err := s.SaveLive(Incoming{
		Message: Message{ID: "l", Chat: "123@lid", Text: "from lid", At: at.Add(time.Minute)},
		AltJID:  "15551212000@s.whatsapp.net",
		Phone:   "15551212000",
	})
	if err != nil {
		t.Fatal(err)
	}
	if chat.JID != "123@lid" || chat.Label() != "Ada Lovelace" {
		t.Fatalf("folded chat = %+v", chat)
	}
	got, err := s.Resolve("+15551212000")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].JID != "123@lid" {
		t.Fatalf("resolve = %+v", got)
	}
	msgs, err := s.Messages("123@lid", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 || msgs[0].Text != "from phone" || msgs[1].Text != "from lid" {
		t.Fatalf("messages = %+v", msgs)
	}
	listed, err := s.Chats(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 {
		t.Fatalf("listed = %+v", listed)
	}
}

func TestEditKeepsPlaceAndDeleteRewindsPreview(t *testing.T) {
	s := openTest(t)
	at := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)
	if _, _, err := s.SaveLive(Incoming{
		Message: Message{ID: "1", Chat: "1@lid", Text: "first", At: at},
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.SaveLive(Incoming{
		Message: Message{ID: "2", Chat: "1@lid", Text: "second", At: at.Add(time.Minute)},
	}); err != nil {
		t.Fatal(err)
	}
	chat, changed, err := s.SaveLive(Incoming{
		Message: Message{ID: "1", Chat: "1@lid", Text: "first edited", At: at.Add(2 * time.Minute)},
		Edit:    true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !changed || chat.LastText != "second" {
		t.Fatalf("edit changed the preview: %+v", chat)
	}
	msgs, err := s.Messages("1@lid", 10)
	if err != nil {
		t.Fatal(err)
	}
	if msgs[0].Text != "first edited" || !msgs[0].Edited || !msgs[0].At.Equal(at) {
		t.Fatalf("edited message = %+v", msgs[0])
	}
	chat, ok, err := s.DeleteMessage("1@lid", "2")
	if err != nil || !ok {
		t.Fatalf("delete ok=%v err=%v", ok, err)
	}
	if chat.LastText != "first edited" {
		t.Fatalf("preview after delete = %+v", chat)
	}
	chat, ok, err = s.MarkDeleted("1@lid", "1")
	if err != nil || !ok || chat.LastText != "deleted" {
		t.Fatalf("mark deleted ok=%v err=%v chat=%+v", ok, err, chat)
	}
	msgs, err = s.Messages("1@lid", 10)
	if err != nil || len(msgs) != 1 || !msgs[0].Deleted || msgs[0].Display() != "deleted" {
		t.Fatalf("tombstone = %+v err=%v", msgs, err)
	}
}

func TestFind(t *testing.T) {
	s := openTest(t)
	at := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)
	if _, _, err := s.SaveLive(Incoming{
		Message: Message{ID: "1", Chat: "1@lid", Text: "pack the 100% bag", At: at},
		Name:    "Ada",
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.SaveLive(Incoming{
		Message: Message{ID: "2", Chat: "1@lid", Text: "nothing", At: at.Add(time.Minute)},
	}); err != nil {
		t.Fatal(err)
	}
	hits, err := s.Find("100%", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].ChatName != "Ada" {
		t.Fatalf("hits = %+v", hits)
	}
}

func TestReceiptUpgradesAndCoversEarlier(t *testing.T) {
	s := openTest(t)
	base := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	for i, id := range []string{"a", "b"} {
		_, _, err := s.SaveLive(Incoming{Message: Message{
			ID: id, Chat: "123@lid", FromMe: true, Text: id, Receipt: "sent", At: base.Add(time.Duration(i) * time.Minute),
		}})
		if err != nil {
			t.Fatal(err)
		}
	}
	updated, err := s.SetReceipt("123@lid", []string{"b"}, "delivered", base.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(updated) != 1 || updated[0].ID != "b" || updated[0].Receipt != "delivered" {
		t.Fatalf("delivered = %#v", updated)
	}
	updated, err = s.SetReceipt("123@lid", []string{"b"}, "read", base.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, m := range updated {
		got[m.ID] = m.Receipt
	}
	if got["a"] != "read" || got["b"] != "read" {
		t.Fatalf("read = %#v", got)
	}
	again, err := s.SetReceipt("123@lid", []string{"b"}, "delivered", base.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(again) != 0 {
		t.Fatalf("delivered must not erase read: %#v", again)
	}
}

func TestMessagesBeforeStopsAtTheAnchor(t *testing.T) {
	s := openTest(t)
	base := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	for i, id := range []string{"a", "b", "c"} {
		if _, _, err := s.SaveLive(Incoming{Message: Message{
			ID: id, Chat: "1@lid", Text: id, At: base.Add(time.Duration(i) * time.Minute),
		}}); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.MessagesBefore("1@lid", "c", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != "a" || got[1].ID != "b" {
		t.Fatalf("before c = %+v", got)
	}
	none, err := s.MessagesBefore("1@lid", "a", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(none) != 0 {
		t.Fatalf("before a = %+v", none)
	}
}

func TestFindInChatSpan(t *testing.T) {
	s := openTest(t)
	base := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	texts := []string{"alpha", "beta code", "gamma"}
	for i, text := range texts {
		if _, _, err := s.SaveLive(Incoming{Message: Message{
			ID: text, Chat: "1@lid", Text: text, At: base.Add(time.Duration(i) * time.Minute),
		}}); err != nil {
			t.Fatal(err)
		}
	}
	got, ok, err := s.FindInChat("1@lid", "code")
	if err != nil || !ok || got.ID != "beta code" {
		t.Fatalf("hit %+v ok %v err %v", got, ok, err)
	}
	span, err := s.MessagesSpan("1@lid", "alpha", "gamma", 300)
	if err != nil {
		t.Fatal(err)
	}
	if len(span) != 3 || span[0].ID != "alpha" || span[2].ID != "gamma" {
		t.Fatalf("span = %+v", span)
	}
	missing, ok, err := s.FindInChat("1@lid", "nope")
	if err != nil || ok || missing.ID != "" {
		t.Fatalf("missing %+v ok %v", missing, ok)
	}
}

func TestGroupSenderPreview(t *testing.T) {
	s := openTest(t)
	at := time.Date(2026, 11, 1, 18, 0, 0, 0, time.UTC)
	chat, _, err := s.SaveLive(Incoming{Message: Message{
		ID: "g1", Chat: "120363@g.us", SenderName: "Sam", Text: "hello", At: at,
	}, Name: "Family"})
	if err != nil {
		t.Fatal(err)
	}
	if chat.LastText != "Sam: hello" || chat.Name != "Family" {
		t.Fatalf("chat = %+v", chat)
	}
	msgs, err := s.Messages("120363@g.us", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 || msgs[0].SenderName != "Sam" {
		t.Fatalf("messages = %+v", msgs)
	}
	own := Message{Chat: "120363@g.us", SenderName: "Sam", FromMe: true, Text: "hello"}
	if own.ListPreview() != "hello" {
		t.Fatalf("own preview = %q", own.ListPreview())
	}
}

func TestSeenIfEmptyListsAGroupOnce(t *testing.T) {
	s := openTest(t)
	if err := s.Remember("120363@g.us", "", "Family", "", ""); err != nil {
		t.Fatal(err)
	}
	listed, err := s.Chats(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 0 {
		t.Fatalf("unnamed time listed = %+v", listed)
	}
	created := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
	if err := s.SeenIfEmpty("120363@g.us", created); err != nil {
		t.Fatal(err)
	}
	listed, err = s.Chats(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].Name != "Family" || !listed[0].LastAt.Equal(created) {
		t.Fatalf("listed = %+v", listed)
	}
	later := created.Add(48 * time.Hour)
	if _, _, err := s.SaveLive(Incoming{Message: Message{
		ID: "g1", Chat: "120363@g.us", Text: "hi", At: later,
	}}); err != nil {
		t.Fatal(err)
	}
	if err := s.SeenIfEmpty("120363@g.us", created); err != nil {
		t.Fatal(err)
	}
	chat, _, err := s.Chat("120363@g.us")
	if err != nil {
		t.Fatal(err)
	}
	if !chat.LastAt.Equal(later) {
		t.Fatalf("last = %s", chat.LastAt)
	}
}

func TestRenameSenderRefreshesPreview(t *testing.T) {
	s := openTest(t)
	at := time.Date(2026, 11, 1, 18, 0, 0, 0, time.UTC)
	if _, _, err := s.SaveLive(Incoming{Message: Message{
		ID: "g1", Chat: "120363@g.us", Sender: "1@lid", SenderName: "Sam", Text: "hello", At: at,
	}, Name: "Family"}); err != nil {
		t.Fatal(err)
	}
	if err := s.RenameSender([]string{"1@lid"}, "Samuel"); err != nil {
		t.Fatal(err)
	}
	chat, _, err := s.Chat("120363@g.us")
	if err != nil {
		t.Fatal(err)
	}
	if chat.LastText != "Samuel: hello" {
		t.Fatalf("preview = %q", chat.LastText)
	}
	msgs, err := s.Messages("120363@g.us", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 || msgs[0].SenderName != "Samuel" {
		t.Fatalf("messages = %+v", msgs)
	}
}

func TestViewOnceNoticeDoesNotReplaceFile(t *testing.T) {
	s := openTest(t)
	at := time.Date(2026, 11, 1, 18, 0, 0, 0, time.UTC)
	if _, _, err := s.SaveLive(Incoming{Message: Message{
		ID: "once", Chat: "1@lid", Text: "view once photo", Kind: "image",
		MediaState: "remote", MediaRef: "ref", At: at,
	}}); err != nil {
		t.Fatal(err)
	}
	_, changed, err := s.SaveLive(Incoming{
		Message: Message{ID: "once", Chat: "1@lid", Text: "view once", At: at},
		Keep:    true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("notice replaced a stored file")
	}
	got, err := s.Media("1@lid", "once")
	if err != nil {
		t.Fatal(err)
	}
	if got.Text != "view once photo" || got.MediaRef != "ref" {
		t.Fatalf("stored = %#v", got)
	}
}

func TestMediaFileRoundTrip(t *testing.T) {
	s := openTest(t)
	at := time.Date(2026, 11, 1, 18, 0, 0, 0, time.UTC)
	_, _, err := s.SaveLive(Incoming{Message: Message{
		ID:         "img",
		Chat:       "123@lid",
		Text:       "image · 240 KB",
		Caption:    "look",
		Kind:       "image",
		FileSize:   245760,
		MediaState: "remote",
		MediaRef:   `{"path":"/v","type":"WhatsApp Image Keys","mms":"image"}`,
		At:         at,
	}})
	if err != nil {
		t.Fatal(err)
	}
	chat, _, err := s.Chat("123@lid")
	if err != nil {
		t.Fatal(err)
	}
	if chat.LastText != "look" {
		t.Fatalf("preview = %q", chat.LastText)
	}
	got, err := s.Media("123@lid", "img")
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != "image" || got.MediaRef == "" || got.Display() != "image · 240 KB\nlook" {
		t.Fatalf("stored = %#v", got)
	}
	updated, err := s.SetFile("123@lid", "img", "", "unavailable", 0)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Text != "image · 240 KB · unavailable" || updated.Preview() != "look" {
		t.Fatalf("unavailable = %#v", updated)
	}
}

func openTest(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "messages.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}
