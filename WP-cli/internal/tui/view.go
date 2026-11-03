package tui

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/mdp/qrterminal/v3"

	"wp/internal/store"
)

func chatMatches(c store.Chat, query string) bool {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return true
	}
	if strings.Contains(strings.ToLower(c.Label()), query) || strings.Contains(strings.ToLower(c.JID), query) {
		return true
	}
	digits := onlyDigits(query)
	if digits == "" {
		return strings.Contains(c.Phone, query)
	}
	return strings.Contains(c.Phone, digits) || strings.Contains(onlyDigits(c.JID), digits)
}

func messageMatches(m store.Message, query string) bool {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return false
	}
	hay := strings.ToLower(strings.Join([]string{m.Text, m.Caption, m.SenderName, m.ReplyText, m.FileName}, "\n"))
	if strings.Contains(hay, query) {
		return true
	}
	digits := onlyDigits(query)
	return len(digits) >= 3 && strings.Contains(onlyDigits(hay), digits)
}

func matchFrom(msgs []store.Message, query string, start, dir int) int {
	n := len(msgs)
	if n == 0 || dir == 0 {
		return -1
	}
	if start < 0 || start >= n {
		start = 0
	}
	for step := 0; step < n; step++ {
		i := start + dir*step
		i = ((i % n) + n) % n
		if messageMatches(msgs[i], query) {
			return i
		}
	}
	return -1
}

