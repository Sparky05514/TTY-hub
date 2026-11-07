package tui

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-isatty"

	"wp/internal/ipc"
	"wp/internal/store"
)

var (
	styleAccent    = lipgloss.NewStyle().Foreground(lipgloss.Color("4"))
	styleAccentEm  = lipgloss.NewStyle().Foreground(lipgloss.Color("4")).Italic(true)
	styleGroup     = lipgloss.NewStyle().Foreground(lipgloss.Color("5"))
	styleMedia     = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
	styleSent      = lipgloss.NewStyle().Faint(true)
	styleDelivered = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	styleBad       = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	styleDim       = lipgloss.NewStyle().Faint(true)
	styleItalic    = lipgloss.NewStyle().Italic(true)
	styleSelect    = lipgloss.NewStyle().Foreground(lipgloss.Color("0")).Background(lipgloss.Color("4"))
	styleSelectDim = lipgloss.NewStyle().Foreground(lipgloss.Color("7")).Background(lipgloss.Color("8"))
	styleBold      = lipgloss.NewStyle().Bold(true)
	styleBlock     = lipgloss.NewStyle().Reverse(true)
)

type focus int

const (
	focusList focus = iota
	focusThread
	focusInput
)

type model struct {
	width  int
	height int
	focus  focus
	input  textinput.Model

	status       *ipc.Status
	chats        []store.Chat
	cursor       int
	listStart    int
	filter       string
	find         bool
	findIn       focus
	findAnchor   int
	pendingFind  string
	findWaiting  bool
	resumeSearch bool
	partial      bool

	openJID      string
	openName     string
	messages     []store.Message
	msgCursor    int
	followLatest bool
	scroll       int
	gen          int
	replyID      string
	replyText    string
	editID       string
	insertFrom   focus
	pick         bool
	pickDir      string
	pickFilter   string
	pickCursor   int
	pickStart    int
	help         bool
	reactPick    bool

	events       <-chan ipc.Live
	watchPending bool
	notice       string
	ready        bool

	drafts      map[string]draft
	typingJID   string
	typingAt    time.Time
	theirWho    string
	theirAt     time.Time
	onlineAt    time.Time
	pendingNew  int
	pendingSeen map[string]bool
	seenMedia   map[string]bool
}

type draft struct {
	text      string
	replyID   string
	replyText string
	editID    string
}

type readyMsg struct{}
type fatalMsg struct{ err error }
type statusMsg struct{ st ipc.Status }
type chatsMsg struct{ chats []store.Chat }
type threadMsg struct {
	gen  int
	jid  string
	name string
	msgs []store.Message
}
type olderMsg struct {
	gen  int
	jid  string
	msgs []store.Message
}
type findHitMsg struct {
	gen   int
	jid   string
	query string
	msgs  []store.Message
}
type sentMsg struct{ text string }
type mediaDone struct{ err error }
type sendFail struct {
	text   string
	editID string
	err    error
}
type errMsg struct{ err error }
type watchMsg struct{ ch <-chan ipc.Live }
type watchLostMsg struct{}
type liveMsg struct{ ev ipc.Live }
type tickMsg time.Time
type newlineMsg struct{}

func Run() int {
	if !isatty.IsTerminal(os.Stdout.Fd()) && !isatty.IsCygwinTerminal(os.Stdout.Fd()) {
		fmt.Fprintln(os.Stderr, "wp needs a terminal")
		return 1
	}
	p := tea.NewProgram(newModel(), tea.WithAltScreen(), tea.WithFilter(programFilter))
	final, err := p.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "wp: %v\n", err)
		return 1
	}
	if m, ok := final.(model); ok && m.notice != "" && !m.ready {
		fmt.Fprintf(os.Stderr, "wp: %s\n", m.notice)
		return 1
	}
	return 0
}

func newModel() model {
	in := textinput.New()
	in.Placeholder = "message"
	in.Prompt = "> "
	in.CharLimit = 4096
	return model{input: in, focus: focusList, followLatest: true}
}

func (m model) Init() tea.Cmd {
	return startSession
}

func startSession() tea.Msg {
	if err := ensureSession(); err != nil {
		return fatalMsg{err}
	}
	return readyMsg{}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.input.Width = max(4, m.threadWidth()-len([]rune(m.input.Prompt)))
		return m, nil
	case fatalMsg:
		m.notice = msg.err.Error()
		return m, m.quit()
	case readyMsg:
		m.ready = true
		return m, tea.Batch(loadStatus, poll(true))
	case tickMsg:
		pairing := m.status == nil || !m.status.Linked
		cmds := []tea.Cmd{loadStatus, poll(pairing)}
		if m.linked() {
			cmds = append(cmds, loadChats)
			if m.openJID != "" && m.followLatest {
				cmds = append(cmds, m.loadThread())
			}
			if time.Since(m.onlineAt) > 20*time.Second {
				m.onlineAt = time.Now()
				cmds = append(cmds, presence("", "available"))
			}
			if cmd := m.ensureWatch(); cmd != nil {
				cmds = append(cmds, cmd)
			}
			if cmd := m.noteCompose(); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
		return m, tea.Batch(cmds...)
	case statusMsg:
		m.status = &msg.st
		return m, m.ensureWatch()
	case watchMsg:
		m.watchPending = false
		m.events = msg.ch
		return m, waitLive(msg.ch)
	case watchLostMsg:
		m.watchPending = false
		m.events = nil
		return m, nil
	case liveMsg:
		cmd := m.onLive(msg.ev)
		return m, tea.Batch(cmd, waitLive(m.events))
	case chatsMsg:
		m.applyChats(msg.chats)
		if m.openJID != "" && len(m.messages) == 0 {
			return m, m.loadThread()
		}
		return m, nil
	case threadMsg:
		if msg.gen != m.gen || msg.jid != m.openJID {
			return m, nil
		}
		if msg.name != "" {
			m.openName = msg.name
		}
		follow := m.followLatest
		m.messages = msg.msgs
		n := len(m.messages)
		if n == 0 {
			m.msgCursor = 0
			m.scroll = 0
			m.followLatest = true
			return m, m.resumeSearchAfter()
		}
		if follow || m.msgCursor < 0 || m.msgCursor >= n {
			m.msgCursor = n - 1
			m.followLatest = true
			m.scroll = 0
			m.clearPending()
			return m, m.resumeSearchAfter()
		}
		m.revealMessage(0)
		return m, m.resumeSearchAfter()
	case olderMsg:
		if msg.gen != m.gen || msg.jid != m.openJID {
			return m, nil
		}
		m.prependOlder(msg.msgs)
		return m, m.resumeSearchAfter()
	case findHitMsg:
		if msg.gen != m.gen || msg.jid != m.openJID || msg.query != m.filter && msg.query != m.pendingFind {
			return m, nil
		}
		m.landFind(msg.msgs)
		return m, nil
	case sentMsg:
		m.notice = ""
		return m, m.loadThread()
	case mediaDone:
		m.notice = ""
		if msg.err != nil {
			m.notice = msg.err.Error()
		}
		return m, m.loadThread()
	case sendFail:
		m.input.SetValue(msg.text)
		m.editID = msg.editID
		m.notice = msg.err.Error()
		return m, m.noteCompose()
	case errMsg:
		m.notice = msg.err.Error()
		return m, nil
	case reactDoneMsg:
		if msg.jid == m.openJID {
			for i := range m.messages {
				if m.messages[i].ID == msg.id {
					m.messages[i].Reactions = msg.reactions
					if m.messages[i].Reactions == nil {
						m.messages[i].Reactions = []store.Reaction{}
					}
					break
				}
			}
		}
		return m, nil
	case newlineMsg:
		if m.focus != focusInput {
			return m, nil
		}
		m.insertNewline()
		return m, m.noteCompose()
	case tea.KeyMsg:
		return m.onKey(msg)
	}
	if m.focus == focusInput {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
	return m, nil
}

var reactChoices = []string{"👍", "❤️", "😂", "😮", "😢", "🙏"}

func (m model) onKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, m.quit()
	}
	if m.status == nil || !m.status.Linked {
		if msg.String() == "q" {
			return m, m.quit()
		}
		return m, nil
	}
	if m.reactPick {
		return m.onReactPick(msg)
	}
	m.notice = ""
	if m.find {
		return m.onFind(msg)
	}
	if m.help {
		return m.onHelp(msg)
	}
	if m.pick {
		return m.onPick(msg)
	}
	if m.focus == focusInput {
		return m.onCompose(msg)
	}
	if m.focus == focusThread {
		return m.onThread(msg)
	}
	switch msg.String() {
	case "q":
		return m, m.quit()
	case "tab":
		m.focus = focusThread
		return m, nil
	case "j", "down":
		return m, m.move(1)
	case "k", "up":
		return m, m.move(-1)
	case "g":
		return m, m.move(-len(m.shown()))
	case "G":
		return m, m.move(len(m.shown()))
	case "pgup", "ctrl+u":
		return m, m.move(-m.listSlots())
	case "pgdown", "ctrl+d":
		return m, m.move(m.listSlots())
	case "enter":
		return m, m.insert()
	case "K":
		m.help = true
		return m, nil
	case "/":
		m.beginFind(focusList)
		return m, nil
	case "r":
		cmd := m.armReply()
		return m, cmd
	case "e":
		cmd := m.armEdit()
		return m, cmd
	case "t":
		m.beginReact()
		return m, nil
	case "a":
		m.startPick()
		return m, nil
	case "p":
		return m.pasteImage()
	case "]":
		return m, m.nextUnread()
	case " ", "space":
		return m, m.spaceVoice()
	}
	return m, nil
}

