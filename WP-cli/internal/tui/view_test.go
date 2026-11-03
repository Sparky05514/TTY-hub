package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"wp/internal/ipc"
	"wp/internal/store"
)

func TestSlashFindsContactOrMessage(t *testing.T) {
	m := newModel()
	m.status = &ipc.Status{Linked: true, Connected: true}
	m.width = 80
	m.height = 24
	m.chats = []store.Chat{
		{JID: "15551212000@s.whatsapp.net", Name: "Ada"},
		{JID: "2@lid", Name: "Bea"},
	}
	m.focus = focusList
	got, _ := m.onKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	next := got.(model)
	if !next.find || next.findIn != focusList {
		t.Fatal("slash in the list should find contacts")
	}
	next.filter = "555121"
	if len(next.shown()) != 1 || next.shown()[0].Name != "Ada" {
		t.Fatalf("number search = %+v", next.shown())
	}

	next.find = false
	next.focus = focusThread
	next.openJID = "2@lid"
	next.messages = []store.Message{
		{ID: "a", Text: "hello"},
		{ID: "b", Text: "gate code 4421", SenderName: "Sam"},
		{ID: "c", Text: "later"},
	}
	next.msgCursor = 0
	got, _ = next.onKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	found := got.(model)
	if !found.find || found.findIn != focusThread || found.focus != focusThread {
		t.Fatalf("message find focus %v in %v", found.focus, found.findIn)
	}
	found.filter = "4421"
	found.applyFind(false)
	if found.msgCursor != 1 {
		t.Fatalf("cursor = %d", found.msgCursor)
	}
	if len(found.shown()) != 2 {
		t.Fatal("message search should leave the contact list intact")
	}
}

func TestFilterChats(t *testing.T) {
	chats := []store.Chat{
		{JID: "1@lid", Name: "Ada Lovelace"},
		{JID: "2@lid", Name: "Grace Hopper", Phone: "15551212000"},
	}
	got := filterChats(chats, "grace")
	if len(got) != 1 || got[0].JID != "2@lid" {
		t.Fatalf("name filter = %+v", got)
	}
	got = filterChats(chats, "555121")
	if len(got) != 1 || got[0].JID != "2@lid" {
		t.Fatalf("phone filter = %+v", got)
	}
	if len(filterChats(chats, " ")) != 2 {
		t.Fatal("blank filter should keep every chat")
	}
}

func TestFitLinesPinsToBottom(t *testing.T) {
	lines := []string{"a", "b", "c", "d", "e"}
	got := fitLines(lines, 2, 0)
	if len(got) != 2 || got[0] != "d" || got[1] != "e" {
		t.Fatalf("bottom = %#v", got)
	}
	got = fitLines(lines, 2, 3)
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("scrolled = %#v", got)
	}
}

func TestThreadWrapsOnWords(t *testing.T) {
	msgs := []store.Message{{
		Text:   "hello from the other side of the morning",
		At:     time.Date(2026, 11, 1, 14, 2, 0, 0, time.UTC),
		FromMe: true,
	}}
	lines := threadBody(msgs, "Ada", 20, false)
	var body []string
	for _, ln := range lines {
		if ln.Msg == 0 && !ln.Time {
			body = append(body, ln.Text)
		}
	}
	joined := strings.Join(body, " ")
	if !strings.Contains(joined, "hello from") || !strings.Contains(joined, "morning") {
		t.Fatalf("wrapped = %#v", body)
	}
	for _, ln := range body {
		if strings.Contains(ln, "mornin") && !strings.Contains(ln, "morning") {
			t.Fatalf("cut a word: %q", ln)
		}
	}
}