func onlyDigits(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func filterChats(chats []store.Chat, query string) []store.Chat {
	if strings.TrimSpace(query) == "" {
		return chats
	}
	out := make([]store.Chat, 0, len(chats))
	for _, c := range chats {
		if chatMatches(c, query) {
			out = append(out, c)
		}
	}
	return out
}

// placeWindow keeps the cursor inside the visible rows and scrolls only when it leaves them.
func placeWindow(cursor, start, slots, n int) int {
	if slots < 1 {
		slots = 1
	}
	if n < 1 {
		return 0
	}
	if cursor < 0 {
		cursor = 0
	}
	if cursor >= n {
		cursor = n - 1
	}
	if start < 0 {
		start = 0
	}
	if cursor < start {
		start = cursor
	}
	if cursor >= start+slots {
		start = cursor - slots + 1
	}
	maxStart := n - slots
	if maxStart < 0 {
		maxStart = 0
	}
	if start > maxStart {
		start = maxStart
	}
	if cursor < start {
		start = cursor
	}
	return start
}

func fitLines(lines []string, height, scroll int) []string {
	if height <= 0 || len(lines) == 0 {
		return nil
	}
	if scroll < 0 {
		scroll = 0
	}
	end := len(lines) - scroll
	if end < 0 {
		end = 0
	}
	if end > len(lines) {
		end = len(lines)
	}
	start := end - height
	if start < 0 {
		start = 0
	}
	return lines[start:end]
}

type threadLine struct {
	Text    string
	Msg     int
	Mine    bool
	Time    bool
	Quote   bool
	Media   bool
	Image   bool
	Reply   bool
	Day     bool
	Deleted bool
	React   bool
	MineReact bool
	Receipt string
}

func threadBody(msgs []store.Message, name string, width int, group bool) []threadLine {
	if width < 8 {
		width = 8
	}
	var out []threadLine
	for i, m := range msgs {
		if i == 0 || !sameLocalDay(msgs[i-1].At, m.At) {
			out = append(out, threadLine{Text: dayLabel(m.At), Day: true, Msg: -1})
		}
		reply := !m.Deleted && (m.ReplyTo != "" || m.ReplyText != "")
		if reply && m.ReplyText != "" {
			out = append(out, threadLine{
				Text: "│ " + quoteWho(msgs, m, name) + ": " + oneLine(m.ReplyText),
				Msg:  i, Mine: m.FromMe, Quote: true,
			})
		}
		stamp := formatTime(m.At)
		receipt := ""
		if group {
			who := selfName
			if !m.FromMe {
				who = m.SenderName
				if who == "" {
					who = "someone"
				}
			}
			stamp = who + "  " + stamp
		} else if m.FromMe {
			receipt = m.Receipt
		}
		if m.Edited {
			stamp += "  edited"
		}
		if showClock(msgs, i, receipt) || m.Edited {
			out = append(out, threadLine{Text: stamp, Msg: i, Mine: m.FromMe, Time: true, Receipt: receipt})
		}
		if m.Deleted {
			out = append(out, threadLine{Text: "deleted", Msg: i, Mine: m.FromMe, Deleted: true})
			out = append(out, threadLine{Msg: -1})
			continue
		}
		parts := wrapWords(m.Display(), width)
		if len(parts) == 0 {
			parts = []string{""}
		}
		labelN := len(parts)
		if m.Kind != "" && strings.TrimSpace(m.Caption) != "" {
			label, _, _ := strings.Cut(m.Display(), "\n")
			labelN = len(wrapWords(label, width))
		}
		who := messageWho(m, name, group)
		for n, part := range parts {
			media := (m.Kind != "" && n < labelN) || mediaLabelLine(part)
			text := part
			if media && n == 0 {
				text = who + " · " + part
			}
			out = append(out, threadLine{Text: text, Msg: i, Mine: m.FromMe, Media: media, Reply: reply})
		}
		if m.Kind == "image" && m.FilePath != "" && inlineImages() {
			out = append(out, threadLine{Text: kittyImage(m.FilePath), Msg: i, Image: true})
		}
		if react, mine := reactionLine(m.Reactions); react != "" {
			out = append(out, threadLine{Text: react, Msg: i, Mine: m.FromMe, React: true, MineReact: mine})
		}
		out = append(out, threadLine{Msg: -1})
	}
	return out
}

// reactionLine aggregates reactions as "👍 2  ❤️". mine is true when we reacted with any shown emoji.
func reactionLine(rs []store.Reaction) (string, bool) {
	if len(rs) == 0 {
		return "", false
	}
	type agg struct {
		emoji string
		n     int
		mine  bool
	}
	order := make([]string, 0, len(rs))
	by := map[string]*agg{}
	mineAny := false
	for _, r := range rs {
		a, ok := by[r.Emoji]
		if !ok {
			a = &agg{emoji: r.Emoji}
			by[r.Emoji] = a
			order = append(order, r.Emoji)
		}
		a.n++
		if r.FromMe {
			a.mine = true
			mineAny = true
		}
	}
	parts := make([]string, 0, len(order))
	for _, e := range order {
		a := by[e]
		if a.n > 1 {
			parts = append(parts, fmt.Sprintf("%s %d", a.emoji, a.n))
		} else {
			parts = append(parts, a.emoji)
		}
	}
	return strings.Join(parts, "  "), mineAny
}

// mediaLabelLine reports whether a body line is a file label rather than a caption or sentence.
const selfName = "5514"

func inlineImages() bool {
	if os.Getenv("KITTY_WINDOW_ID") != "" || os.Getenv("GHOSTTY_RESOURCES_DIR") != "" {
		return true
	}
	term := os.Getenv("TERM")
	return term == "xterm-kitty" || strings.Contains(term, "ghostty")
}

func kittyImage(path string) string {
	payload := base64.StdEncoding.EncodeToString([]byte(path))
	return "\x1b_Ga=T,t=f;" + payload + "\x1b\\"
}

func messageWho(m store.Message, name string, group bool) string {
	if m.FromMe {
		return selfName
	}
	if m.SenderName != "" {
		return m.SenderName
	}
	if !group && name != "" {
		return name
	}
	if group {
		return "someone"
	}
	return "them"
}

func mediaLabelLine(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return false
	}
	for _, b := range []string{"[image]", "[video]", "[file]", "[voice message]", "[audio]", "[sticker]"} {
		if s == b || strings.HasPrefix(s, b+" ") {
			return true
		}
	}
	for _, h := range []string{"image", "video", "audio", "file", "sticker"} {
		if s == h || strings.HasPrefix(s, h+" ·") || strings.HasPrefix(s, h+"·") {
			return true
		}
	}
	return s == "voice message" || strings.HasPrefix(s, "voice message ") || strings.HasPrefix(s, "voice message·") || strings.HasPrefix(s, "view once")
}

func quoteWho(msgs []store.Message, m store.Message, name string) string {
	for _, q := range msgs {
		if m.ReplyTo == "" || q.ID != m.ReplyTo {
			continue
		}
		if q.FromMe {
			return selfName
		}
		if q.SenderName != "" {
			return q.SenderName
		}
		break
	}
	if name != "" {
		return name
	}
	return "them"
}

func fitThread(lines []threadLine, height, scroll int) []threadLine {
	if height <= 0 || len(lines) == 0 {
		return nil
	}
	if scroll < 0 {
		scroll = 0
	}
	end := len(lines) - scroll
	if end < 0 {
		end = 0
	}
	if end > len(lines) {
		end = len(lines)
	}
	start := end - height
	if start < 0 {
		start = 0
	}
	return lines[start:end]
}

