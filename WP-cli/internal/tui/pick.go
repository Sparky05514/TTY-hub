package tui

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

type pickEntry struct {
	name string
	dir  bool
}

func (m *model) startPick() {
	dir, err := os.UserHomeDir()
	if err != nil || dir == "" {
		dir = "."
	}
	m.pick = true
	m.pickDir = dir
	m.pickFilter = ""
	m.pickCursor = 0
	m.pickStart = 0
}

func (m model) onPick(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	entries := readPick(m.pickDir, m.pickFilter)
	slots := m.threadHeight() - 1
	if slots < 1 {
		slots = 1
	}
	switch msg.String() {
	case "q":
		return m, m.quit()
	case "esc":
		m.pick = false
		return m, nil
	case "j", "down":
		m.pickCursor++
	case "k", "up":
		m.pickCursor--
	case "g":
		m.pickCursor = 0
	case "G":
		m.pickCursor = len(entries) - 1
	case "pgup", "ctrl+u":
		m.pickCursor -= slots
	case "pgdown", "ctrl+d":
		m.pickCursor += slots
	case "enter":
		if len(entries) == 0 || m.pickCursor < 0 || m.pickCursor >= len(entries) {
			return m, nil
		}
		ent := entries[m.pickCursor]
		if ent.dir {
			next := m.pickDir
			if ent.name == ".." {
				next = filepath.Dir(m.pickDir)
			} else {
				next = filepath.Join(m.pickDir, ent.name)
			}
			m.pickDir = next
			m.pickFilter = ""
			m.pickCursor = 0
			m.pickStart = 0
			return m, nil
		}
		return m.sendPicked(filepath.Join(m.pickDir, ent.name))
	case "backspace":
		r := []rune(m.pickFilter)
		if len(r) > 0 {
			m.pickFilter = string(r[:len(r)-1])
			m.pickCursor = 0
			m.pickStart = 0
			return m, nil
		}
		parent := filepath.Dir(m.pickDir)
		if parent != m.pickDir {
			m.pickDir = parent
			m.pickCursor = 0
			m.pickStart = 0
		}
		return m, nil
	default:
		if msg.Type == tea.KeyRunes {
			m.pickFilter += msg.String()
			m.pickCursor = 0
			m.pickStart = 0
		}
		return m, nil
	}
	if m.pickCursor < 0 {
		m.pickCursor = 0
	}
	if len(entries) == 0 {
		m.pickCursor = 0
	} else if m.pickCursor >= len(entries) {
		m.pickCursor = len(entries) - 1
	}
	m.pickStart = placeWindow(m.pickCursor, m.pickStart, slots, len(entries))
	return m, nil
}

func (m *model) sendPicked(path string) (tea.Model, tea.Cmd) {
	m.pick = false
	if m.openJID == "" {
		m.notice = "select a chat"
		return m, nil
	}
	caption := m.input.Value()
	reply := m.replyID
	m.input.SetValue("")
	m.replyID = ""
	m.replyText = ""
	delete(m.drafts, m.openJID)
	m.notice = "sending…"
	return *m, tea.Batch(sendFile(m.openJID, path, caption, reply), m.pauseTyping())
}

func (m model) pickLines(height int) []string {
	w := m.threadWidth()
	title := shortPath(m.pickDir)
	entries := readPick(m.pickDir, m.pickFilter)
	slots := height - 1
	if slots < 1 {
		slots = 1
	}
	start := placeWindow(m.pickCursor, m.pickStart, slots, len(entries))
	out := []string{styleBold.Render(pad(trunc(title, w), w))}
	if len(entries) == 0 {
		out = append(out, pad("no matches", w))
	}
	end := start + slots
	if end > len(entries) {
		end = len(entries)
	}
	for i := start; i < end && len(out) < height; i++ {
		label := entries[i].name
		if entries[i].dir {
			label += "/"
		}
		line := pad(trunc(label, w), w)
		if i == m.pickCursor {
			line = styleSelect.Render(line)
		}
		out = append(out, line)
	}
	for len(out) < height {
		out = append(out, pad("", w))
	}
	return out[:height]
}

func readPick(dir, filter string) []pickEntry {
	filter = strings.ToLower(strings.TrimSpace(filter))
	var out []pickEntry
	parent := filepath.Dir(dir)
	if parent != dir {
		out = append(out, pickEntry{name: "..", dir: true})
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		return out
	}
	var dirs, files []pickEntry
	for _, ent := range ents {
		name := ent.Name()
		if strings.HasPrefix(name, ".") && !strings.HasPrefix(filter, ".") {
			continue
		}
		if filter != "" && !strings.Contains(strings.ToLower(name), filter) && name != ".." {
			continue
		}
		item := pickEntry{name: name, dir: ent.IsDir()}
		if item.dir {
			dirs = append(dirs, item)
		} else {
			files = append(files, item)
		}
	}
	sort.Slice(dirs, func(i, j int) bool { return strings.ToLower(dirs[i].name) < strings.ToLower(dirs[j].name) })
	sort.Slice(files, func(i, j int) bool { return strings.ToLower(files[i].name) < strings.ToLower(files[j].name) })
	out = append(out, dirs...)
	out = append(out, files...)
	return out
}

func shortPath(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	if path == home {
		return "~"
	}
	prefix := home + string(os.PathSeparator)
	if strings.HasPrefix(path, prefix) {
		return "~" + string(os.PathSeparator) + strings.TrimPrefix(path, prefix)
	}
	return path
}
