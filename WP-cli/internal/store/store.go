package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"wp/internal/dbfile"
	"wp/internal/msgtext"

	_ "modernc.org/sqlite"
)

type Chat struct {
	JID      string    `json:"jid"`
	AltJID   string    `json:"altJid,omitempty"`
	Name     string    `json:"name,omitempty"`
	PushName string    `json:"pushName,omitempty"`
	Phone    string    `json:"phone,omitempty"`
	LastText string    `json:"lastText,omitempty"`
	LastAt   time.Time `json:"lastAt,omitzero"`
	Unread   int       `json:"unread,omitempty"`
}

func (c Chat) Label() string {
	if c.Name != "" {
		return c.Name
	}
	if c.PushName != "" {
		return c.PushName
	}
	if c.Phone != "" {
		return "+" + c.Phone
	}
	if i := strings.IndexByte(c.JID, '@'); i > 0 {
		return c.JID[:i]
	}
	return c.JID
}

type Message struct {
	ID         string    `json:"id"`
	Chat       string    `json:"chat"`
	Sender     string    `json:"sender,omitempty"`
	FromMe     bool      `json:"fromMe"`
	Text       string    `json:"text"`
	Caption    string    `json:"caption,omitempty"`
	Kind       string    `json:"kind,omitempty"`
	Mime       string    `json:"mime,omitempty"`
	FileName   string    `json:"fileName,omitempty"`
	FileSize   int64     `json:"fileSize,omitempty"`
	FilePath   string    `json:"filePath,omitempty"`
	MediaState string    `json:"mediaState,omitempty"`
	MediaRef   string    `json:"-"`
	SenderName string    `json:"senderName,omitempty"`
	ReplyTo    string    `json:"replyTo,omitempty"`
	ReplyText  string    `json:"replyText,omitempty"`
	Receipt    string    `json:"receipt,omitempty"`
	Deleted    bool      `json:"deleted,omitempty"`
	Edited     bool      `json:"edited,omitempty"`
	At         time.Time `json:"at"`
}

// ListPreview is the chat-list line. A group message is prefixed with its sender.
func (m Message) ListPreview() string {
	body := m.Preview()
	if m.SenderName != "" && !m.FromMe && strings.HasSuffix(m.Chat, "@g.us") {
		return m.SenderName + ": " + body
	}
	return body
}

// Preview is the chat-list line: a caption when there is one, otherwise the text.
func (m Message) Preview() string {
	if m.Deleted {
		return "deleted"
	}
	if strings.TrimSpace(m.Caption) != "" {
		return m.Caption
	}
	return m.Text
}

// Display is the thread body. A caption follows the file label.
func (m Message) Display() string {
	if m.Deleted {
		return "deleted"
	}
	if m.Kind == "" || strings.TrimSpace(m.Caption) == "" {
		return m.Text
	}
	return m.Text + "\n" + m.Caption
}

type Incoming struct {
	Message
	AltJID   string
	Name     string
	PushName string
	Phone    string
	Unread   bool
	Edit     bool
	Keep     bool
}

type Hit struct {
	Message
	ChatName string `json:"chatName"`
}