func TestReceiptTicks(t *testing.T) {
	lipgloss.SetColorProfile(termenv.ANSI)
	sent, sentStyle := receiptIcon("sent")
	delivered, deliveredStyle := receiptIcon("delivered")
	read, readStyle := receiptIcon("read")
	if sent != "✓" || delivered != "✓✓" || read != "✓✓" {
		t.Fatalf("marks %q %q %q", sent, delivered, read)
	}
	gray := deliveredStyle.Render(delivered)
	blue := readStyle.Render(read)
	if gray != sentStyle.Render(sent+"✓") && !strings.Contains(gray, "2m") {
		t.Fatalf("delivered ticks = %q", gray)
	}
	if !strings.Contains(blue, "34") {
		t.Fatalf("read ticks = %q", blue)
	}
	if strings.Contains(gray, "32") || strings.Contains(gray, "34") {
		t.Fatalf("delivered ticks are colored %q", gray)
	}
}

func TestThreadShowsQuoteAndReceipt(t *testing.T) {
	when := time.Date(2026, 11, 1, 14, 2, 0, 0, time.Local)
	msgs := []store.Message{
		{ID: "q", Text: "the photo", FromMe: false, At: when},
		{ID: "a", Text: "on it", FromMe: true, ReplyTo: "q", ReplyText: "the photo", Receipt: "read", At: when},
	}
	var quote, receipt string
	var replyBody bool
	for _, ln := range threadBody(msgs, "Ada", 40, false) {
		if ln.Quote {
			quote = ln.Text
		}
		if ln.Time && ln.Mine {
			receipt = ln.Receipt
		}
		if ln.Reply && !ln.Time && !ln.Quote {
			replyBody = true
		}
	}
	if quote != "│ Ada: the photo" || receipt != "read" || !replyBody {
		t.Fatalf("quote %q receipt %q reply %v", quote, receipt, replyBody)
	}
}

func TestThreadShowsReactions(t *testing.T) {
	when := time.Date(2026, 11, 1, 14, 2, 0, 0, time.Local)
	msgs := []store.Message{{
		ID: "a", Text: "hi", At: when,
		Reactions: []store.Reaction{
			{Emoji: "👍", Reactor: "a@lid", At: when},
			{Emoji: "👍", Reactor: "b@lid", At: when.Add(time.Second)},
			{Emoji: "❤️", Reactor: "me@lid", FromMe: true, At: when.Add(2 * time.Second)},
		},
	}}
	var react string
	var mine bool
	for _, ln := range threadBody(msgs, "Ada", 40, false) {
		if ln.React {
			react = ln.Text
			mine = ln.MineReact
		}
	}
	if react != "👍 2  ❤️" || !mine {
		t.Fatalf("react %q mine=%v", react, mine)
	}
	line, mineAny := reactionLine(nil)
	if line != "" || mineAny {
		t.Fatalf("empty = %q %v", line, mineAny)
	}
}

// dividerCol returns the display column of the pane divider, or -1 if missing.
func dividerCol(line string) int {
	plain := ansi.Strip(line)
	idx := strings.IndexRune(plain, '│')
	if idx < 0 {
		return -1
	}
	return cellWidth(plain[:idx])
}

func TestRowPlainFillsWidth(t *testing.T) {
	got := rowPlain("Ada Lovelace", "14:02 2", 24)
	if cellWidth(got) != 24 || !strings.HasSuffix(got, "14:02 2") {
		t.Fatalf("row = %q (%d)", got, cellWidth(got))
	}
	emoji := rowPlain("Ada 👋", "14:02", 24)
	if cellWidth(emoji) != 24 {
		t.Fatalf("emoji row = %q (%d)", emoji, cellWidth(emoji))
	}
}