func (m model) onThread(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q":
		return m, m.quit()
	case "tab":
		m.focus = focusList
		return m, nil
	case "esc":
		m.focus = focusList
		return m, nil
	case "j", "down":
		return m, m.moveMsg(1)
	case "k", "up":
		return m, m.moveMsg(-1)
	case "g":
		return m, m.moveMsg(-len(m.messages))
	case "G":
		if m.pendingNew > 0 || m.partial {
			m.partial = false
			m.followLatest = true
			m.scroll = 0
			m.clearPending()
			return m, m.loadThread()
		}
		return m, m.moveMsg(len(m.messages))
	case "pgup", "ctrl+u":
		return m, m.moveMsg(-(m.threadHeight() / 2))
	case "pgdown", "ctrl+d":
		return m, m.moveMsg(m.threadHeight() / 2)
	case "enter":
		return m, m.insert()
	case "o":
		return m, m.openSelected()
	case "'":
		m.jumpQuote()
		return m, nil
	case "r":
		cmd := m.armReply()
		return m, cmd
	case "e":
		cmd := m.armEdit()
		return m, cmd
	case "t":
		m.beginReact()
		return m, nil
	case "a":
		m.startPick()
		return m, nil
	case "y":
		m.copySelected()
		return m, nil
	case "d":
		return m, m.revokeSelected()
	case "p":
		return m.pasteImage()
	case "]":
		return m, m.nextUnread()
	case " ", "space":
		return m, m.spaceVoice()
	case "n":
		if m.findIn == focusThread && strings.TrimSpace(m.filter) != "" {
			return m, m.findStep(1)
		}
	case "N":
		if m.findIn == focusThread && strings.TrimSpace(m.filter) != "" {
			return m, m.findStep(-1)
		}
	case "K":
		m.help = true
		return m, nil
	case "/":
		m.beginFind(focusThread)
		return m, nil
	}
	return m, nil
}

func (m model) onCompose(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		if m.editID != "" {
			m.editID = ""
			m.input.SetValue("")
			if m.openJID != "" {
				delete(m.drafts, m.openJID)
			}
		}
		m.focus = m.insertFrom
		if m.focus != focusList && m.focus != focusThread {
			m.focus = focusThread
		}
		m.input.Blur()
		return m, nil
	case "tab":
		return m, nil
	case "shift+enter":
		m.insertNewline()
		return m, m.noteCompose()
	case "enter":
		text := strings.TrimSpace(m.composerText())
		if text == "" || m.openJID == "" {
			if m.openJID == "" {
				m.notice = "select a chat"
			}
			return m, nil
		}
		raw := m.composerText()
		reply := m.replyID
		editID := m.editID
		m.input.SetValue("")
		m.replyID = ""
		m.replyText = ""
		m.editID = ""
		delete(m.drafts, m.openJID)
		if editID != "" {
			return m, tea.Batch(edit(m.openJID, editID, raw), m.pauseTyping())
		}
		return m, tea.Batch(send(m.openJID, raw, reply), m.pauseTyping())
	default:
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, tea.Batch(cmd, m.noteCompose())
	}
}

func (m *model) beginFind(where focus) {
	m.find = true
	m.findIn = where
	m.filter = ""
	m.notice = ""
	if where == focusThread {
		m.findAnchor = m.msgCursor
		return
	}
	m.listStart = 0
}

func (m model) onFind(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.find = false
		m.filter = ""
		m.notice = ""
		if m.findIn == focusThread {
			m.msgCursor = m.findAnchor
			m.followLatest = len(m.messages) > 0 && m.msgCursor == len(m.messages)-1
			if m.followLatest {
				m.scroll = 0
			} else {
				m.revealMessage(0)
			}
			return m, nil
		}
		m.cursor = 0
		m.listStart = 0
		return m, m.move(0)
	case "enter":
		m.find = false
		m.notice = ""
		if m.findIn == focusThread {
			if strings.TrimSpace(m.filter) == "" {
				return m, nil
			}
			if matchFrom(m.messages, m.filter, m.findAnchor, 1) < 0 {
				return m, m.searchStored(true)
			}
			return m, nil
		}
		m.cursor = 0
		m.listStart = 0
		return m, m.move(0)
	case "n", "down", "j":
		if m.findIn == focusThread {
			return m, m.findStep(1)
		}
	case "N", "up", "k":
		if m.findIn == focusThread {
			return m, m.findStep(-1)
		}
	case "backspace":
		r := []rune(m.filter)
		if len(r) > 0 {
			m.filter = string(r[:len(r)-1])
		}
		return m, m.applyFind(false)
	default:
		if msg.Type == tea.KeyRunes && msg.String() != "" {
			m.filter += msg.String()
			return m, m.applyFind(false)
		}
		return m, nil
	}
	return m, nil
}

func (m *model) applyFind(step bool) tea.Cmd {
	if m.findIn == focusThread {
		return m.jumpFind(step)
	}
	m.cursor = 0
	m.listStart = 0
	return nil
}