type Store struct {
	mu sync.Mutex
	db *sql.DB
}

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", dbfile.DSN(path))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) migrate() error {
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS chats (
			jid TEXT PRIMARY KEY,
			alt_jid TEXT NOT NULL DEFAULT '',
			name TEXT NOT NULL DEFAULT '',
			push_name TEXT NOT NULL DEFAULT '',
			phone TEXT NOT NULL DEFAULT '',
			last_text TEXT NOT NULL DEFAULT '',
			last_at INTEGER NOT NULL DEFAULT 0,
			unread INTEGER NOT NULL DEFAULT 0
		);
		CREATE TABLE IF NOT EXISTS messages (
			id TEXT NOT NULL,
			chat TEXT NOT NULL,
			sender TEXT NOT NULL DEFAULT '',
			from_me INTEGER NOT NULL,
			text TEXT NOT NULL,
			at INTEGER NOT NULL,
			kind TEXT NOT NULL DEFAULT '',
			caption TEXT NOT NULL DEFAULT '',
			mime TEXT NOT NULL DEFAULT '',
			file_name TEXT NOT NULL DEFAULT '',
			file_size INTEGER NOT NULL DEFAULT 0,
			file_path TEXT NOT NULL DEFAULT '',
			media_state TEXT NOT NULL DEFAULT '',
			media_ref TEXT NOT NULL DEFAULT '',
			reply_to TEXT NOT NULL DEFAULT '',
			reply_text TEXT NOT NULL DEFAULT '',
			receipt TEXT NOT NULL DEFAULT '',
			sender_name TEXT NOT NULL DEFAULT '',
			deleted INTEGER NOT NULL DEFAULT 0,
			edited INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (chat, id)
		);
		CREATE INDEX IF NOT EXISTS messages_chat_at ON messages(chat, at);
	`)
	if err != nil {
		return err
	}
	return ensureMessageColumns(s.db)
}

func ensureMessageColumns(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA table_info(messages)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	have := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return err
		}
		have[name] = true
	}
	if err := rows.Err(); err != nil {
		return err
	}
	want := []struct{ name, ddl string }{
		{"kind", "TEXT NOT NULL DEFAULT ''"},
		{"caption", "TEXT NOT NULL DEFAULT ''"},
		{"mime", "TEXT NOT NULL DEFAULT ''"},
		{"file_name", "TEXT NOT NULL DEFAULT ''"},
		{"file_size", "INTEGER NOT NULL DEFAULT 0"},
		{"file_path", "TEXT NOT NULL DEFAULT ''"},
		{"media_state", "TEXT NOT NULL DEFAULT ''"},
		{"media_ref", "TEXT NOT NULL DEFAULT ''"},
		{"reply_to", "TEXT NOT NULL DEFAULT ''"},
		{"reply_text", "TEXT NOT NULL DEFAULT ''"},
		{"receipt", "TEXT NOT NULL DEFAULT ''"},
		{"sender_name", "TEXT NOT NULL DEFAULT ''"},
		{"deleted", "INTEGER NOT NULL DEFAULT 0"},
		{"edited", "INTEGER NOT NULL DEFAULT 0"},
	}
	for _, col := range want {
		if have[col.name] {
			continue
		}
		if _, err := db.Exec(`ALTER TABLE messages ADD COLUMN ` + col.name + ` ` + col.ddl); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) Count() (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM chats WHERE last_at > 0`).Scan(&n)
	return n, err
}

func (s *Store) Chats(limit int) ([]Chat, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.chats(limit, true)
}

func (s *Store) Chat(jid string) (Chat, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.chat(jid)
}