func TestMainViewFitsTheTerminal(t *testing.T) {
	m := newModel()
	m.width = 80
	m.height = 24
	m.ready = true
	m.status = &ipc.Status{Linked: true, Connected: true}
	now := time.Now()
	m.chats = []store.Chat{{
		JID: "1@lid", Name: "Ada 👋 Lovelace", LastText: "thanks ❤️", Unread: 2, LastAt: now,
	}}
	m.openJID = "1@lid"
	m.openName = "Ada 👋 Lovelace"
	m.messages = []store.Message{{
		ID: "1", Text: "image · 240 KB", Caption: "look", Kind: "image", FromMe: true, At: now,
	}, {
		ID: "2", Text: "hello from the other side 🎉", At: now,
	}}
	got := m.View()
	if !strings.Contains(got, "Ada") || !strings.Contains(got, "image · 240 KB") || !strings.Contains(got, "look") {
		t.Fatalf("view missing content:\n%s", got)
	}
	if !strings.Contains(got, "connected") || strings.Contains(got, "i write") {
		t.Fatalf("status = %q", got)
	}
	body := strings.Split(strings.TrimSuffix(got, "\n"), "\n")
	if len(body) < m.height-1 {
		t.Fatalf("lines = %d", len(body))
	}
	divCol := -1
	for i, line := range body {
		if strings.Contains(line, "\x1b_G") {
			continue
		}
		if w := lipgloss.Width(line); w != m.width {
			t.Fatalf("line %d width = %d\n%q", i, w, line)
		}
		col := dividerCol(line)
		if col < 0 {
			continue
		}
		if divCol < 0 {
			divCol = col
		} else if col != divCol {
			t.Fatalf("divider drifted on line %d: col %d want %d\n%q", i, col, divCol, line)
		}
	}
	if divCol != m.listWidth()+1 {
		t.Fatalf("divider col = %d, want %d", divCol, m.listWidth()+1)
	}
	last := body[len(body)-1]
	if !strings.Contains(last, ">") {
		t.Fatalf("composer missing from the message column:\n%s", last)
	}
	m.notice = "no file"
	withNotice := m.View()
	if !strings.Contains(withNotice, "no file") {
		t.Fatal("notice missing under the contacts")
	}
}

func TestMediaAction(t *testing.T) {
	if mediaAction("image", false) != "open" || mediaAction("image", true) != "" {
		t.Fatal("images open only when received")
	}
	if mediaAction("voice", true) != "play" || mediaAction("voice", false) != "play" {
		t.Fatal("voice messages should play")
	}
	if mediaAction("file", false) != "" {
		t.Fatal("files stay closed")
	}
}

func TestMediaLabelLine(t *testing.T) {
	if !mediaLabelLine("voice message") || !mediaLabelLine("[voice message]") || !mediaLabelLine("image · 240 KB") {
		t.Fatal("file labels should be marked")
	}
	if mediaLabelLine("see you tomorrow") || mediaLabelLine("look") {
		t.Fatal("a normal sentence should not be marked")
	}
	lines := threadBody([]store.Message{
		{Text: "voice message", Kind: "audio", SenderName: "Sam"},
		{Text: "see you tomorrow"},
		{Text: "image · 240 KB", Kind: "image", FromMe: true},
	}, "Ada", 40, true)
	var voice, sentence bool
	var voiceText, mineText string
	for _, ln := range lines {
		if strings.Contains(ln.Text, "voice message") {
			voice = ln.Media
			voiceText = ln.Text
		}
		if ln.Text == "see you tomorrow" {
			sentence = ln.Media
		}
		if strings.Contains(ln.Text, "image ·") {
			mineText = ln.Text
		}
	}
	if !voice || sentence || voiceText != "Sam · voice message" || mineText != "5514 · image · 240 KB" {
		t.Fatalf("voice %v %q sentence %v mine %q", voice, voiceText, sentence, mineText)
	}
}

func TestQuoteWhoUsesLoadedSender(t *testing.T) {
	when := time.Date(2026, 11, 1, 14, 2, 0, 0, time.Local)
	msgs := []store.Message{
		{ID: "q", Text: "the photo", SenderName: "Sam", At: when},
		{ID: "a", Text: "on it", ReplyTo: "q", ReplyText: "the photo", At: when},
	}
	var quote, stamp string
	for _, ln := range threadBody(msgs, "Family", 40, true) {
		if ln.Quote {
			quote = ln.Text
		}
		if ln.Time && ln.Msg == 0 {
			stamp = ln.Text
		}
	}
	if quote != "│ Sam: the photo" || !strings.HasPrefix(stamp, "Sam  ") {
		t.Fatalf("quote %q stamp %q", quote, stamp)
	}
}