func (m *model) jumpFind(step bool) tea.Cmd {
	if strings.TrimSpace(m.filter) == "" {
		m.msgCursor = m.findAnchor
		m.notice = ""
		return nil
	}
	start := m.findAnchor
	dir := 1
	if step {
		start = m.msgCursor + 1
		if start >= len(m.messages) {
			start = 0
		}
	}
	i := matchFrom(m.messages, m.filter, start, dir)
	if i < 0 {
		if step {
			return m.searchStored(true)
		}
		return m.searchStored(false)
	}
	m.notice = ""
	m.msgCursor = i
	m.followLatest = len(m.messages) > 0 && i == len(m.messages)-1
	if m.followLatest {
		m.scroll = 0
		return nil
	}
	m.revealMessage(dir)
	return nil
}

func (m *model) findStep(dir int) tea.Cmd {
	if strings.TrimSpace(m.filter) == "" || len(m.messages) == 0 {
		return nil
	}
	start := m.msgCursor + dir
	if start < 0 {
		start = len(m.messages) - 1
	}
	if start >= len(m.messages) {
		start = 0
	}
	i := matchFrom(m.messages, m.filter, start, dir)
	if i < 0 {
		return m.searchStored(true)
	}
	m.notice = ""
	m.msgCursor = i
	m.followLatest = i == len(m.messages)-1
	if m.followLatest {
		m.scroll = 0
		return nil
	}
	m.revealMessage(dir)
	return nil
}

func (m *model) searchStored(phone bool) tea.Cmd {
	query := strings.TrimSpace(m.filter)
	if query == "" || m.openJID == "" {
		m.notice = "no match"
		return nil
	}
	if phone && m.findWaiting && m.pendingFind == query {
		m.notice = "no match"
		return nil
	}
	jid := m.openJID
	oldest := ""
	if len(m.messages) > 0 {
		oldest = m.messages[0].ID
	}
	gen := m.gen
	backfill := ""
	if phone {
		m.pendingFind = query
		m.findWaiting = true
		backfill = "backfill"
	}
	return func() tea.Msg {
		resp, err := call(ipc.Request{Cmd: "findchat", Query: jid, Text: query, Reply: oldest, File: backfill})
		if err != nil {
			return errMsg{err}
		}
		msgs := resp.Messages
		if msgs == nil {
			msgs = []store.Message{}
		}
		return findHitMsg{gen: gen, jid: jid, query: query, msgs: msgs}
	}
}

func (m *model) landFind(msgs []store.Message) {
	if len(msgs) == 0 {
		if m.pendingFind != "" && !m.findWaiting {
			m.notice = "no match"
		} else if m.findWaiting {
			m.notice = ""
		} else {
			m.notice = "no match"
		}
		return
	}
	hit := msgs[0].ID
	for i, msg := range m.messages {
		if msg.ID != hit {
			continue
		}
		m.pendingFind = ""
		m.findWaiting = false
		m.notice = ""
		m.msgCursor = i
		m.followLatest = i == len(m.messages)-1
		if m.followLatest {
			m.scroll = 0
		} else {
			m.revealMessage(0)
		}
		return
	}
	if len(m.messages) > 0 && msgs[len(msgs)-1].ID == m.messages[0].ID {
		m.messages = append(msgs[:len(msgs)-1], m.messages...)
	} else {
		m.messages = msgs
		m.partial = true
	}
	m.pendingFind = ""
	m.findWaiting = false
	m.notice = ""
	m.followLatest = false
	m.msgCursor = 0
	for i, msg := range m.messages {
		if msg.ID == hit {
			m.msgCursor = i
			break
		}
	}
	m.revealMessage(0)
}

func (m *model) resumeSearchAfter() tea.Cmd {
	if !m.resumeSearch || m.pendingFind == "" {
		return nil
	}
	m.resumeSearch = false
	q := m.pendingFind
	m.filter = q
	m.findIn = focusThread
	if i := matchFrom(m.messages, q, 0, 1); i >= 0 {
		m.pendingFind = ""
		m.findWaiting = false
		m.notice = ""
		m.msgCursor = i
		m.followLatest = i == len(m.messages)-1
		if !m.followLatest {
			m.revealMessage(0)
		}
		return nil
	}
	m.pendingFind = ""
	m.findWaiting = false
	m.notice = "no match"
	return nil
}

func (m *model) copySelected() {
	msg, ok := m.selected()
	if !ok {
		m.notice = "no message"
		return
	}
	text := msg.FilePath
	if text == "" {
		text = msg.Display()
	}
	if strings.TrimSpace(text) == "" {
		m.notice = "no message"
		return
	}
	if err := clipboard.WriteAll(text); err != nil {
		m.notice = err.Error()
		return
	}
	m.notice = "copied"
}

func (m *model) revokeSelected() tea.Cmd {
	msg, ok := m.selected()
	if !ok {
		m.notice = "no message"
		return nil
	}
	if msg.Deleted {
		m.notice = "already deleted"
		return nil
	}
	jid := m.openJID
	id := msg.ID
	m.notice = "deleting…"
	return func() tea.Msg {
		_, err := callWait(ipc.Request{Cmd: "revoke", Query: jid, Text: id}, 30*time.Second)
		if err != nil {
			return errMsg{err}
		}
		return nil
	}
}

func (m model) pasteImage() (tea.Model, tea.Cmd) {
	if m.openJID == "" {
		m.notice = "select a chat"
		return m, nil
	}
	caption := m.composerText()
	reply := m.replyID
	jid := m.openJID
	m.input.SetValue("")
	m.replyID = ""
	m.replyText = ""
	m.editID = ""
	delete(m.drafts, jid)
	m.notice = "sending…"
	return m, func() tea.Msg {
		path, err := clipboardImage()
		if err != nil {
			return sendFail{text: caption, err: err}
		}
		return sendFile(jid, path, caption, reply)()
	}
}

func (m *model) move(delta int) tea.Cmd {
	shown := m.shown()
	if len(shown) == 0 {
		m.openJID = ""
		m.messages = nil
		return nil
	}
	m.cursor += delta
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor >= len(shown) {
		m.cursor = len(shown) - 1
	}
	m.revealList()
	c := shown[m.cursor]
	if c.JID == m.openJID && delta != 0 {
		return nil
	}
	m.saveDraft(m.openJID)
	pause := m.pauseTyping()
	m.openJID = c.JID
	m.openName = c.Label()
	m.scroll = 0
	m.messages = nil
	m.msgCursor = 0
	m.followLatest = true
	m.clearPending()
	m.theirWho = ""
	m.theirAt = time.Time{}
	m.restoreDraft(c.JID)
	return tea.Batch(m.loadThread(), pause, m.noteCompose())
}

func (m *model) saveDraft(jid string) {
	if jid == "" {
		return
	}
	if m.drafts == nil {
		m.drafts = map[string]draft{}
	}
	if m.input.Value() == "" && m.replyID == "" && m.editID == "" {
		delete(m.drafts, jid)
		return
	}
	m.drafts[jid] = draft{text: m.input.Value(), replyID: m.replyID, replyText: m.replyText, editID: m.editID}
}

func (m *model) restoreDraft(jid string) {
	var d draft
	if m.drafts != nil {
		d = m.drafts[jid]
	}
	m.input.SetValue(d.text)
	m.replyID = d.replyID
	m.replyText = d.replyText
	m.editID = d.editID
}

func (m *model) applyChats(chats []store.Chat) {
	prev := m.openJID
	m.chats = chats
	shown := m.shown()
	m.cursor = 0
	for i, c := range shown {
		if c.JID == prev {
			m.cursor = i
			m.openName = c.Label()
			m.revealList()
			return
		}
	}
	if prev == "" && len(shown) > 0 {
		m.openJID = shown[0].JID
		m.openName = shown[0].Label()
	}
	m.revealList()
}