func (s *Store) Messages(jid string, limit int) ([]Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.Query(`
		SELECT `+messageCols+`
		FROM messages WHERE chat = ? ORDER BY at DESC, rowid DESC LIMIT ?`, jid, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Message
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

// MessagesBefore returns up to limit messages strictly older than beforeID, oldest first.
func (s *Store) MessagesBefore(jid, beforeID string, limit int) ([]Message, error) {
	if limit <= 0 {
		limit = 50
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var at, rowid int64
	err := s.db.QueryRow(`SELECT at, rowid FROM messages WHERE chat = ? AND id = ?`, jid, beforeID).Scan(&at, &rowid)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	rows, err := s.db.Query(`
		SELECT `+messageCols+`
		FROM messages
		WHERE chat = ? AND (at < ? OR (at = ? AND rowid < ?))
		ORDER BY at DESC, rowid DESC
		LIMIT ?`, jid, at, at, rowid, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Message
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

// FindInChat returns the newest message in jid whose text or caption contains query.
func (s *Store) FindInChat(jid, query string) (Message, bool, error) {
	query = strings.TrimSpace(query)
	if jid == "" || query == "" {
		return Message{}, false, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	row := s.db.QueryRow(`
		SELECT `+messageCols+`
		FROM messages
		WHERE chat = ? AND deleted = 0 AND (text LIKE ? ESCAPE '\' OR caption LIKE ? ESCAPE '\')
		ORDER BY at DESC, rowid DESC
		LIMIT 1`, jid, likeContains(query), likeContains(query))
	m, err := scanMessage(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Message{}, false, nil
	}
	if err != nil {
		return Message{}, false, err
	}
	return m, true, nil
}

// MessagesSpan returns messages from fromID through untilID, oldest first, capped at limit.
// An empty untilID reads forward from fromID only.
func (s *Store) MessagesSpan(jid, fromID, untilID string, limit int) ([]Message, error) {
	if limit <= 0 {
		limit = 300
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	fromAt, fromRow, err := messagePos(s.db, jid, fromID)
	if err != nil {
		return nil, err
	}
	q := `SELECT ` + messageCols + ` FROM messages WHERE chat = ? AND (at > ? OR (at = ? AND rowid >= ?))`
	args := []any{jid, fromAt, fromAt, fromRow}
	if untilID != "" && untilID != fromID {
		untilAt, untilRow, err := messagePos(s.db, jid, untilID)
		if err != nil {
			return nil, err
		}
		q += ` AND (at < ? OR (at = ? AND rowid <= ?))`
		args = append(args, untilAt, untilAt, untilRow)
	}
	q += ` ORDER BY at ASC, rowid ASC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Message
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func messagePos(db *sql.DB, jid, id string) (int64, int64, error) {
	var at, rowid int64
	err := db.QueryRow(`SELECT at, rowid FROM messages WHERE chat = ? AND id = ?`, jid, id).Scan(&at, &rowid)
	return at, rowid, err
}

func (s *Store) MarkRead(jid string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`UPDATE chats SET unread = 0 WHERE jid = ?`, jid)
	return err
}

func (s *Store) Remember(jid, alt, name, push, phone string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if jid == "" {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	target, err := s.fold(tx, jid, alt)
	if err != nil {
		return err
	}
	other := alt
	if target != jid {
		other = jid
	}
	if err := s.remember(tx, target, other, name, push, phone); err != nil {
		return err
	}
	return tx.Commit()
}

// SeenIfEmpty sets the chat time when the row has never had one, so a named chat is listed.
func (s *Store) SeenIfEmpty(jid string, at time.Time) error {
	if jid == "" || at.IsZero() {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`UPDATE chats SET last_at = ? WHERE jid = ? AND last_at = 0`, at.UnixMilli(), jid)
	return err
}

// RenameSender sets the display name on group messages from those senders and refreshes previews.
func (s *Store) RenameSender(ids []string, name string) error {
	name = strings.TrimSpace(name)
	var clean []string
	for _, id := range ids {
		if id != "" {
			clean = append(clean, id)
		}
	}
	if name == "" || len(clean) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	marks := strings.TrimRight(strings.Repeat("?,", len(clean)), ",")
	args := make([]any, 0, len(clean)+2)
	args = append(args, name)
	for _, id := range clean {
		args = append(args, id)
	}
	args = append(args, name)
	if _, err := tx.Exec(`UPDATE messages SET sender_name = ? WHERE from_me = 0 AND sender IN (`+marks+`) AND sender_name != ?`, args...); err != nil {
		return err
	}
	rows, err := tx.Query(`SELECT DISTINCT chat FROM messages WHERE from_me = 0 AND sender IN (`+marks+`)`, args[1:len(clean)+1]...)
	if err != nil {
		return err
	}
	var chats []string
	for rows.Next() {
		var chat string
		if err := rows.Scan(&chat); err != nil {
			rows.Close()
			return err
		}
		chats = append(chats, chat)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, chat := range chats {
		if err := s.refreshTail(tx, chat); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) Find(query string, limit int) ([]Hit, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("empty search")
	}
	if limit <= 0 {
		limit = 50
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(`
		SELECT m.id, m.chat, m.sender, m.from_me, m.text, m.at,
		       m.kind, m.caption, m.mime, m.file_name, m.file_size, m.file_path, m.media_state, m.media_ref,
		       c.name, c.push_name, c.phone, c.jid
		FROM messages m
		JOIN chats c ON c.jid = m.chat
		WHERE m.deleted = 0 AND (m.text LIKE ? ESCAPE '\' OR m.caption LIKE ? ESCAPE '\')
		ORDER BY m.at DESC
		LIMIT ?`, likeContains(query), likeContains(query), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Hit
	for rows.Next() {
		var h Hit
		var fromMe int
		var at int64
		var name, push, phone, jid string
		if err := rows.Scan(
			&h.ID, &h.Chat, &h.Sender, &fromMe, &h.Text, &at,
			&h.Kind, &h.Caption, &h.Mime, &h.FileName, &h.FileSize, &h.FilePath, &h.MediaState, &h.MediaRef,
			&name, &push, &phone, &jid,
		); err != nil {
			return nil, err
		}
		h.FromMe = fromMe != 0
		h.At = time.UnixMilli(at)
		h.ChatName = Chat{JID: jid, Name: name, PushName: push, Phone: phone}.Label()
		out = append(out, h)
	}
	return out, rows.Err()
}

func (s *Store) DeleteMessage(chat, id string) (Chat, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return Chat{}, false, err
	}
	defer tx.Rollback()
	jid, err := s.lookup(tx, chat)
	if err != nil {
		return Chat{}, false, err
	}
	res, err := tx.Exec(`DELETE FROM messages WHERE chat = ? AND id = ?`, jid, id)
	if err != nil {
		return Chat{}, false, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return Chat{}, false, tx.Commit()
	}
	if err := s.refreshTail(tx, jid); err != nil {
		return Chat{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return Chat{}, false, err
	}
	saved, _, err := s.chat(jid)
	return saved, true, err
}

// MarkDeleted keeps the message and hides its contents. The chat preview follows the newest row.
func (s *Store) MarkDeleted(chat, id string) (Chat, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return Chat{}, false, err
	}
	defer tx.Rollback()
	jid, err := s.lookup(tx, chat)
	if err != nil {
		return Chat{}, false, err
	}
	res, err := tx.Exec(`
		UPDATE messages
		SET deleted = 1, text = 'deleted', caption = '', kind = '', file_path = '', media_state = ''
		WHERE chat = ? AND id = ?`, jid, id)
	if err != nil {
		return Chat{}, false, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return Chat{}, false, tx.Commit()
	}
	if err := s.refreshTail(tx, jid); err != nil {
		return Chat{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return Chat{}, false, err
	}
	saved, _, err := s.chat(jid)
	return saved, true, err
}

func (s *Store) ClearChat(jid string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	id, err := s.lookup(tx, jid)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM messages WHERE chat = ?`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE chats SET last_text = '', last_at = 0, unread = 0 WHERE jid = ?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) DeleteChat(jid string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	id, err := s.lookup(tx, jid)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM messages WHERE chat = ?`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM chats WHERE jid = ?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) SetUnread(jid string, n int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	id, err := s.lookup(tx, jid)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE chats SET unread = ? WHERE jid = ?`, n, id); err != nil {
		return err
	}
	return tx.Commit()
}

// SaveLive upserts one message. The bool is true when the text is new or changed.
func (s *Store) SaveLive(in Incoming) (Chat, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if in.Chat == "" || in.ID == "" || in.Text == "" {
		return Chat{}, false, fmt.Errorf("incomplete message")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return Chat{}, false, err
	}
	defer tx.Rollback()

	s.fillFromAlt(tx, in.Chat, in.AltJID, &in.Name, &in.PushName, &in.Phone)
	target, err := s.fold(tx, in.Chat, in.AltJID)
	if err != nil {
		return Chat{}, false, err
	}
	other := in.AltJID
	if target != in.Chat {
		other = in.Chat
	}
	in.Chat = target
	in.AltJID = other

	var old string
	var oldAt int64
	var oldDeleted int
	err = tx.QueryRow(`SELECT text, at, deleted FROM messages WHERE chat = ? AND id = ?`, in.Chat, in.ID).Scan(&old, &oldAt, &oldDeleted)
	exists := err == nil
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return Chat{}, false, err
	}
	if exists && in.Keep {
		if err := tx.Commit(); err != nil {
			return Chat{}, false, err
		}
		chat, _, err := s.chat(in.Chat)
		return chat, false, err
	}
	if exists && in.Edit {
		changed := false
		if oldDeleted == 0 {
			if _, err := tx.Exec(`UPDATE messages SET text = ?, edited = 1 WHERE chat = ? AND id = ?`, in.Text, in.Chat, in.ID); err != nil {
				return Chat{}, false, err
			}
			if _, err := tx.Exec(`UPDATE chats SET last_text = ? WHERE jid = ? AND last_at = ?`, in.ListPreview(), in.Chat, oldAt); err != nil {
				return Chat{}, false, err
			}
			changed = old != in.Text
		}
		if err := tx.Commit(); err != nil {
			return Chat{}, false, err
		}
		chat, _, err := s.chat(in.Chat)
		return chat, changed, err
	}
	if err := insertMessage(tx, in.Message); err != nil {
		return Chat{}, false, err
	}
	unread := 0
	if !exists && in.Unread {
		unread = 1
	}
	if err := upsertChat(tx, in.Chat, in.AltJID, in.Name, in.PushName, in.Phone, in.ListPreview(), in.At.UnixMilli(), unread, false); err != nil {
		return Chat{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return Chat{}, false, err
	}
	chat, _, err := s.chat(in.Chat)
	return chat, !exists || old != in.Text, err
}

// SaveHistory stores a synced conversation. Unread is the phone's absolute count.
func (s *Store) SaveHistory(jid, alt, name, push, phone string, unread int, lastAt time.Time, lastText string, msgs []Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	s.fillFromAlt(tx, jid, alt, &name, &push, &phone)
	target, err := s.fold(tx, jid, alt)
	if err != nil {
		return err
	}
	if target != jid {
		alt = jid
		jid = target
	}
	syncAt := millis(lastAt)
	var localLast int64
	var localUnread int
	err = tx.QueryRow(`SELECT last_at, unread FROM chats WHERE jid = ?`, jid).Scan(&localLast, &localUnread)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err == nil && localLast > syncAt {
		unread = localUnread
	}
	for _, m := range msgs {
		if m.Text == "" || m.ID == "" {
			continue
		}
		m.Chat = jid
		if err := insertMessage(tx, m); err != nil {
			return err
		}
		if m.At.After(lastAt) {
			lastAt = m.At
			lastText = m.ListPreview()
		}
	}
	if strings.TrimSpace(lastText) == "" {
		var text, caption, sender string
		var fromMe int
		err := tx.QueryRow(`SELECT text, caption, sender_name, from_me FROM messages WHERE chat = ? ORDER BY at DESC, rowid DESC LIMIT 1`, jid).Scan(&text, &caption, &sender, &fromMe)
		if err == nil {
			lastText = (Message{Chat: jid, Text: text, Caption: caption, SenderName: sender, FromMe: fromMe != 0}).ListPreview()
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
	}
	if err := upsertChat(tx, jid, alt, name, push, phone, lastText, millis(lastAt), unread, true); err != nil {
		return err
	}
	return tx.Commit()
}

// Resolve finds chats for a name, phone number, or WhatsApp id.
func (s *Store) Resolve(query string) ([]Chat, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("empty chat")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	all, err := s.chats(0, false)
	if err != nil {
		return nil, err
	}
	if strings.Contains(query, "@") {
		var exact []Chat
		for _, c := range all {
			if c.JID == query || c.AltJID == query {
				exact = append(exact, c)
			}
		}
		return exact, nil
	}
	if digits, ok := phoneQuery(query); ok {
		var exact []Chat
		for _, c := range all {
			if c.Phone == digits || userOf(c.JID) == digits || userOf(c.AltJID) == digits {
				exact = append(exact, c)
			}
		}
		return exact, nil
	}
	q := strings.ToLower(query)
	var exact, partial []Chat
	for _, c := range all {
		label := strings.ToLower(c.Label())
		name := strings.ToLower(c.Name)
		push := strings.ToLower(c.PushName)
		if label == q || name == q || push == q {
			exact = append(exact, c)
			continue
		}
		if strings.Contains(label, q) || strings.Contains(name, q) || strings.Contains(push, q) {
			partial = append(partial, c)
		}
	}
	if len(exact) > 0 {
		return preferActive(exact), nil
	}
	return preferActive(partial), nil
}

func preferActive(in []Chat) []Chat {
	var active []Chat
	for _, c := range in {
		if !c.LastAt.IsZero() {
			active = append(active, c)
		}
	}
	if len(active) > 0 {
		return active
	}
	return in
}

func (s *Store) chats(limit int, messagesOnly bool) ([]Chat, error) {
	q := `SELECT jid, alt_jid, name, push_name, phone, last_text, last_at, unread FROM chats`
	var args []any
	if messagesOnly {
		q += ` WHERE last_at > 0`
	}
	q += ` ORDER BY last_at DESC`
	if limit > 0 {
		q += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Chat
	for rows.Next() {
		c, err := scanChat(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) chat(jid string) (Chat, bool, error) {
	row := s.db.QueryRow(`
		SELECT jid, alt_jid, name, push_name, phone, last_text, last_at, unread
		FROM chats WHERE jid = ?`, jid)
	c, err := scanChat(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Chat{}, false, nil
	}
	if err != nil {
		return Chat{}, false, err
	}
	return c, true, nil
}

func (s *Store) fold(tx *sql.Tx, a, b string) (string, error) {
	if b == "" || a == b {
		return a, nil
	}
	ar, aok, err := chatTx(tx, a)
	if err != nil {
		return "", err
	}
	br, bok, err := chatTx(tx, b)
	if err != nil {
		return "", err
	}
	keep := preferJID(a, b, millis(ar.LastAt), millis(br.LastAt))
	drop := b
	if keep == b {
		drop = a
	}
	dropExists := (drop == a && aok) || (drop == b && bok)
	if !dropExists {
		return keep, nil
	}
	keepExists := (keep == a && aok) || (keep == b && bok)
	if !keepExists {
		if _, err := tx.Exec(`UPDATE messages SET chat = ? WHERE chat = ?`, keep, drop); err != nil {
			return "", err
		}
		if _, err := tx.Exec(`UPDATE chats SET jid = ?, alt_jid = ? WHERE jid = ?`, keep, drop, drop); err != nil {
			return "", err
		}
		return keep, nil
	}
	if _, err := tx.Exec(`
		DELETE FROM messages WHERE chat = ? AND id IN (SELECT id FROM messages WHERE chat = ?)`, drop, keep); err != nil {
		return "", err
	}
	if _, err := tx.Exec(`UPDATE messages SET chat = ? WHERE chat = ?`, keep, drop); err != nil {
		return "", err
	}
	dropRow := br
	if drop == a {
		dropRow = ar
	}
	if _, err := tx.Exec(`
		UPDATE chats SET
			alt_jid = ?,
			name = CASE WHEN name = '' THEN ? ELSE name END,
			push_name = CASE WHEN push_name = '' THEN ? ELSE push_name END,
			phone = CASE WHEN phone = '' THEN ? ELSE phone END,
			unread = MAX(unread, ?)
		WHERE jid = ?`, drop, dropRow.Name, dropRow.PushName, dropRow.Phone, dropRow.Unread, keep); err != nil {
		return "", err
	}
	if _, err := tx.Exec(`DELETE FROM chats WHERE jid = ?`, drop); err != nil {
		return "", err
	}
	if err := s.refreshTail(tx, keep); err != nil {
		return "", err
	}
	return keep, nil
}

func (s *Store) refreshTail(tx *sql.Tx, jid string) error {
	var text, caption, sender string
	var fromMe int
	var at int64
	var deleted int
	err := tx.QueryRow(`SELECT text, caption, sender_name, from_me, deleted, at FROM messages WHERE chat = ? ORDER BY at DESC, rowid DESC LIMIT 1`, jid).Scan(&text, &caption, &sender, &fromMe, &deleted, &at)
	if errors.Is(err, sql.ErrNoRows) {
		_, err = tx.Exec(`UPDATE chats SET last_text = '', last_at = 0 WHERE jid = ?`, jid)
		return err
	}
	if err != nil {
		return err
	}
	preview := (Message{Chat: jid, Text: text, Caption: caption, SenderName: sender, FromMe: fromMe != 0, Deleted: deleted != 0}).ListPreview()
	_, err = tx.Exec(`UPDATE chats SET last_text = ?, last_at = ? WHERE jid = ?`, preview, at, jid)
	return err
}

func (s *Store) lookup(tx *sql.Tx, jid string) (string, error) {
	var found string
	err := tx.QueryRow(`SELECT jid FROM chats WHERE jid = ? OR alt_jid = ? LIMIT 1`, jid, jid).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return jid, nil
	}
	return found, err
}

func (s *Store) remember(tx *sql.Tx, jid, alt, name, push, phone string) error {
	if jid == "" {
		return nil
	}
	_, err := tx.Exec(`
		INSERT INTO chats (jid, alt_jid, name, push_name, phone)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(jid) DO UPDATE SET
			alt_jid = CASE WHEN excluded.alt_jid != '' THEN excluded.alt_jid ELSE chats.alt_jid END,
			name = CASE WHEN excluded.name != '' THEN excluded.name ELSE chats.name END,
			push_name = CASE WHEN excluded.push_name != '' THEN excluded.push_name ELSE chats.push_name END,
			phone = CASE WHEN excluded.phone != '' THEN excluded.phone ELSE chats.phone END`,
		jid, alt, name, push, phone)
	return err
}

func (s *Store) fillFromAlt(tx *sql.Tx, jid, alt string, name, push, phone *string) {
	if alt == "" {
		return
	}
	var n, p, ph string
	err := tx.QueryRow(`
		SELECT name, push_name, phone FROM chats WHERE jid = ? AND jid != ?`, alt, jid).Scan(&n, &p, &ph)
	if err != nil {
		return
	}
	if *name == "" {
		*name = n
	}
	if *push == "" {
		*push = p
	}
	if *phone == "" {
		*phone = ph
	}
}

func upsertChat(tx *sql.Tx, jid, alt, name, push, phone, lastText string, lastAt int64, unread int, absoluteUnread bool) error {
	if absoluteUnread {
		_, err := tx.Exec(`
			INSERT INTO chats (jid, alt_jid, name, push_name, phone, last_text, last_at, unread)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(jid) DO UPDATE SET
				alt_jid = CASE WHEN excluded.alt_jid != '' THEN excluded.alt_jid ELSE chats.alt_jid END,
				name = CASE WHEN excluded.name != '' THEN excluded.name ELSE chats.name END,
				push_name = CASE WHEN excluded.push_name != '' THEN excluded.push_name ELSE chats.push_name END,
				phone = CASE WHEN excluded.phone != '' THEN excluded.phone ELSE chats.phone END,
				last_text = CASE WHEN excluded.last_at >= chats.last_at THEN excluded.last_text ELSE chats.last_text END,
				last_at = CASE WHEN excluded.last_at >= chats.last_at THEN excluded.last_at ELSE chats.last_at END,
				unread = excluded.unread`,
			jid, alt, name, push, phone, lastText, lastAt, unread)
		return err
	}
	_, err := tx.Exec(`
		INSERT INTO chats (jid, alt_jid, name, push_name, phone, last_text, last_at, unread)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(jid) DO UPDATE SET
			alt_jid = CASE WHEN excluded.alt_jid != '' THEN excluded.alt_jid ELSE chats.alt_jid END,
			name = CASE WHEN excluded.name != '' THEN excluded.name ELSE chats.name END,
			push_name = CASE WHEN excluded.push_name != '' THEN excluded.push_name ELSE chats.push_name END,
			phone = CASE WHEN excluded.phone != '' THEN excluded.phone ELSE chats.phone END,
			last_text = CASE WHEN excluded.last_at >= chats.last_at THEN excluded.last_text ELSE chats.last_text END,
			last_at = CASE WHEN excluded.last_at >= chats.last_at THEN excluded.last_at ELSE chats.last_at END,
			unread = chats.unread + excluded.unread`,
		jid, alt, name, push, phone, lastText, lastAt, unread)
	return err
}

type scanner interface {
	Scan(dest ...any) error
}

func scanChat(row scanner) (Chat, error) {
	var c Chat
	var last int64
	if err := row.Scan(&c.JID, &c.AltJID, &c.Name, &c.PushName, &c.Phone, &c.LastText, &last, &c.Unread); err != nil {
		return Chat{}, err
	}
	if last > 0 {
		c.LastAt = time.UnixMilli(last)
	}
	return c, nil
}

const messageCols = `id, chat, sender, from_me, text, at, kind, caption, mime, file_name, file_size, file_path, media_state, media_ref, reply_to, reply_text, receipt, sender_name, deleted, edited`

func scanMessage(row scanner) (Message, error) {
	var m Message
	var fromMe, deleted, edited int
	var at int64
	if err := row.Scan(
		&m.ID, &m.Chat, &m.Sender, &fromMe, &m.Text, &at,
		&m.Kind, &m.Caption, &m.Mime, &m.FileName, &m.FileSize, &m.FilePath, &m.MediaState, &m.MediaRef,
		&m.ReplyTo, &m.ReplyText, &m.Receipt, &m.SenderName, &deleted, &edited,
	); err != nil {
		return Message{}, err
	}
	m.FromMe = fromMe != 0
	m.Deleted = deleted != 0
	m.Edited = edited != 0
	m.At = time.UnixMilli(at)
	return m, nil
}

func insertMessage(tx *sql.Tx, m Message) error {
	_, err := tx.Exec(`
		INSERT INTO messages (
			id, chat, sender, from_me, text, at,
			kind, caption, mime, file_name, file_size, file_path, media_state, media_ref,
			reply_to, reply_text, receipt, sender_name, deleted, edited
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(chat, id) DO UPDATE SET
			text = CASE
				WHEN messages.deleted = 1 THEN messages.text
				WHEN messages.media_state = 'local' THEN messages.text
				ELSE excluded.text
			END,
			at = excluded.at,
			from_me = excluded.from_me,
			sender = excluded.sender,
			sender_name = CASE WHEN excluded.sender_name != '' THEN excluded.sender_name ELSE messages.sender_name END,
			kind = CASE WHEN messages.kind != '' THEN messages.kind ELSE excluded.kind END,
			caption = CASE WHEN messages.caption != '' THEN messages.caption ELSE excluded.caption END,
			mime = CASE WHEN messages.mime != '' THEN messages.mime ELSE excluded.mime END,
			file_name = CASE WHEN messages.file_name != '' THEN messages.file_name ELSE excluded.file_name END,
			file_size = CASE WHEN messages.file_size > 0 THEN messages.file_size ELSE excluded.file_size END,
			file_path = CASE WHEN messages.media_state = 'local' THEN messages.file_path ELSE excluded.file_path END,
			media_state = CASE WHEN messages.media_state = 'local' THEN messages.media_state ELSE excluded.media_state END,
			media_ref = CASE WHEN messages.media_ref != '' THEN messages.media_ref ELSE excluded.media_ref END,
			reply_to = CASE WHEN messages.reply_to != '' THEN messages.reply_to ELSE excluded.reply_to END,
			reply_text = CASE WHEN messages.reply_text != '' THEN messages.reply_text ELSE excluded.reply_text END,
			receipt = CASE
				WHEN messages.receipt = 'read' THEN messages.receipt
				WHEN messages.receipt = 'delivered' AND excluded.receipt IN ('', 'sent') THEN messages.receipt
				WHEN messages.receipt = 'sent' AND excluded.receipt = '' THEN messages.receipt
				WHEN excluded.receipt != '' THEN excluded.receipt
				ELSE messages.receipt
			END,
			deleted = CASE WHEN messages.deleted = 1 OR excluded.deleted = 1 THEN 1 ELSE 0 END,
			edited = CASE WHEN messages.edited = 1 OR excluded.edited = 1 THEN 1 ELSE 0 END`,
		m.ID, m.Chat, m.Sender, boolInt(m.FromMe), m.Text, m.At.UnixMilli(),
		m.Kind, m.Caption, m.Mime, m.FileName, m.FileSize, m.FilePath, m.MediaState, m.MediaRef,
		m.ReplyTo, m.ReplyText, m.Receipt, m.SenderName, boolInt(m.Deleted), boolInt(m.Edited))
	return err
}

// Media returns one stored message, following a merged phone-number alias.
func (s *Store) Media(chat, id string) (Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return Message{}, err
	}
	defer tx.Rollback()
	jid, err := s.lookup(tx, chat)
	if err != nil {
		return Message{}, err
	}
	row := tx.QueryRow(`SELECT `+messageCols+` FROM messages WHERE chat = ? AND id = ?`, jid, id)
	m, err := scanMessage(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Message{}, fmt.Errorf("message not found")
	}
	return m, err
}

// SetReceipt moves outgoing messages forward to delivered or read.
// A read receipt also covers every earlier outgoing message in the chat.
func (s *Store) SetReceipt(chat string, ids []string, state string, at time.Time) ([]Message, error) {
	if state != "delivered" && state != "read" {
		return nil, fmt.Errorf("unknown receipt")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	jid, err := s.lookup(tx, chat)
	if err != nil {
		return nil, err
	}
	seen := map[string]struct{}{}
	var targets []string
	add := func(id, cur string) {
		if id == "" {
			return
		}
		if _, ok := seen[id]; ok {
			return
		}
		if state == "delivered" && cur != "" && cur != "sent" {
			return
		}
		if state == "read" && cur == "read" {
			return
		}
		seen[id] = struct{}{}
		targets = append(targets, id)
	}
	if state == "read" && !at.IsZero() {
		rows, err := tx.Query(`
			SELECT id, receipt FROM messages
			WHERE chat = ? AND from_me = 1 AND at > 0 AND at <= ?`, jid, at.UnixMilli())
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id, cur string
			if err := rows.Scan(&id, &cur); err != nil {
				rows.Close()
				return nil, err
			}
			add(id, cur)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
	}
	for _, id := range ids {
		var cur string
		err := tx.QueryRow(`SELECT receipt FROM messages WHERE chat = ? AND id = ? AND from_me = 1`, jid, id).Scan(&cur)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, err
		}
		add(id, cur)
	}
	for _, id := range targets {
		if _, err := tx.Exec(`UPDATE messages SET receipt = ? WHERE chat = ? AND id = ?`, state, jid, id); err != nil {
			return nil, err
		}
	}
	out := make([]Message, 0, len(targets))
	for _, id := range targets {
		row := tx.QueryRow(`SELECT `+messageCols+` FROM messages WHERE chat = ? AND id = ?`, jid, id)
		m, err := scanMessage(row)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return out, nil
}

// SetFile records a finished or failed download and refreshes the chat preview.
func (s *Store) SetFile(chat, id, path, state string, size int64) (Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return Message{}, err
	}
	defer tx.Rollback()
	jid, err := s.lookup(tx, chat)
	if err != nil {
		return Message{}, err
	}
	row := tx.QueryRow(`SELECT `+messageCols+` FROM messages WHERE chat = ? AND id = ?`, jid, id)
	m, err := scanMessage(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Message{}, fmt.Errorf("message not found")
	}
	if err != nil {
		return Message{}, err
	}
	if size > 0 {
		m.FileSize = size
	}
	if path != "" {
		m.FilePath = path
	}
	m.MediaState = state
	if m.Kind != "" {
		m.Text = msgtext.MediaLabel(m.Kind, m.FileName, m.FileSize, state)
	}
	if _, err := tx.Exec(`
		UPDATE messages SET text = ?, file_path = ?, media_state = ?, file_size = ?
		WHERE chat = ? AND id = ?`, m.Text, m.FilePath, m.MediaState, m.FileSize, m.Chat, m.ID); err != nil {
		return Message{}, err
	}
	if _, err := tx.Exec(`
		UPDATE chats SET last_text = ? WHERE jid = ? AND last_at = ?`, m.Preview(), m.Chat, m.At.UnixMilli()); err != nil {
		return Message{}, err
	}
	if err := tx.Commit(); err != nil {
		return Message{}, err
	}
	return m, nil
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func millis(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixMilli()
}

func chatTx(tx *sql.Tx, jid string) (Chat, bool, error) {
	row := tx.QueryRow(`
		SELECT jid, alt_jid, name, push_name, phone, last_text, last_at, unread
		FROM chats WHERE jid = ?`, jid)
	c, err := scanChat(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Chat{}, false, nil
	}
	if err != nil {
		return Chat{}, false, err
	}
	return c, true, nil
}

func preferJID(a, b string, aAt, bAt int64) string {
	if b == "" || a == b {
		return a
	}
	aLID := isLID(a)
	bLID := isLID(b)
	if aLID != bLID {
		if aLID {
			return a
		}
		return b
	}
	if bAt > aAt {
		return b
	}
	return a
}

func isLID(jid string) bool {
	return strings.HasSuffix(jid, "@lid") || strings.HasSuffix(jid, "@hosted.lid")
}

func likeContains(q string) string {
	var b strings.Builder
	b.WriteByte('%')
	for _, r := range q {
		switch r {
		case '\\', '%', '_':
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	b.WriteByte('%')
	return b.String()
}

func userOf(jid string) string {
	if i := strings.IndexByte(jid, '@'); i > 0 {
		return jid[:i]
	}
	return ""
}

func phoneQuery(q string) (string, bool) {
	var b strings.Builder
	for _, r := range q {
		switch {
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '+' || r == ' ' || r == '-' || r == '(' || r == ')':
		default:
			return "", false
		}
	}
	d := b.String()
	if len(d) < 8 || len(d) > 15 {
		return "", false
	}
	return d, true
}