func TestMessageCursorFollowsOnlyTheNewest(t *testing.T) {
	now := time.Now()
	older := []store.Message{
		{ID: "a", Text: "one", At: now},
		{ID: "b", Text: "two", At: now},
	}
	newer := append(append([]store.Message{}, older...), store.Message{ID: "c", Text: "three", At: now})
	m := newModel()
	m.width = 80
	m.height = 24
	m.openJID = "1@lid"
	m.followLatest = false
	m.msgCursor = 0
	m.messages = older
	got, _ := m.Update(threadMsg{jid: "1@lid", msgs: newer})
	next := got.(model)
	if next.msgCursor != 0 || next.followLatest {
		t.Fatalf("cursor %d follow %v", next.msgCursor, next.followLatest)
	}

	m.followLatest = true
	got, _ = m.Update(threadMsg{jid: "1@lid", msgs: newer})
	next = got.(model)
	if next.msgCursor != 2 || !next.followLatest || next.scroll != 0 {
		t.Fatalf("cursor %d follow %v scroll %d", next.msgCursor, next.followLatest, next.scroll)
	}
}

func TestJumpQuote(t *testing.T) {
	now := time.Now()
	m := newModel()
	m.width = 80
	m.height = 24
	m.focus = focusThread
	m.openJID = "1@g.us"
	m.messages = []store.Message{
		{ID: "q", Text: "the photo", SenderName: "Sam", At: now},
		{ID: "a", Text: "on it", ReplyTo: "q", ReplyText: "the photo", At: now},
		{ID: "b", Text: "later", ReplyTo: "missing", ReplyText: "old", At: now},
	}
	m.msgCursor = 1
	m.jumpQuote()
	if m.msgCursor != 0 {
		t.Fatalf("cursor = %d", m.msgCursor)
	}
	m.msgCursor = 2
	m.jumpQuote()
	if m.notice != "not in the loaded history" || m.msgCursor != 2 {
		t.Fatalf("notice %q cursor %d", m.notice, m.msgCursor)
	}
	m.msgCursor = 1
	m.openSelected()
	if m.notice != "no file" {
		t.Fatalf("notice = %q", m.notice)
	}
}

func TestCursorUpKeepsVisibleScroll(t *testing.T) {
	now := time.Now()
	m := newModel()
	m.width = 80
	m.height = 24
	m.openJID = "1@lid"
	m.messages = []store.Message{
		{ID: "a", Text: "one", At: now},
		{ID: "b", Text: "two", At: now},
	}
	m.msgCursor = 1
	m.followLatest = true
	m.scroll = 0
	m.moveMsg(-1)
	if m.msgCursor != 0 || m.scroll != 0 {
		t.Fatalf("cursor %d scroll %d", m.msgCursor, m.scroll)
	}
}

func TestOpenSelectedUsesKindOrPath(t *testing.T) {
	m := newModel()
	m.openJID = "1@lid"
	m.messages = []store.Message{{ID: "a", Text: "hello"}}
	m.msgCursor = 0
	if cmd := m.openSelected(); cmd != nil || m.notice != "no file" {
		t.Fatalf("notice %q cmd %v", m.notice, cmd != nil)
	}
	m.messages[0].Kind = "image"
	m.notice = ""
	if cmd := m.openSelected(); cmd == nil || m.notice != "opening…" {
		t.Fatalf("kind notice %q", m.notice)
	}
	m.messages[0].Kind = ""
	m.messages[0].FilePath = "/tmp/a.jpg"
	m.notice = ""
	if cmd := m.openSelected(); cmd == nil || m.notice != "opening…" {
		t.Fatalf("path notice %q", m.notice)
	}
}