func (m *model) insert() tea.Cmd {
	if m.focus == focusList || m.focus == focusThread {
		m.insertFrom = m.focus
	}
	m.focus = focusInput
	return m.input.Focus()
}

func (m model) listSlots() int {
	slots := (m.threadHeight() - 1) / 2
	if slots < 1 {
		return 1
	}
	return slots
}

func (m *model) revealList() {
	m.listStart = placeWindow(m.cursor, m.listStart, m.listSlots(), len(m.shown()))
}

func (m *model) onLive(ev ipc.Live) tea.Cmd {
	if ev.Presence != "" {
		if ev.Chat != m.openJID {
			return nil
		}
		if ev.Presence != "composing" {
			m.theirWho = ""
			m.theirAt = time.Time{}
			return nil
		}
		who := ev.Name
		if who == "" {
			who = m.openName
		}
		if who == "" {
			who = "someone"
		}
		m.theirWho = who
		m.theirAt = time.Now()
		return nil
	}
	if ev.Kind == "reaction" {
		if ev.Chat == m.openJID && ev.ID != "" {
			for i := range m.messages {
				if m.messages[i].ID == ev.ID {
					m.messages[i].Reactions = ev.Reactions
					if m.messages[i].Reactions == nil {
						m.messages[i].Reactions = []store.Reaction{}
					}
					break
				}
			}
		}
		return nil
	}
	if ev.Refresh {
		if ev.Chat != m.openJID {
			return nil
		}
		if m.pendingFind != "" {
			m.resumeSearch = true
			if len(m.messages) > 0 {
				return m.loadOlder(m.messages[0].ID)
			}
		}
		if m.followLatest || len(m.messages) == 0 {
			return m.loadThread()
		}
		return m.loadOlder(m.messages[0].ID)
	}
	if ev.Receipt != "" && ev.ID != "" {
		if ev.Chat == m.openJID {
			for i := range m.messages {
				if m.messages[i].ID == ev.ID {
					m.messages[i].Receipt = ev.Receipt
				}
			}
		}
		return nil
	}
	if ev.Deleted && ev.ID != "" {
		latest := false
		if ev.Chat == m.openJID {
			for i := range m.messages {
				if m.messages[i].ID == ev.ID {
					m.messages[i].Deleted = true
					m.messages[i].Text = "deleted"
					m.messages[i].Caption = ""
					m.messages[i].Kind = ""
					m.messages[i].FilePath = ""
					latest = i == len(m.messages)-1
				}
			}
		}
		if latest {
			for i := range m.chats {
				if m.chats[i].JID == ev.Chat {
					m.chats[i].LastText = "deleted"
					break
				}
			}
		}
		return nil
	}
	if ev.Edited && ev.ID != "" {
		if ev.Chat == m.openJID {
			for i := range m.messages {
				if m.messages[i].ID == ev.ID {
					m.messages[i].Edited = true
					m.messages[i].Text = ev.Text
				}
			}
		}
		for i := range m.chats {
			if m.chats[i].JID == ev.Chat && !m.chats[i].LastAt.After(ev.At) {
				m.chats[i].LastText = ev.Text
				m.chats[i].LastAt = ev.At
			}
		}
		return nil
	}
	if ev.Text == "[deleted]" && ev.ID != "" && ev.Chat == m.openJID {
		kept := m.messages[:0]
		for _, msg := range m.messages {
			if msg.ID != ev.ID {
				kept = append(kept, msg)
			}
		}
		m.messages = kept
		if m.msgCursor >= len(m.messages) {
			m.msgCursor = len(m.messages) - 1
		}
		if m.msgCursor < 0 {
			m.msgCursor = 0
		}
		m.followLatest = len(m.messages) == 0 || m.msgCursor == len(m.messages)-1
	}
	known := false
	for i := range m.chats {
		if m.chats[i].JID != ev.Chat {
			continue
		}
		known = true
		if ev.Text != "[deleted]" {
			m.chats[i].LastText = ev.Text
			m.chats[i].LastAt = ev.At
		}
		if ev.Chat != m.openJID && !ev.FromMe && ev.Text != "[deleted]" {
			m.chats[i].Unread++
			notify(m.chats[i].Label(), oneLine(ev.Text))
		}
		break
	}
	if !known && ev.Chat != "" && ev.Chat != m.openJID && !ev.FromMe && ev.Text != "" && ev.Text != "[deleted]" {
		name := ev.Name
		if name == "" {
			name = "message"
		}
		notify(name, oneLine(ev.Text))
	}
	auto := m.autoMedia(ev)
	if ev.Chat == m.openJID && ev.Text != "[deleted]" {
		if ev.ID != "" && m.hasMessage(ev.ID) {
			m.patchLive(ev)
			return auto
		}
		if m.followLatest {
			m.scroll = 0
			return tea.Batch(m.loadThread(), auto)
		}
		if ev.ID != "" {
			if m.pendingSeen == nil {
				m.pendingSeen = map[string]bool{}
			}
			if !m.pendingSeen[ev.ID] {
				m.pendingSeen[ev.ID] = true
				m.pendingNew++
			}
		}
		return auto
	}
	return auto
}

func (m *model) hasMessage(id string) bool {
	for _, msg := range m.messages {
		if msg.ID == id {
			return true
		}
	}
	return false
}

func (m *model) patchLive(ev ipc.Live) {
	for i := range m.messages {
		if m.messages[i].ID != ev.ID {
			continue
		}
		if ev.Kind != "" && ev.Kind != "reaction" {
			m.messages[i].Kind = ev.Kind
		}
		if ev.Path != "" {
			m.messages[i].FilePath = ev.Path
		}
		return
	}
}

func (m *model) clearPending() {
	m.pendingNew = 0
	m.pendingSeen = nil
}

func (m model) typingShown() bool {
	return m.theirWho != "" && time.Since(m.theirAt) < 8*time.Second
}

func mediaAction(kind string, fromMe bool) string {
	if kind == "image" && !fromMe {
		return "open"
	}
	if kind == "voice" {
		return "play"
	}
	return ""
}

func (m *model) autoMedia(ev ipc.Live) tea.Cmd {
	if ev.Path == "" || ev.ID == "" || ev.Chat != m.openJID || strings.HasPrefix(ev.Text, "view once") {
		return nil
	}
	action := mediaAction(ev.Kind, ev.FromMe)
	if action == "" {
		return nil
	}
	key := ev.Chat + "\n" + ev.ID
	if m.seenMedia[key] {
		return nil
	}
	if m.seenMedia == nil {
		m.seenMedia = map[string]bool{}
	}
	m.seenMedia[key] = true
	path := ev.Path
	return func() tea.Msg {
		var err error
		if action == "open" {
			if inlineImages() {
				return nil
			}
			err = openPath(path)
		} else {
			err = playAudio(path)
		}
		if err != nil {
			return errMsg{err}
		}
		return nil
	}
}

func (m model) shown() []store.Chat {
	if m.findIn == focusThread {
		return m.chats
	}
	return filterChats(m.chats, m.filter)
}

func (m model) linked() bool {
	return m.status != nil && m.status.Linked
}

func (m model) threadHeight() int {
	h := m.height - m.composerRows()
	if m.replyID != "" || m.editID != "" {
		h--
	}
	if m.typingShown() {
		h--
	}
	if h < 1 {
		return 1
	}
	return h
}