func listWidthFor(term int) int {
	if term < 20 {
		w := term / 2
		if w < 8 {
			return 8
		}
		return w
	}
	w := term / 3
	if w < 22 {
		w = 22
	}
	if w > 40 {
		w = 40
	}
	if term-w-3 < 16 {
		w = term - 3 - 16
	}
	if w < 12 {
		w = 12
	}
	return w
}

func rowPlain(name, right string, width int) string {
	right = strings.TrimSpace(right)
	rw := cellWidth(right)
	nameW := width - rw
	if rw > 0 {
		nameW--
	}
	if nameW < 1 {
		nameW = 1
	}
	name = trunc(name, nameW)
	gap := width - cellWidth(name) - rw
	if gap < 0 {
		gap = 0
	}
	return name + strings.Repeat(" ", gap) + right
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(s, "\n", " ")), " ")
}

func wrapWords(s string, width int) []string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	if width < 1 {
		width = 1
	}
	var lines []string
	for _, para := range strings.Split(s, "\n") {
		lines = append(lines, wrapPara(para, width)...)
	}
	if len(lines) == 0 {
		return []string{""}
	}
	return lines
}

func wrapPara(para string, width int) []string {
	words := strings.Fields(para)
	if len(words) == 0 {
		return []string{""}
	}
	var lines []string
	var cur string
	flush := func() {
		lines = append(lines, cur)
		cur = ""
	}
	for _, word := range words {
		ww := cellWidth(word)
		if ww > width {
			if cur != "" {
				flush()
			}
			for cellWidth(word) > width {
				piece := ansi.Truncate(word, width, "")
				if piece == "" {
					break
				}
				lines = append(lines, piece)
				word = strings.TrimPrefix(word, piece)
			}
			cur = word
			continue
		}
		if cur == "" {
			cur = word
			continue
		}
		if cellWidth(cur)+1+ww > width {
			flush()
			cur = word
			continue
		}
		cur += " " + word
	}
	if cur != "" {
		flush()
	}
	return lines
}

func cellWidth(s string) int {
	return ansi.StringWidth(s)
}

func trunc(s string, n int) string {
	if n < 1 {
		return ""
	}
	return ansi.Truncate(s, n, "…")
}

func pad(s string, n int) string {
	if cellWidth(s) > n {
		return trunc(s, n)
	}
	return s + strings.Repeat(" ", n-cellWidth(s))
}

func padLeft(s string, n int) string {
	if cellWidth(s) >= n {
		return trunc(s, n)
	}
	return strings.Repeat(" ", n-cellWidth(s)) + s
}

func showClock(msgs []store.Message, i int, receipt string) bool {
	if i == 0 || receipt != "" {
		return true
	}
	prev := msgs[i-1]
	m := msgs[i]
	if !sameSender(prev, m) || !sameLocalDay(prev.At, m.At) {
		return true
	}
	gap := m.At.Sub(prev.At)
	return gap < 0 || gap >= 5*time.Minute
}

func sameSender(a, b store.Message) bool {
	if a.FromMe || b.FromMe {
		return a.FromMe && b.FromMe
	}
	if a.Sender != "" || b.Sender != "" {
		return a.Sender == b.Sender
	}
	return a.SenderName == b.SenderName
}

func sameLocalDay(a, b time.Time) bool {
	al, bl := a.Local(), b.Local()
	return al.Year() == bl.Year() && al.YearDay() == bl.YearDay()
}

func dayLabel(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	local := t.Local()
	now := time.Now()
	if local.Year() == now.Year() && local.YearDay() == now.YearDay() {
		return "today"
	}
	yest := now.AddDate(0, 0, -1)
	if local.Year() == yest.Year() && local.YearDay() == yest.YearDay() {
		return "yesterday"
	}
	if local.Year() == now.Year() {
		return local.Format("2 Jan")
	}
	return local.Format("2 Jan 2006")
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	local := t.Local()
	now := time.Now()
	if local.Year() == now.Year() && local.YearDay() == now.YearDay() {
		return local.Format("15:04")
	}
	if local.Year() == now.Year() {
		return local.Format("Jan 2")
	}
	return local.Format("2006")
}

func qrBlocks(code string) string {
	if code == "" {
		return ""
	}
	var buf bytes.Buffer
	qrterminal.GenerateHalfBlock(code, qrterminal.L, &buf)
	return buf.String()
}