func TestDraftStaysWithTheChat(t *testing.T) {
	m := newModel()
	m.chats = []store.Chat{
		{JID: "a@lid", Name: "Ada"},
		{JID: "b@lid", Name: "Bea"},
	}
	m.openJID = "a@lid"
	m.input.SetValue("hello")
	m.replyID = "m1"
	m.replyText = "photo"
	m.move(1)
	if m.openJID != "b@lid" || m.input.Value() != "" || m.replyID != "" || m.replyText != "" {
		t.Fatalf("left jid %s text %q reply %q %q", m.openJID, m.input.Value(), m.replyID, m.replyText)
	}
	m.move(-1)
	if m.openJID != "a@lid" || m.input.Value() != "hello" || m.replyID != "m1" || m.replyText != "photo" {
		t.Fatalf("back jid %s text %q reply %q %q", m.openJID, m.input.Value(), m.replyID, m.replyText)
	}
}

func TestPrependOlderKeepsTheCursorMessage(t *testing.T) {
	now := time.Now()
	m := newModel()
	m.openJID = "1@lid"
	m.messages = []store.Message{{ID: "b", Text: "two", At: now}}
	m.msgCursor = 0
	m.scroll = 4
	m.prependOlder([]store.Message{
		{ID: "a", Text: "one", At: now.Add(-time.Minute)},
		{ID: "b", Text: "two", At: now},
	})
	if m.scroll != 4 || m.msgCursor != 1 || m.messages[0].ID != "a" || len(m.messages) != 2 {
		t.Fatalf("cursor %d scroll %d msgs %+v", m.msgCursor, m.scroll, m.messages)
	}
}

func TestPlaceWindowScrollsOnlyAtTheEdge(t *testing.T) {
	start := 0
	for cursor := 0; cursor < 3; cursor++ {
		start = placeWindow(cursor, start, 3, 10)
	}
	if start != 0 {
		t.Fatalf("inside start = %d", start)
	}
	start = placeWindow(3, start, 3, 10)
	if start != 1 {
		t.Fatalf("edge start = %d", start)
	}
	start = placeWindow(2, start, 3, 10)
	if start != 1 {
		t.Fatalf("moving up inside the window changed start to %d", start)
	}
}

func TestComposerUsesABlockCursor(t *testing.T) {
	lipgloss.SetColorProfile(termenv.ANSI)
	m := newModel()
	m.width = 80
	m.height = 24
	m.focus = focusInput
	m.input.SetValue("hi")
	m.input.SetCursor(1)
	rows := m.composerRowsText(40)
	if len(rows) == 0 || strings.Contains(rows[0], "▌") || !strings.Contains(rows[0], "\x1b[7m") {
		t.Fatalf("composer = %q", rows)
	}
	if !strings.Contains(rows[0], "h") || !strings.Contains(rows[0], "i") {
		t.Fatalf("composer = %q", rows)
	}
}

func TestViewOnceDoesNotAutoOpen(t *testing.T) {
	m := newModel()
	m.openJID = "1@lid"
	ev := ipc.Live{ID: "a", Chat: "1@lid", Kind: "image", Path: "/tmp/a.jpg", Text: "view once photo"}
	if m.autoMedia(ev) != nil {
		t.Fatal("view once opened on arrival")
	}
}

func TestInsertAndEsc(t *testing.T) {
	m := newModel()
	m.status = &ipc.Status{Linked: true, Connected: true}
	m.focus = focusList
	got, _ := m.onKey(tea.KeyMsg{Type: tea.KeyEnter})
	next := got.(model)
	if next.focus != focusInput || next.insertFrom != focusList {
		t.Fatalf("focus %v from %v", next.focus, next.insertFrom)
	}
	got, _ = next.onKey(tea.KeyMsg{Type: tea.KeyEsc})
	if got.(model).focus != focusList {
		t.Fatalf("focus after esc = %v", got.(model).focus)
	}
}