func (m model) composerRows() int {
	if m.find || m.pick {
		return 1
	}
	n := strings.Count(m.composerText(), "\n") + 1
	if n > 6 {
		return 6
	}
	if n < 1 {
		return 1
	}
	return n
}

func (m model) listWidth() int {
	return listWidthFor(m.width)
}

func (m model) threadWidth() int {
	w := m.width - m.listWidth() - 3
	if w < 8 {
		return 8
	}
	return w
}

func (m *model) loadThread() tea.Cmd {
	if m.openJID == "" {
		return nil
	}
	m.partial = false
	m.gen++
	gen := m.gen
	jid := m.openJID
	return func() tea.Msg {
		resp, err := call(ipc.Request{Cmd: "read", Query: jid, Limit: 300})
		if err != nil {
			return errMsg{err}
		}
		name := ""
		msgs := resp.Messages
		if resp.Chat != nil {
			name = resp.Chat.Label()
		}
		if msgs == nil {
			msgs = []store.Message{}
		}
		return threadMsg{gen: gen, jid: jid, name: name, msgs: msgs}
	}
}

func loadStatus() tea.Msg {
	resp, err := call(ipc.Request{Cmd: "status"})
	if err != nil {
		return errMsg{err}
	}
	if resp.Status == nil {
		return errMsg{fmt.Errorf("missing status")}
	}
	return statusMsg{st: *resp.Status}
}

func loadChats() tea.Msg {
	resp, err := call(ipc.Request{Cmd: "chats", Limit: 200})
	if err != nil {
		return errMsg{err}
	}
	return chatsMsg{chats: resp.Chats}
}

func (m *model) ensureWatch() tea.Cmd {
	if !m.linked() || m.events != nil || m.watchPending {
		return nil
	}
	m.watchPending = true
	return openWatch
}

func openWatch() tea.Msg {
	ch, err := watch()
	if err != nil {
		return watchLostMsg{}
	}
	return watchMsg{ch: ch}
}

func waitLive(ch <-chan ipc.Live) tea.Cmd {
	if ch == nil {
		return nil
	}
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return watchLostMsg{}
		}
		return liveMsg{ev: ev}
	}
}

func (m *model) selected() (store.Message, bool) {
	if m.msgCursor < 0 || m.msgCursor >= len(m.messages) {
		return store.Message{}, false
	}
	msg := m.messages[m.msgCursor]
	if msg.ID == "" {
		return store.Message{}, false
	}
	return msg, true
}

func (m *model) moveMsg(delta int) tea.Cmd {
	if len(m.messages) == 0 {
		return nil
	}
	atOldest := m.msgCursor <= 0
	next := m.msgCursor + delta
	if next < 0 {
		next = 0
	}
	if next >= len(m.messages) {
		next = len(m.messages) - 1
	}
	m.msgCursor = next
	m.followLatest = next == len(m.messages)-1
	var cmd tea.Cmd
	if delta < 0 && atOldest && next == 0 && m.messages[0].ID != "" {
		cmd = m.loadOlder(m.messages[0].ID)
	}
	if m.followLatest {
		m.scroll = 0
		if m.pendingNew > 0 {
			m.clearPending()
			return m.loadThread()
		}
		return cmd
	}
	m.revealMessage(delta)
	return cmd
}

func (m *model) loadOlder(beforeID string) tea.Cmd {
	if m.openJID == "" || beforeID == "" {
		return nil
	}
	gen := m.gen
	jid := m.openJID
	return func() tea.Msg {
		resp, err := call(ipc.Request{Cmd: "older", Query: jid, Text: beforeID})
		if err != nil {
			return errMsg{err}
		}
		msgs := resp.Messages
		if msgs == nil {
			msgs = []store.Message{}
		}
		return olderMsg{gen: gen, jid: jid, msgs: msgs}
	}
}

func (m *model) prependOlder(msgs []store.Message) {
	if len(msgs) == 0 || len(m.messages) == 0 {
		return
	}
	have := map[string]bool{}
	for _, msg := range m.messages {
		if msg.ID != "" {
			have[msg.ID] = true
		}
	}
	var add []store.Message
	for _, msg := range msgs {
		if msg.ID == "" || have[msg.ID] {
			continue
		}
		add = append(add, msg)
	}
	if len(add) == 0 {
		return
	}
	keep := ""
	if m.msgCursor >= 0 && m.msgCursor < len(m.messages) {
		keep = m.messages[m.msgCursor].ID
	}
	m.messages = append(add, m.messages...)
	m.followLatest = false
	if keep == "" {
		return
	}
	for i, msg := range m.messages {
		if msg.ID == keep {
			m.msgCursor = i
			return
		}
	}
}

func (m model) quit() tea.Cmd {
	jid := m.typingJID
	return func() tea.Msg {
		if jid != "" {
			_, _ = call(ipc.Request{Cmd: "presence", Query: jid, Text: "paused"})
		}
		_, _ = call(ipc.Request{Cmd: "presence", Text: "unavailable"})
		return tea.Quit()
	}
}

func (m *model) noteCompose() tea.Cmd {
	if strings.TrimSpace(m.composerText()) == "" || m.openJID == "" {
		return m.pauseTyping()
	}
	if m.typingJID == m.openJID && time.Since(m.typingAt) < 8*time.Second {
		return nil
	}
	m.typingJID = m.openJID
	m.typingAt = time.Now()
	return presence(m.openJID, "composing")
}

func (m *model) pauseTyping() tea.Cmd {
	if m.typingJID == "" {
		return nil
	}
	jid := m.typingJID
	m.typingJID = ""
	m.typingAt = time.Time{}
	return presence(jid, "paused")
}

func presence(jid, state string) tea.Cmd {
	return func() tea.Msg {
		_, _ = call(ipc.Request{Cmd: "presence", Query: jid, Text: state})
		return nil
	}
}

func (m *model) revealMessage(dir int) {
	if len(m.messages) == 0 {
		m.scroll = 0
		return
	}
	if m.msgCursor < 0 {
		m.msgCursor = 0
	}
	if m.msgCursor >= len(m.messages) {
		m.msgCursor = len(m.messages) - 1
	}
	lines := threadBody(m.messages, m.openName, m.threadWidth(), m.groupChat())
	first, last := -1, -1
	for i, ln := range lines {
		if ln.Msg != m.msgCursor {
			continue
		}
		if first < 0 {
			first = i
		}
		last = i
	}
	if first < 0 {
		m.scroll = 0
		return
	}
	h := m.threadHeight() - 1
	if h < 1 {
		h = 1
	}
	end := len(lines) - m.scroll
	if end > len(lines) {
		end = len(lines)
	}
	start := end - h
	if start < 0 {
		start = 0
	}
	minScroll := len(lines) - h - first
	if minScroll < 0 {
		minScroll = 0
	}
	maxScroll := len(lines) - last - 1
	if maxScroll < 0 {
		maxScroll = 0
	}
	fits := minScroll <= maxScroll
	if fits && first >= start && last < end {
		return
	}
	if !fits {
		if dir > 0 {
			m.scroll = maxScroll
		} else if dir < 0 {
			m.scroll = minScroll
		} else if first >= start && first < end {
			return
		} else if last < start {
			m.scroll = minScroll
		} else {
			m.scroll = maxScroll
		}
		return
	}
	if dir < 0 || last < start {
		m.scroll = minScroll
		return
	}
	m.scroll = maxScroll
}

func (m *model) jumpQuote() {
	msg, ok := m.selected()
	if !ok || msg.ReplyTo == "" {
		return
	}
	for i, q := range m.messages {
		if q.ID != msg.ReplyTo {
			continue
		}
		dir := -1
		if i > m.msgCursor {
			dir = 1
		}
		m.msgCursor = i
		m.followLatest = i == len(m.messages)-1
		if m.followLatest {
			m.scroll = 0
			return
		}
		m.revealMessage(dir)
		return
	}
	m.notice = "not in the loaded history"
}

func (m model) groupChat() bool {
	return strings.HasSuffix(m.openJID, "@g.us")
}

func (m *model) openSelected() tea.Cmd {
	if m.openJID == "" {
		m.notice = "select a chat"
		return nil
	}
	msg, ok := m.selected()
	if !ok || (msg.Kind == "" && msg.FilePath == "") {
		if strings.HasPrefix(msg.Text, "view once") {
			m.notice = "unavailable"
			return nil
		}
		m.notice = "no file"
		return nil
	}
	m.notice = "opening…"
	return openMedia(m.openJID, msg.ID)
}

func (m *model) armReply() tea.Cmd {
	if m.openJID == "" {
		m.notice = "select a chat"
		return nil
	}
	msg, ok := m.selected()
	if !ok {
		m.notice = "no message"
		return nil
	}
	m.editID = ""
	m.replyID = msg.ID
	m.replyText = oneLine(msg.Preview())
	m.focus = focusInput
	return m.input.Focus()
}

func (m *model) armEdit() tea.Cmd {
	if m.openJID == "" {
		m.notice = "select a chat"
		return nil
	}
	msg, ok := m.selected()
	if !ok {
		m.notice = "no message"
		return nil
	}
	if !msg.FromMe {
		m.notice = "not your message"
		return nil
	}
	if msg.Deleted {
		m.notice = "already deleted"
		return nil
	}
	if msg.Kind != "" {
		m.notice = "only text messages"
		return nil
	}
	m.replyID = ""
	m.replyText = ""
	m.editID = msg.ID
	m.input.SetValue(msg.Text)
	m.focus = focusInput
	return m.input.Focus()
}

func (m *model) beginReact() {
	if m.openJID == "" {
		m.notice = "select a chat"
		return
	}
	if _, ok := m.selected(); !ok {
		m.notice = "no message"
		return
	}
	m.reactPick = true
	parts := make([]string, 0, len(reactChoices))
	for i, e := range reactChoices {
		parts = append(parts, fmt.Sprintf("%d %s", i+1, e))
	}
	m.notice = strings.Join(parts, "  ")
}

func (m model) onReactPick(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q":
		m.reactPick = false
		m.notice = ""
		return m, nil
	case "1", "2", "3", "4", "5", "6":
		i := int(msg.String()[0] - '1')
		if i < 0 || i >= len(reactChoices) {
			return m, nil
		}
		emoji := reactChoices[i]
		m.reactPick = false
		m.notice = ""
		return m, m.sendReact(emoji)
	default:
		return m, nil
	}
}

func (m *model) sendReact(emoji string) tea.Cmd {
	msg, ok := m.selected()
	if !ok || m.openJID == "" {
		m.notice = "no message"
		return nil
	}
	jid, id := m.openJID, msg.ID
	mine := ""
	for _, r := range msg.Reactions {
		if r.FromMe {
			mine = r.Emoji
			break
		}
	}
	if mine != "" && mine == emoji {
		emoji = ""
	}
	return func() tea.Msg {
		resp, err := call(ipc.Request{Cmd: "react", Query: jid, Text: id, Reply: emoji})
		if err != nil {
			return errMsg{err}
		}
		if !resp.OK {
			return errMsg{fmt.Errorf("%s", resp.Error)}
		}
		list := []store.Reaction{}
		if len(resp.Messages) > 0 {
			list = resp.Messages[0].Reactions
		}
		return reactDoneMsg{jid: jid, id: id, reactions: list}
	}
}

type reactDoneMsg struct {
	jid       string
	id        string
	reactions []store.Reaction
}

func openMedia(jid, id string) tea.Cmd {
	return func() tea.Msg {
		resp, err := callWait(ipc.Request{Cmd: "media", Query: jid, Text: id}, 3*time.Minute)
		if err != nil {
			return mediaDone{err: err}
		}
		if err := openPath(resp.Path); err != nil {
			return mediaDone{err: err}
		}
		return mediaDone{}
	}
}

func send(jid, text, reply string) tea.Cmd {
	return func() tea.Msg {
		_, err := call(ipc.Request{Cmd: "send", Args: []string{jid, text}, Reply: reply})
		if err != nil {
			return sendFail{text: text, err: err}
		}
		return sentMsg{text: text}
	}
}

func edit(jid, id, text string) tea.Cmd {
	return func() tea.Msg {
		_, err := callWait(ipc.Request{Cmd: "edit", Query: jid, Text: id, Reply: text}, 30*time.Second)
		if err != nil {
			return sendFail{text: text, editID: id, err: err}
		}
		return sentMsg{text: text}
	}
}

func sendFile(jid, path, caption, reply string) tea.Cmd {
	return func() tea.Msg {
		_, err := callWait(ipc.Request{
			Cmd:   "send",
			Query: jid,
			Text:  caption,
			File:  path,
			Reply: reply,
		}, 3*time.Minute)
		if err != nil {
			return errMsg{err}
		}
		return sentMsg{text: caption}
	}
}