func TestPickOrdersDirectoriesFirst(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "note.txt"), []byte("a"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "pics"), 0o700); err != nil {
		t.Fatal(err)
	}
	ents := readPick(dir, "")
	var names []string
	for _, ent := range ents {
		if ent.name != ".." {
			names = append(names, ent.name)
		}
	}
	if len(names) != 2 || names[0] != "pics" || names[1] != "note.txt" {
		t.Fatalf("entries = %#v", names)
	}
	filtered := readPick(dir, "note")
	found := false
	for _, ent := range filtered {
		if ent.name == "note.txt" {
			found = true
		}
		if ent.name == "pics" {
			t.Fatal("filter kept a directory that does not match")
		}
	}
	if !found {
		t.Fatal("filter missed note.txt")
	}
}

func TestSelectedMineStaysRight(t *testing.T) {
	lipgloss.SetColorProfile(termenv.ANSI)
	m := newModel()
	m.width = 80
	m.height = 24
	m.focus = focusThread
	m.openName = "Ada"
	m.messages = []store.Message{{ID: "1", Text: "hello", FromMe: true, At: time.Now()}}
	m.msgCursor = 0
	var hit string
	for _, ln := range m.threadLines(8) {
		if strings.Contains(ln, "hello") {
			hit = ln
			break
		}
	}
	if !strings.Contains(hit, " hello") {
		t.Fatalf("selected line %q", hit)
	}
}

func TestHighlightFollowsFocus(t *testing.T) {
	lipgloss.SetColorProfile(termenv.ANSI)
	m := newModel()
	chat := store.Chat{Name: "Ada", LastText: "hi", LastAt: time.Now()}
	brightTop, brightPreview := m.paintChat(chat, true, true, 24)
	dimTop, dimPreview := m.paintChat(chat, true, false, 24)
	if brightTop == dimTop || brightPreview == dimPreview {
		t.Fatal("unfocused contact uses the same highlight")
	}
	m.width = 80
	m.height = 24
	m.openName = "Ada"
	m.messages = []store.Message{{ID: "1", Text: "hello", At: time.Now()}}
	m.msgCursor = 0
	m.focus = focusList
	idle := strings.Join(m.threadLines(8), "\n")
	m.focus = focusThread
	active := strings.Join(m.threadLines(8), "\n")
	if idle == active {
		t.Fatal("message highlight shows when the thread is not focused")
	}
}

func TestNoticeClearsOnTheNextKey(t *testing.T) {
	m := newModel()
	m.status = &ipc.Status{Linked: true, Connected: true}
	m.chats = []store.Chat{{JID: "1@lid", Name: "Ada"}}
	m.openJID = "1@lid"
	m.focus = focusThread
	m.messages = []store.Message{{ID: "a", Text: "photo", Kind: "image"}}
	m.msgCursor = 0
	got, _ := m.onKey(tea.KeyMsg{Type: tea.KeyEnter})
	if got.(model).notice != "opening…" {
		t.Fatalf("notice = %q", got.(model).notice)
	}
	got, _ = got.(model).onKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if got.(model).notice != "" {
		t.Fatalf("notice stayed %q", got.(model).notice)
	}
}

func TestEnterOpensOrWrites(t *testing.T) {
	m := newModel()
	m.status = &ipc.Status{Linked: true, Connected: true}
	m.openJID = "1@lid"
	m.focus = focusThread
	m.messages = []store.Message{{ID: "a", Text: "hello"}}
	m.msgCursor = 0
	got, _ := m.onKey(tea.KeyMsg{Type: tea.KeyEnter})
	next := got.(model)
	if next.focus != focusInput || next.notice != "" {
		t.Fatalf("text enter focus %v notice %q", next.focus, next.notice)
	}
	m = newModel()
	m.status = &ipc.Status{Linked: true, Connected: true}
	m.openJID = "1@lid"
	m.focus = focusThread
	m.messages = []store.Message{{ID: "a", Text: "photo", Kind: "image"}}
	m.msgCursor = 0
	got, cmd := m.onKey(tea.KeyMsg{Type: tea.KeyEnter})
	next = got.(model)
	if next.focus != focusThread || next.notice != "opening…" || cmd == nil {
		t.Fatalf("media enter focus %v notice %q cmd %v", next.focus, next.notice, cmd != nil)
	}
}

func TestBindingsReplaceTheKeyList(t *testing.T) {
	m := newModel()
	m.ready = true
	m.status = &ipc.Status{Linked: true, Connected: true}
	m.width = 80
	m.height = 24
	m.focus = focusThread
	m.openName = "Ada"
	got, _ := m.onKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'K'}})
	next := got.(model)
	if !next.help {
		t.Fatal("K should open the bindings")
	}
	view := next.View()
	if !strings.Contains(view, "bindings") || !strings.Contains(view, "reply") {
		t.Fatalf("bindings missing:\n%s", view)
	}
	got, _ = next.onKey(tea.KeyMsg{Type: tea.KeyEsc})
	if got.(model).help {
		t.Fatal("esc should close the bindings")
	}
}

func TestNextUnreadWraps(t *testing.T) {
	m := newModel()
	m.chats = []store.Chat{
		{JID: "a", Name: "A"},
		{JID: "b", Name: "B", Unread: 2},
		{JID: "c", Name: "C", Unread: 1},
	}
	m.cursor = 2
	m.openJID = "c"
	m.nextUnread()
	if m.openJID != "b" {
		t.Fatalf("open = %s", m.openJID)
	}
	m.chats[1].Unread = 0
	m.chats[2].Unread = 0
	m.nextUnread()
	if m.notice != "no unread" {
		t.Fatalf("notice = %q", m.notice)
	}
}

func TestShiftEnterAndDraftMark(t *testing.T) {
	if !shiftEnter("?CSI[49 51 59 50 117]?") || !shiftEnter("27;2;13~") {
		t.Fatal("shift+enter sequences should be recognized")
	}
	if shiftEnter("enter") {
		t.Fatal("plain enter is not a newline")
	}
	m := newModel()
	m.input.SetValue("hi")
	m.input.SetCursor(2)
	m.insertNewline()
	if m.composerText() != "hi\n" {
		t.Fatalf("value = %q", m.composerText())
	}
	if m.composerRows() != 2 {
		t.Fatalf("rows = %d", m.composerRows())
	}
	m.drafts = map[string]draft{"1@lid": {text: "hi"}}
	top, _ := m.paintChat(store.Chat{JID: "1@lid", Name: "Ada"}, false, false, 40)
	if !strings.Contains(top, "draft") {
		t.Fatalf("row = %q", top)
	}
}

func TestThreadLayout(t *testing.T) {
	base := time.Date(2026, 11, 1, 14, 0, 0, 0, time.Local)
	msgs := []store.Message{
		{ID: "a", Text: "one", FromMe: true, At: base},
		{ID: "b", Text: "two", FromMe: true, At: base.Add(time.Minute)},
		{ID: "c", Text: "three", FromMe: false, At: base.AddDate(0, 0, -1)},
	}
	lines := threadBody(msgs, "Ada", 40, false)
	var times, days int
	var mine string
	for _, ln := range lines {
		if ln.Time && ln.Mine {
			times++
		}
		if ln.Day {
			days++
		}
		if ln.Mine && !ln.Time && !ln.Quote && ln.Text == "two" {
			mine = ln.Text
		}
	}
	if times != 1 {
		t.Fatalf("clocks = %d", times)
	}
	if days < 2 {
		t.Fatalf("days = %d", days)
	}
	m := newModel()
	m.width = 40
	m.focus = focusList
	painted := alignLine(styleAccent.Render(mine), 20, true)
	if !strings.Contains(painted, mine) || !strings.HasPrefix(painted, " ") || lipgloss.Width(painted) != 20 {
		t.Fatalf("aligned %q width %d", painted, lipgloss.Width(painted))
	}
	lipgloss.SetColorProfile(termenv.ANSI)
	top, _ := m.paintChat(store.Chat{JID: "1@lid", Name: "Ada"}, false, false, 30)
	if !strings.Contains(top, styleAccent.Render("Ada")) {
		t.Fatalf("row = %q", top)
	}
	if dayLabel(time.Now()) != "today" {
		t.Fatal("today")
	}
	if dayLabel(time.Now().AddDate(0, 0, -1)) != "yesterday" {
		t.Fatal("yesterday")
	}
}