func poll(pairing bool) tea.Cmd {
	d := 2 * time.Second
	if pairing {
		d = 400 * time.Millisecond
	}
	return tea.Tick(d, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m model) View() string {
	if m.width == 0 || m.height == 0 {
		return "starting\n"
	}
	if !m.ready && m.notice == "" {
		return "starting the session\n"
	}
	if m.notice != "" && (m.status == nil || !m.status.Linked) && !m.ready {
		return m.notice + "\n"
	}
	if m.status == nil || !m.status.Linked {
		return m.pairingView()
	}
	return m.mainView()
}

func (m model) pairingView() string {
	var b strings.Builder
	b.WriteString("\n  ")
	b.WriteString(styleAccent.Render("Link a device"))
	b.WriteString("\n\n  ")
	b.WriteString(styleDim.Render("On the phone: WhatsApp → Linked devices → Link a device"))
	b.WriteString("\n\n")
	if m.status != nil && m.status.Code != "" {
		b.WriteString(qrBlocks(m.status.Code))
		b.WriteByte('\n')
	} else if m.ready {
		b.WriteString("  waiting for a QR code\n")
	}
	if m.notice != "" {
		fmt.Fprintf(&b, "\n  %s\n", m.notice)
	}
	b.WriteString("\n  ")
	b.WriteString(styleDim.Render("q quit"))
	b.WriteByte('\n')
	return b.String()
}

func (m model) mainView() string {
	bodyH := m.threadHeight()
	left := m.listLines(m.shown(), bodyH)
	right := m.threadLines(bodyH)
	var b strings.Builder
	div := styleDim.Render(" │ ")
	lw, tw := m.listWidth(), m.threadWidth()
	for i := 0; i < bodyH; i++ {
		l, r := "", ""
		if i < len(left) {
			l = left[i]
		}
		if i < len(right) {
			r = right[i]
		}
		fmt.Fprintf(&b, "%s%s%s\n", fitWidth(l, lw), div, fitPane(r, tw))
	}
	if m.typingShown() {
		line := styleDim.Render(pad(trunc(m.theirWho+" is typing", tw), tw))
		fmt.Fprintf(&b, "%s%s%s\n", pad("", lw), div, line)
	}
	if m.editID != "" {
		edit := styleDim.Render(pad(trunc("edit", tw), tw))
		fmt.Fprintf(&b, "%s%s%s\n", pad("", lw), div, edit)
	} else if m.replyID != "" {
		reply := styleDim.Render(pad(trunc("reply: "+oneLine(m.replyText), tw), tw))
		fmt.Fprintf(&b, "%s%s%s\n", pad("", lw), div, reply)
	}
	rows := m.composerRowsText(tw)
	for i, row := range rows {
		left := pad("", lw)
		if i == len(rows)-1 {
			left = m.statusCell(lw)
		}
		fmt.Fprintf(&b, "%s%s%s\n", fitWidth(left, lw), div, fitPane(row, tw))
	}
	return b.String()
}

func (m model) composerRowsText(w int) []string {
	if m.find || m.pick {
		s := m.input.View()
		if m.find {
			label := "contact: "
			if m.findIn == focusThread {
				label = "message: "
			}
			s = blockCursor(label+m.filter, len([]rune(label+m.filter)))
		}
		if m.pick {
			s = shortPath(m.pickDir) + "/" + m.pickFilter
			s = blockCursor(s, len([]rune(s)))
		}
		return []string{fitWidth(s, w)}
	}
	val := m.composerText()
	lines := strings.Split(val, "\n")
	if len(lines) == 0 {
		lines = []string{""}
	}
	if m.focus == focusInput {
		line, col := cursorLine(val, m.input.Position())
		if line >= len(lines) {
			line = len(lines) - 1
		}
		lines[line] = blockCursor(lines[line], col)
	}
	if len(lines) > 6 {
		lines = lines[len(lines)-6:]
	}
	out := make([]string, len(lines))
	for i, ln := range lines {
		if i == 0 {
			ln = "> " + ln
		}
		out[i] = fitWidth(ln, w)
	}
	return out
}

func blockCursor(line string, col int) string {
	r := []rune(line)
	if col < 0 {
		col = 0
	}
	if col >= len(r) {
		return line + styleBlock.Render(" ")
	}
	return string(r[:col]) + styleBlock.Render(string(r[col])) + string(r[col+1:])
}

func cursorLine(val string, pos int) (int, int) {
	line, col := 0, 0
	for i, r := range []rune(val) {
		if i == pos {
			return line, col
		}
		if r == '\n' {
			line++
			col = 0
			continue
		}
		col++
	}
	return line, col
}

func fitWidth(s string, w int) string {
	if w < 1 {
		return ""
	}
	if lipgloss.Width(s) > w {
		return trunc(s, w)
	}
	if extra := w - lipgloss.Width(s); extra > 0 {
		s += strings.Repeat(" ", extra)
	}
	return s
}

// fitPane pads or truncates a thread cell, leaving kitty image escapes alone.
func fitPane(s string, w int) string {
	if strings.Contains(s, "\x1b_G") {
		return s
	}
	return fitWidth(s, w)
}

func (m model) composerText() string {
	return strings.ReplaceAll(m.input.Value(), "\u2028", "\n")
}

func (m *model) insertNewline() {
	v := []rune(m.input.Value())
	pos := m.input.Position()
	if pos < 0 {
		pos = 0
	}
	if pos > len(v) {
		pos = len(v)
	}
	next := make([]rune, 0, len(v)+1)
	next = append(next, v[:pos]...)
	next = append(next, '\u2028')
	next = append(next, v[pos:]...)
	m.input.SetValue(string(next))
	m.input.SetCursor(pos + 1)
}

func (m *model) nextUnread() tea.Cmd {
	shown := m.shown()
	for i := 1; i < len(shown); i++ {
		j := (m.cursor + i) % len(shown)
		if shown[j].Unread > 0 {
			return m.move(j - m.cursor)
		}
	}
	m.notice = "no unread"
	return nil
}

func (m *model) spaceVoice() tea.Cmd {
	if acted, paused := toggleVoice(); acted {
		if paused {
			m.notice = "paused"
		} else {
			m.notice = "playing"
		}
		return nil
	}
	msg, ok := m.selected()
	if !ok || (msg.Kind != "voice" && msg.Kind != "audio") {
		return nil
	}
	if msg.FilePath != "" {
		path := msg.FilePath
		m.notice = "playing"
		return func() tea.Msg {
			if err := playAudio(path); err != nil {
				return errMsg{err}
			}
			return nil
		}
	}
	jid, id := m.openJID, msg.ID
	m.notice = "playing"
	return func() tea.Msg {
		resp, err := callWait(ipc.Request{Cmd: "media", Query: jid, Text: id}, 3*time.Minute)
		if err != nil {
			return errMsg{err}
		}
		if err := playAudio(resp.Path); err != nil {
			return errMsg{err}
		}
		return nil
	}
}

func programFilter(_ tea.Model, msg tea.Msg) tea.Msg {
	s, ok := msg.(fmt.Stringer)
	if !ok {
		return msg
	}
	if shiftEnter(s.String()) {
		return newlineMsg{}
	}
	return msg
}

func shiftEnter(raw string) bool {
	return strings.Contains(raw, "13;2u") || strings.Contains(raw, "27;2;13") ||
		strings.Contains(raw, "49 51 59 50 117") || strings.Contains(raw, "50 55 59 50 59 49 51 126")
}

func (m model) listLines(shown []store.Chat, height int) []string {
	w := m.listWidth()
	title := "chats"
	if m.findIn != focusThread && (m.find || m.filter != "") {
		title = "filter"
	}
	lines := []string{styleDim.Render(pad(title, w))}
	slots := (height - 1) / 2
	if slots < 1 {
		slots = 1
	}
	if len(shown) == 0 {
		lines = append(lines, pad("no chats", w), pad("", w))
	} else {
		start := placeWindow(m.cursor, m.listStart, slots, len(shown))
		end := start + slots
		if end > len(shown) {
			end = len(shown)
		}
		for i := start; i < end; i++ {
			top, preview := m.paintChat(shown[i], i == m.cursor, m.focus == focusList, w)
			lines = append(lines, top, preview)
		}
	}
	for len(lines) < height {
		lines = append(lines, pad("", w))
	}
	if len(lines) > height {
		return lines[:height]
	}
	return lines
}

func (m model) paintChat(c store.Chat, selected, focused bool, w int) (string, string) {
	preview := pad(trunc(oneLine(c.LastText), w), w)
	meta := formatTime(c.LastAt)
	if c.Unread > 0 {
		if meta != "" {
			meta += " "
		}
		meta += fmt.Sprintf("%d", c.Unread)
	}
	label := c.Label() + m.rowMarks(c)
	if selected {
		st := styleSelectDim
		if focused {
			st = styleSelect
		}
		row := pad(rowPlain(label, meta, w), w)
		return st.Render(row), st.Render(preview)
	}
	timeStr := formatTime(c.LastAt)
	unread := ""
	if c.Unread > 0 {
		unread = fmt.Sprintf("%d", c.Unread)
	}
	rightW := cellWidth(timeStr)
	if unread != "" {
		if timeStr != "" {
			rightW++
		}
		rightW += cellWidth(unread)
	}
	nameW := w - rightW
	if rightW > 0 {
		nameW--
	}
	if nameW < 1 {
		nameW = 1
	}
	mark := m.rowMarks(c)
	nameW -= cellWidth(mark)
	if nameW < 1 {
		nameW = 1
	}
	name := trunc(c.Label(), nameW)
	gap := w - cellWidth(name) - cellWidth(mark) - rightW
	if gap < 0 {
		gap = 0
	}
	nameStyle := styleAccent
	if strings.HasSuffix(c.JID, "@g.us") {
		nameStyle = styleGroup
	}
	var b strings.Builder
	b.WriteString(nameStyle.Render(name))
	b.WriteString(styleDim.Render(mark))
	b.WriteString(strings.Repeat(" ", gap))
	if timeStr != "" {
		b.WriteString(styleDim.Render(timeStr))
	}
	if unread != "" {
		if timeStr != "" {
			b.WriteByte(' ')
		}
		b.WriteString(styleAccent.Render(unread))
	}
	return fitWidth(b.String(), w), styleDim.Render(preview)
}

func (m model) rowMarks(c store.Chat) string {
	var b strings.Builder
	if strings.HasSuffix(c.JID, "@g.us") {
		b.WriteString(" group")
	}
	if d, ok := m.drafts[c.JID]; ok && (d.text != "" || d.replyID != "" || d.editID != "") {
		b.WriteString(" draft")
	}
	return b.String()
}

func (m model) threadLines(height int) []string {
	if m.help {
		return m.helpLines(height)
	}
	if m.pick {
		return m.pickLines(height)
	}
	w := m.threadWidth()
	title := m.openName
	if title == "" {
		title = "messages"
	}
	foot := 0
	if m.pendingNew > 0 && !m.followLatest && height > 2 {
		foot = 1
	}
	bodyH := height - 1 - foot
	if bodyH < 1 {
		bodyH = 1
	}
	body := fitThread(threadBody(m.messages, m.openName, w, m.groupChat()), bodyH, m.scroll)
	targetID := ""
	if m.msgCursor >= 0 && m.msgCursor < len(m.messages) {
		targetID = m.messages[m.msgCursor].ReplyTo
	}
	out := make([]string, 0, height)
	out = append(out, styleBold.Render(pad(trunc(title, w), w)))
	prev := -2
	for _, ln := range body {
		text := ln.Text
		focused := m.focus == focusThread
		selected := focused && ln.Msg >= 0 && ln.Msg == m.msgCursor
		target := focused && targetID != "" && ln.Msg >= 0 && ln.Msg != m.msgCursor && ln.Msg < len(m.messages) && m.messages[ln.Msg].ID == targetID
		if target && ln.Msg != prev {
			text = "▸ " + text
		}
		prev = ln.Msg
		mark, markStyle := receiptIcon(ln.Receipt)
		right := ln.Mine && !selected && !ln.Image && ln.Msg >= 0
		var line string
		switch {
		case ln.Day:
			line = styleDim.Render(centerText(text, w))
		case ln.Msg < 0:
			line = pad("", w)
		case ln.Image:
			line = text
		case selected:
			full := text
			if mark != "" {
				full += " " + mark
			}
			full = trunc(full, w)
			if ln.Mine && !ln.Image {
				full = padLeft(full, w)
			} else {
				full = pad(full, w)
			}
			line = styleSelect.Render(full)
		case target:
			line = alignLine(styleDim.Render(trunc(text, w)), w, right)
		case ln.Media:
			line = alignLine(styleMedia.Render(trunc(text, w)), w, right)
		case ln.Time:
			line = alignLine(markedText(text, mark, markStyle, styleDim, w), w, right)
		case ln.Deleted:
			line = alignLine(styleDim.Render(trunc(text, w)), w, right)
		case ln.Quote:
			line = alignLine(styleDim.Render(trunc(text, w)), w, right)
		case ln.React && ln.MineReact:
			line = alignLine(styleAccent.Render(trunc(text, w)), w, right)
		case ln.React:
			line = alignLine(styleDim.Render(trunc(text, w)), w, right)
		case ln.Mine && ln.Reply:
			line = alignLine(styleAccentEm.Render(trunc(text, w)), w, right)
		case ln.Mine:
			line = alignLine(styleAccent.Render(trunc(text, w)), w, right)
		case ln.Reply:
			line = alignLine(styleItalic.Render(trunc(text, w)), w, right)
		default:
			line = alignLine(trunc(text, w), w, false)
		}
		out = append(out, line)
	}
	for len(out) < height-foot {
		out = append(out, pad("", w))
	}
	if foot == 1 {
		label := "1 new"
		if m.pendingNew != 1 {
			label = fmt.Sprintf("%d new", m.pendingNew)
		}
		out = append(out, styleDim.Render(centerText(label, w)))
	}
	return out[:height]
}

func (m model) statusCell(w int) string {
	word, st := "disconnected", styleBad
	if m.notice != "" {
		word, st = m.notice, styleAccent
	} else if m.status != nil && m.status.Connected {
		word, st = "connected", styleDelivered
	} else if m.status != nil && m.status.Pairing {
		word, st = "pairing", styleAccent
	}
	return st.Render(pad(trunc(word, w), w))
}

func (m model) onHelp(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return m, m.quit()
	case "esc", "K":
		m.help = false
		return m, nil
	default:
		return m, nil
	}
}

func (m model) helpLines(height int) []string {
	w := m.threadWidth()
	lines := []string{
		"j k         move",
		"g G         first and last",
		"ctrl-u d    page",
		"tab         switch pane",
		"enter       write",
		"esc         normal mode",
		"/           find contact or message",
		"n N         next and previous match",
		"'           quoted message",
		"]           next unread",
		"space       pause or play voice",
		"shift+enter newline",
		"r           reply",
		"e           edit",
		"t           react",
		"o           open file",
		"y           copy message",
		"d           delete for everyone",
		"a           choose a file",
		"p           paste image",
		"K           bindings",
		"q           quit",
	}
	out := []string{styleBold.Render(pad("bindings", w))}
	for _, line := range lines {
		if len(out) >= height {
			break
		}
		out = append(out, pad(trunc(line, w), w))
	}
	for len(out) < height {
		out = append(out, pad("", w))
	}
	return out[:height]
}

func receiptIcon(state string) (string, lipgloss.Style) {
	switch state {
	case "read":
		return "✓✓", styleAccent
	case "delivered":
		return "✓✓", styleSent
	case "sent":
		return "✓", styleSent
	default:
		return "", styleDim
	}
}

func markedText(text, mark string, markStyle, base lipgloss.Style, w int) string {
	if mark == "" {
		return base.Render(trunc(text, w))
	}
	room := w - lipgloss.Width(mark) - 1
	if room < 1 {
		room = 1
	}
	return base.Render(trunc(text, room)) + " " + markStyle.Render(mark)
}

func alignLine(rendered string, w int, right bool) string {
	if lipgloss.Width(rendered) > w {
		return trunc(rendered, w)
	}
	extra := w - lipgloss.Width(rendered)
	if extra <= 0 {
		return rendered
	}
	spaces := strings.Repeat(" ", extra)
	if right {
		return spaces + rendered
	}
	return rendered + spaces
}

func centerText(s string, w int) string {
	s = trunc(s, w)
	extra := w - cellWidth(s)
	if extra <= 0 {
		return s
	}
	left := extra / 2
	return strings.Repeat(" ", left) + s + strings.Repeat(" ", extra-left)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