func TestDeletedEditedTypingAndArrivals(t *testing.T) {
	now := time.Now()
	lines := threadBody([]store.Message{
		{ID: "a", Text: "hello", FromMe: true, Edited: true, At: now},
		{ID: "b", Text: "secret", Deleted: true, At: now},
	}, "Ada", 40, false)
	var edited, deleted bool
	for _, ln := range lines {
		if ln.Time && strings.Contains(ln.Text, "edited") {
			edited = true
		}
		if ln.Deleted && ln.Text == "deleted" {
			deleted = true
		}
		if ln.Text == "secret" {
			t.Fatal("deleted body stayed visible")
		}
	}
	if !edited || !deleted {
		t.Fatalf("edited %v deleted %v", edited, deleted)
	}
	m := newModel()
	m.width = 80
	m.height = 24
	m.ready = true
	m.status = &ipc.Status{Linked: true, Connected: true}
	m.openJID = "1@lid"
	m.openName = "Ada"
	m.chats = []store.Chat{{JID: "1@lid", Name: "Ada", LastAt: now}}
	m.messages = []store.Message{{ID: "a", Text: "old", At: now}}
	m.followLatest = false
	m.msgCursor = 0
	if cmd := m.onLive(ipc.Live{ID: "b", Chat: "1@lid", Text: "later", At: now}); cmd != nil || m.pendingNew != 1 || len(m.messages) != 1 {
		t.Fatalf("pending %d msgs %d cmd %v", m.pendingNew, len(m.messages), cmd != nil)
	}
	m.focus = focusThread
	view := strings.Join(m.threadLines(8), "\n")
	if !strings.Contains(view, "1 new") {
		t.Fatalf("thread:\n%s", view)
	}
	m.theirWho = "Ada"
	m.theirAt = time.Now()
	got := m.View()
	if !strings.Contains(got, "Ada is typing") {
		t.Fatal("typing line missing")
	}
}

func TestGroupRowIsMarked(t *testing.T) {
	lipgloss.SetColorProfile(termenv.ANSI)
	m := newModel()
	top, _ := m.paintChat(store.Chat{JID: "1@g.us", Name: "Family", LastText: "hi"}, false, false, 40)
	if !strings.Contains(top, "group") {
		t.Fatalf("row = %q", top)
	}
	if !strings.Contains(top, styleGroup.Render("Family")) {
		t.Fatalf("group name not colored: %q", top)
	}
	plain, _ := m.paintChat(store.Chat{JID: "1@lid", Name: "Ada", LastText: "hi"}, false, false, 40)
	if strings.Contains(plain, "group") {
		t.Fatalf("direct row = %q", plain)
	}
	if !strings.Contains(plain, styleAccent.Render("Ada")) {
		t.Fatalf("contact name not colored: %q", plain)
	}
}

func TestListWidthLeavesRoomForThread(t *testing.T) {
	w := listWidthFor(100)
	if w < 22 || w > 40 || 100-w-3 < 16 {
		t.Fatalf("width = %d", w)
	}
}
