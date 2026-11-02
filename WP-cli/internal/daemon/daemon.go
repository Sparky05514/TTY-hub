package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mdp/qrterminal/v3"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	"wp/internal/dbfile"
	"wp/internal/direct"
	"wp/internal/ipc"
	"wp/internal/msgtext"
	"wp/internal/paths"
	"wp/internal/store"

	_ "modernc.org/sqlite"
)

type Daemon struct {
	client  *whatsmeow.Client
	session *sqlstore.Container
	app     *store.Store
	cancel  context.CancelFunc
	ctx     context.Context
	waiting atomic.Bool
	qr      atomic.Value // string, latest pairing QR payload
	history chan *events.HistorySync

	mu         sync.Mutex
	subs       map[int]chan ipc.Live
	next       int
	mediaSem   chan struct{}
	groupWait  map[string]struct{}
	backfill   map[string]backfillWait
	backfillN  uint64
	askedEmpty map[string]struct{}
}

type backfillWait struct {
	anchor string
	token  uint64
}

func Run(ctx context.Context, phone string) error {
	dir, err := paths.Dir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return err
	}
	sock, err := paths.Sock()
	if err != nil {
		return err
	}
	if running(sock) {
		return fmt.Errorf("wp is already running")
	}
	_ = os.Remove(sock)

	app, err := store.Open(filepath.Join(dir, "messages.db"))
	if err != nil {
		return err
	}
	session, err := sqlstore.New(ctx, "sqlite", dbfile.DSN(filepath.Join(dir, "session.db")), nil)
	if err != nil {
		app.Close()
		return err
	}
	device, err := session.GetFirstDevice(ctx)
	if err != nil {
		session.Close()
		app.Close()
		return err
	}
	client := whatsmeow.NewClient(device, nil)
	runCtx, cancel := context.WithCancel(ctx)
	d := &Daemon{
		client:   client,
		session:  session,
		app:      app,
		cancel:   cancel,
		ctx:      runCtx,
		history:  make(chan *events.HistorySync),
		subs:     map[int]chan ipc.Live{},
		mediaSem: make(chan struct{}, 2),
	}
	histDone := make(chan struct{})
	go func() {
		d.historyLoop()
		close(histDone)
	}()
	client.AddEventHandler(d.onEvent)

	ln, err := net.Listen("unix", sock)
	if err != nil {
		cancel()
		<-histDone
		session.Close()
		app.Close()
		return err
	}
	if err := os.Chmod(sock, 0o600); err != nil {
		ln.Close()
		cancel()
		<-histDone
		session.Close()
		app.Close()
		return err
	}
	defer func() {
		cancel()
		<-histDone
		ln.Close()
		_ = os.Remove(sock)
		client.Disconnect()
		session.Close()
		app.Close()
	}()

	errCh := make(chan error, 1)
	go func() {
		if err := d.login(runCtx, phone); err != nil && runCtx.Err() == nil {
			errCh <- err
			cancel()
		}
	}()
	if err := d.accept(runCtx, ln); err != nil {
		return err
	}
	select {
	case err := <-errCh:
		return err
	default:
		return nil
	}
}

func (d *Daemon) login(ctx context.Context, phone string) error {
	if phone != "" {
		phone = digitsOnly(phone)
		if len(phone) < 8 || len(phone) > 15 {
			return fmt.Errorf("phone number must include the country code")
		}
	}
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if d.client.Store.ID != nil {
			d.waiting.Store(false)
			return d.keepConnected(ctx)
		}
		qrChan, err := d.client.GetQRChannel(ctx)
		if err != nil {
			return err
		}
		d.waiting.Store(true)
		if err := d.client.Connect(); err != nil {
			return err
		}
		if phone != "" {
			fmt.Fprintf(os.Stderr, "waiting for a pairing code\n")
		} else {
			fmt.Fprintf(os.Stderr, "scan this QR with WhatsApp → Linked devices → Link a device\n")
		}
		d.readQR(ctx, qrChan, phone)
		if d.client.Store.ID != nil {
			d.waiting.Store(false)
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		fmt.Fprintf(os.Stderr, "pairing timed out, retrying\n")
		d.client.Disconnect()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

func (d *Daemon) keepConnected(ctx context.Context) error {
	backoff := time.Second
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		fmt.Fprintf(os.Stderr, "connecting\n")
		err := d.client.Connect()
		if err == nil {
			<-ctx.Done()
			return ctx.Err()
		}
		fmt.Fprintf(os.Stderr, "connect: %v\n", err)
		d.client.Disconnect()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
		if backoff < 30*time.Second {
			backoff *= 2
		}
	}
}

func (d *Daemon) historyLoop() {
	for {
		select {
		case <-d.ctx.Done():
			return
		case v := <-d.history:
			d.onHistory(v)
		}
	}
}

func (d *Daemon) readQR(ctx context.Context, qr <-chan whatsmeow.QRChannelItem, phone string) {
	asked := false
	for {
		select {
		case <-ctx.Done():
			return
		case evt, ok := <-qr:
			if !ok {
				return
			}
			switch evt.Event {
			case whatsmeow.QRChannelEventCode:
				d.setQR(evt.Code)
				if phone != "" && !asked {
					asked = true
					code, err := d.client.PairPhone(ctx, phone, true, whatsmeow.PairClientChrome, "Chrome (Linux)")
					if err != nil {
						fmt.Fprintf(os.Stderr, "pairing code: %v\n", err)
						return
					}
					fmt.Fprintf(os.Stderr, "pairing code: %s\nOn the phone: Linked devices → Link with phone number\n", formatCode(code))
					continue
				}
				if phone == "" {
					qrterminal.GenerateHalfBlock(evt.Code, qrterminal.L, os.Stdout)
					fmt.Println()
				}
			default:
				if evt.Event == whatsmeow.QRChannelSuccess.Event {
					d.setQR("")
				}
				if evt.Event != "" {
					fmt.Fprintf(os.Stderr, "login: %s\n", evt.Event)
				}
				if evt.Error != nil {
					fmt.Fprintf(os.Stderr, "pairing error: %v\n", evt.Error)
				}
			}
		}
	}
}

func (d *Daemon) accept(ctx context.Context, ln net.Listener) error {
	go func() {
		<-ctx.Done()
		ln.Close()
	}()
	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		go d.serve(conn)
	}
}

func (d *Daemon) serve(conn net.Conn) {
	defer conn.Close()
	dec := json.NewDecoder(conn)
	var req ipc.Request
	if err := dec.Decode(&req); err != nil {
		return
	}
	if req.Cmd == "watch" {
		d.serveWatch(conn)
		return
	}
	resp := d.handle(req)
	enc := json.NewEncoder(conn)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(resp)
	if req.Cmd == "logout" && resp.OK {
		d.cancel()
	}
}

func (d *Daemon) serveWatch(conn net.Conn) {
	enc := json.NewEncoder(conn)
	enc.SetEscapeHTML(false)
	if !d.linked() {
		_ = enc.Encode(ipc.Response{Error: "not linked yet. Finish pairing in the wp up terminal"})
		return
	}
	ch, unsub := d.subscribe()
	defer unsub()
	if err := enc.Encode(ipc.Response{OK: true}); err != nil {
		return
	}
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(io.Discard, conn)
		close(done)
	}()
	for {
		select {
		case ev := <-ch:
			if err := enc.Encode(ev); err != nil {
				return
			}
		case <-done:
			return
		}
	}
}

func (d *Daemon) handle(req ipc.Request) ipc.Response {
	switch req.Cmd {
	case "status":
		return d.status()
	case "chats":
		return d.chats(req.Limit)
	case "read":
		return d.read(req.Query, req.Limit)
	case "older":
		return d.older(req.Query, req.Text)
	case "presence":
		return d.presence(req.Query, req.Text)
	case "revoke":
		return d.revoke(req.Query, req.Text)
	case "react":
		return d.react(req.Query, req.Text, req.Reply)
	case "send":
		return d.send(req)
	case "find":
		return d.find(req.Query, req.Limit)
	case "findchat":
		return d.findChat(req.Query, req.Text, req.Reply, req.File == "backfill")
	case "media":
		return d.openMedia(req.Query, req.Text)
	case "logout":
		return d.logout()
	default:
		return fail(fmt.Errorf("unknown command"))
	}
}

func (d *Daemon) status() ipc.Response {
	st := &ipc.Status{Pairing: d.waiting.Load(), Code: d.currentQR()}
	if d.client != nil {
		st.Connected = d.client.IsConnected()
		if d.client.Store != nil {
			st.PushName = d.client.Store.PushName
			if d.client.Store.ID != nil {
				st.Linked = true
				st.JID = direct.Canonical(*d.client.Store.ID).String()
			}
		}
	}
	if n, err := d.app.Count(); err == nil {
		st.Chats = n
	}
	return ipc.Response{OK: true, Status: st}
}

func (d *Daemon) chats(limit int) ipc.Response {
	if err := d.requireLinked(); err != nil {
		return fail(err)
	}
	limit = clamp(limit, 30, 1000)
	chats, err := d.app.Chats(limit)
	if err != nil {
		return fail(err)
	}
	if chats == nil {
		chats = []store.Chat{}
	}
	return ipc.Response{OK: true, Chats: chats}
}

func (d *Daemon) read(query string, limit int) ipc.Response {
	if err := d.requireLinked(); err != nil {
		return fail(err)
	}
	limit = clamp(limit, 50, 1000)
	chat, err := d.resolve(query)
	if err != nil {
		return fail(err)
	}
	msgs, err := d.app.Messages(chat.JID, limit)
	if err != nil {
		return fail(err)
	}
	if msgs == nil {
		msgs = []store.Message{}
	}
	if len(msgs) == 0 {
		d.askBackfill(chat.JID, "")
	}
	if err := d.app.MarkRead(chat.JID); err != nil {
		return fail(err)
	}
	chat.Unread = 0
	d.markRemote(chat.JID, msgs)
	return ipc.Response{OK: true, Chat: &chat, Messages: msgs}
}

func (d *Daemon) older(query, beforeID string) ipc.Response {
	if err := d.requireLinked(); err != nil {
		return fail(err)
	}
	if strings.TrimSpace(beforeID) == "" {
		return fail(fmt.Errorf("missing message"))
	}
	chat, err := d.resolve(query)
	if err != nil {
		return fail(err)
	}
	msgs, err := d.app.MessagesBefore(chat.JID, beforeID, 50)
	if err != nil {
		return fail(err)
	}
	if len(msgs) == 0 {
		d.askBackfill(chat.JID, beforeID)
	}
	if msgs == nil {
		msgs = []store.Message{}
	}
	return ipc.Response{OK: true, Chat: &chat, Messages: msgs}
}

func (d *Daemon) presence(query, state string) ipc.Response {
	if err := d.requireLinked(); err != nil {
		return fail(err)
	}
	if d.client == nil || !d.client.IsConnected() {
		return fail(fmt.Errorf("not connected"))
	}
	if state == "available" || state == "unavailable" {
		kind := types.PresenceUnavailable
		if state == "available" {
			kind = types.PresenceAvailable
		}
		ctx, cancel := context.WithTimeout(d.ctx, 10*time.Second)
		defer cancel()
		if err := d.client.SendPresence(ctx, kind); err != nil {
			return fail(err)
		}
		return ipc.Response{OK: true}
	}
	var kind types.ChatPresence
	switch state {
	case "composing":
		kind = types.ChatPresenceComposing
	case "paused":
		kind = types.ChatPresencePaused
	default:
		return fail(fmt.Errorf("unknown presence"))
	}
	chat, err := d.resolve(query)
	if err != nil {
		return fail(err)
	}
	jid, err := types.ParseJID(chat.JID)
	if err != nil {
		return fail(err)
	}
	ctx, cancel := context.WithTimeout(d.ctx, 10*time.Second)
	defer cancel()
	if err := d.client.SendChatPresence(ctx, jid, kind, types.ChatPresenceMedia("")); err != nil {
		return fail(err)
	}
	return ipc.Response{OK: true}
}

func (d *Daemon) askBackfill(chat, anchor string) {
	d.mu.Lock()
	if d.backfill == nil {
		d.backfill = map[string]backfillWait{}
	}
	if d.askedEmpty == nil {
		d.askedEmpty = map[string]struct{}{}
	}
	if _, busy := d.backfill[chat]; busy {
		d.mu.Unlock()
		return
	}
	if anchor == "" {
		if _, ok := d.askedEmpty[chat]; ok {
			d.mu.Unlock()
			return
		}
		d.askedEmpty[chat] = struct{}{}
	}
	d.backfillN++
	token := d.backfillN
	d.backfill[chat] = backfillWait{anchor: anchor, token: token}
	d.mu.Unlock()
	go d.sendBackfill(chat, anchor, token)
}

func (d *Daemon) sendBackfill(chat, anchor string, token uint64) {
	fail := func() { d.clearBackfill(chat, anchor, token) }
	if d.client == nil || !d.client.IsConnected() {
		fail()
		return
	}
	jid, err := types.ParseJID(chat)
	if err != nil {
		fail()
		return
	}
	info := &types.MessageInfo{Timestamp: time.Now()}
	info.Chat = jid
	if anchor != "" {
		msg, err := d.app.Media(chat, anchor)
		if err != nil {
			fail()
			return
		}
		info.ID = types.MessageID(msg.ID)
		info.IsFromMe = msg.FromMe
		if !msg.At.IsZero() {
			info.Timestamp = msg.At
		}
	}
	ctx, cancel := context.WithTimeout(d.ctx, 30*time.Second)
	defer cancel()
	if _, err := d.client.SendPeerMessage(ctx, d.client.BuildHistorySyncRequest(info, 50)); err != nil {
		fmt.Fprintf(os.Stderr, "history: %v\n", err)
		fail()
		return
	}
	time.AfterFunc(45*time.Second, func() {
		d.mu.Lock()
		cur, ok := d.backfill[chat]
		if ok && cur.token == token {
			delete(d.backfill, chat)
		}
		d.mu.Unlock()
	})
}

func (d *Daemon) clearBackfill(chat, anchor string, token uint64) {
	d.mu.Lock()
	defer d.mu.Unlock()
	cur, ok := d.backfill[chat]
	if !ok || cur.token != token {
		return
	}
	delete(d.backfill, chat)
	if anchor == "" {
		delete(d.askedEmpty, chat)
	}
}

func (d *Daemon) finishBackfill(chat string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, ok := d.backfill[chat]; !ok {
		return false
	}
	delete(d.backfill, chat)
	return true
}

func (d *Daemon) findChat(query, text, oldest string, backfill bool) ipc.Response {
	if err := d.requireLinked(); err != nil {
		return fail(err)
	}
	chat, err := d.resolve(query)
	if err != nil {
		return fail(err)
	}
	msg, ok, err := d.app.FindInChat(chat.JID, text)
	if err != nil {
		return fail(err)
	}
	if !ok {
		if backfill {
			d.askBackfill(chat.JID, oldest)
		}
		return ipc.Response{OK: true, Messages: []store.Message{}}
	}
	msgs, err := d.app.MessagesSpan(chat.JID, msg.ID, oldest, 300)
	if err != nil {
		return fail(err)
	}
	if msgs == nil {
		msgs = []store.Message{}
	}
	return ipc.Response{OK: true, Messages: msgs}
}

func (d *Daemon) find(query string, limit int) ipc.Response {
	if err := d.requireLinked(); err != nil {
		return fail(err)
	}
	limit = clamp(limit, 50, 1000)
	hits, err := d.app.Find(query, limit)
	if err != nil {
		return fail(err)
	}
	if hits == nil {
		hits = []store.Hit{}
	}
	return ipc.Response{OK: true, Hits: hits}
}

func (d *Daemon) send(req ipc.Request) ipc.Response {
	if strings.TrimSpace(req.File) != "" {
		return d.sendFile(req)
	}
	args := req.Args
	if len(args) == 0 && req.Query != "" {
		args = []string{req.Query, req.Text}
	}
	query, text, err := splitSend(args, d.matchCount)
	if err != nil {
		return fail(err)
	}
	return d.sendText(query, text, req.Reply)
}

func (d *Daemon) matchCount(query string) (int, error) {
	matches, err := d.app.Resolve(query)
	if err != nil {
		return 0, err
	}
	if len(matches) > 0 {
		return len(matches), nil
	}
	if _, ok := phoneDigits(query); ok {
		return 1, nil
	}
	return 0, nil
}

func (d *Daemon) sendText(query, text, replyID string) ipc.Response {
	if err := d.requireLinked(); err != nil {
		return fail(err)
	}
	if !d.client.IsConnected() {
		return fail(fmt.Errorf("not connected"))
	}
	if strings.TrimSpace(text) == "" {
		return fail(fmt.Errorf("message is empty"))
	}
	if len(text) > 65536 {
		return fail(fmt.Errorf("message is too long"))
	}
	chat, err := d.resolve(query)
	if err != nil {
		return fail(err)
	}
	jid, err := types.ParseJID(chat.JID)
	if err != nil {
		return fail(err)
	}
	quoted, err := d.quoted(chat.JID, replyID)
	if err != nil {
		return fail(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	resp, err := d.client.SendMessage(ctx, jid, textPayload(text, quoted))
	if err != nil {
		return fail(err)
	}
	at := resp.Timestamp
	if at.IsZero() {
		at = time.Now()
	}
	replyTo, replyText := replyFields(quoted)
	in := store.Incoming{
		Message: store.Message{
			ID:        resp.ID,
			Chat:      chat.JID,
			Sender:    ownJID(d.client),
			FromMe:    true,
			Text:      text,
			ReplyTo:   replyTo,
			ReplyText: replyText,
			Receipt:   "sent",
			At:        at,
		},
		AltJID:   chat.AltJID,
		Name:     chat.Name,
		PushName: chat.PushName,
		Phone:    chat.Phone,
	}
	saved, changed, err := d.app.SaveLive(in)
	if err != nil {
		return fail(err)
	}
	if changed {
		d.broadcast(ipc.Live{ID: resp.ID, Chat: saved.JID, Name: saved.Label(), FromMe: true, Text: text, At: at})
	}
	msg := in.Message
	return ipc.Response{OK: true, Chat: &saved, Sent: &msg}
}

func (d *Daemon) logout() ipc.Response {
	if !d.linked() {
		return fail(fmt.Errorf("not linked"))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := d.client.Logout(ctx); err != nil {
		return fail(err)
	}
	return ipc.Response{OK: true}
}

func (d *Daemon) resolve(query string) (store.Chat, error) {
	matches, err := d.app.Resolve(query)
	if err != nil {
		return store.Chat{}, err
	}
	switch len(matches) {
	case 0:
		if phone, ok := phoneDigits(query); ok {
			return d.resolvePhone(phone)
		}
		return store.Chat{}, fmt.Errorf("no chat matching %q", query)
	case 1:
		return matches[0], nil
	default:
		return store.Chat{}, ambiguous(matches)
	}
}

func (d *Daemon) resolvePhone(phone string) (store.Chat, error) {
	if !d.client.IsConnected() {
		return store.Chat{}, fmt.Errorf("not connected")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	resp, err := d.client.IsOnWhatsApp(ctx, []string{phone})
	if err != nil {
		return store.Chat{}, err
	}
	if len(resp) == 0 || !resp[0].IsIn {
		return store.Chat{}, fmt.Errorf("+%s is not on WhatsApp", phone)
	}
	jid := direct.Canonical(resp[0].JID)
	if !direct.Is(jid) {
		return store.Chat{}, fmt.Errorf("+%s is not a direct chat", phone)
	}
	phoneOut := direct.Phone(resp[0].PhoneNumber)
	if phoneOut == "" {
		phoneOut = phone
	}
	alt := ""
	if other, err := d.client.Store.GetAltJID(ctx, jid); err == nil && direct.Is(other) {
		other = direct.Canonical(other)
		if other.String() != jid.String() {
			alt = other.String()
		}
		if p := direct.Phone(other); p != "" {
			phoneOut = p
		}
	}
	if err := d.app.Remember(jid.String(), alt, "", "", phoneOut); err != nil {
		return store.Chat{}, err
	}
	chat, ok, err := d.app.Chat(jid.String())
	if err != nil {
		return store.Chat{}, err
	}
	if !ok {
		return store.Chat{JID: jid.String(), AltJID: alt, Phone: phoneOut}, nil
	}
	return chat, nil
}

func (d *Daemon) markRemote(chatJID string, msgs []store.Message) {
	if d.client == nil || !d.client.IsConnected() {
		return
	}
	var ids []types.MessageID
	var sender types.JID
	for _, m := range msgs {
		if m.FromMe || m.ID == "" {
			continue
		}
		ids = append(ids, m.ID)
		if sender.IsEmpty() && m.Sender != "" {
			if parsed, err := types.ParseJID(m.Sender); err == nil {
				sender = parsed
			}
		}
	}
	if len(ids) == 0 {
		return
	}
	chat, err := types.ParseJID(chatJID)
	if err != nil {
		return
	}
	if sender.IsEmpty() {
		sender = chat
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := d.client.MarkRead(ctx, ids, time.Now(), chat, sender); err != nil {
		fmt.Fprintf(os.Stderr, "read receipt: %v\n", err)
	}
}

func (d *Daemon) onEvent(evt any) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "event handler: %v\n", r)
		}
	}()
	switch v := evt.(type) {
	case *events.Message:
		d.onMessage(v)
	case *events.UndecryptableMessage:
		d.onViewOnce(v)
	case *events.HistorySync:
		select {
		case d.history <- v:
		case <-d.ctx.Done():
		}
	case *events.PushName:
		d.onPushName(v)
	case *events.Contact:
		d.onContact(v)
	case *events.Receipt:
		d.onReceipt(v)
	case *events.ChatPresence:
		d.onChatPresence(v)
	case *events.GroupInfo:
		d.onGroupInfo(v)
	case *events.DeleteForMe:
		d.onDeleteForMe(v)
	case *events.ClearChat:
		d.onClearChat(v)
	case *events.DeleteChat:
		d.onDeleteChat(v)
	case *events.MarkChatAsRead:
		d.onMarkRead(v)
	case *events.Connected:
		d.waiting.Store(false)
		if id := d.client.Store.ID; id != nil {
			fmt.Fprintf(os.Stderr, "connected as %s\n", id.ToNonAD())
		}
		go func() {
			d.syncContacts()
			d.syncGroups()
		}()
	case *events.LoggedOut:
		fmt.Fprintf(os.Stderr, "unlinked (%v)\n", v.Reason)
		d.cancel()
	case *events.StreamReplaced:
		fmt.Fprintf(os.Stderr, "disconnected: another wp process took this session\n")
		d.cancel()
	case *events.TemporaryBan:
		fmt.Fprintf(os.Stderr, "temporarily banned: %s\n", v.String())
		d.cancel()
	case *events.ClientOutdated:
		fmt.Fprintf(os.Stderr, "this WhatsApp client is outdated; rebuild wp to update it\n")
		d.cancel()
	case *events.ConnectFailure:
		fmt.Fprintf(os.Stderr, "connect failure: %s\n", v.Reason)
	}
}

func (d *Daemon) onMessage(v *events.Message) {
	if v == nil || !direct.Allowed(v.Info.Chat) {
		return
	}
	if v.Message != nil && v.Message.GetReactionMessage() != nil {
		d.onReaction(v)
		return
	}
	if v.Info.Edit == types.EditAttributeSenderRevoke || v.Info.Edit == types.EditAttributeAdminRevoke {
		d.tombstone(v.Info.Chat, v.Info.ID, v.Info.IsFromMe, true)
		return
	}
	if id, ok := msgtext.RevokeID(v.RawMessage); ok {
		d.tombstone(v.Info.Chat, id, v.Info.IsFromMe, true)
		return
	}
	if id, ok := msgtext.RevokeID(v.Message); ok {
		d.tombstone(v.Info.Chat, id, v.Info.IsFromMe, true)
		return
	}
	text, att, quote := msgtext.Inspect(v.Message, v.IsViewOnce)
	if text == "" {
		return
	}
	chat := direct.Canonical(v.Info.Chat)
	alt := messageAlt(v.Info.MessageSource)
	if alt.String() == chat.String() {
		alt = types.EmptyJID
	}
	phone := direct.Phone(chat)
	if phone == "" {
		phone = direct.Phone(alt)
	}
	at := v.Info.Timestamp
	if at.IsZero() {
		at = time.Now()
	}
	in := store.Incoming{
		Message: store.Message{
			ID:     v.Info.ID,
			Chat:   chat.String(),
			Sender: direct.Canonical(v.Info.Sender).String(),
			FromMe: v.Info.IsFromMe,
			Text:   text,
			At:     at,
		},
		AltJID: jidString(alt),
		Phone:  phone,
		Unread: !v.Info.IsFromMe && !v.IsEdit && v.Info.Edit != types.EditAttributeMessageEdit,
		Edit:   v.IsEdit || v.Info.Edit == types.EditAttributeMessageEdit,
	}
	if att != nil {
		in.Kind = att.Kind
		in.Caption = att.Caption
		in.Mime = att.Mime
		in.FileName = att.FileName
		in.FileSize = att.Size
		in.MediaState = att.State()
		in.MediaRef = att.Ref
	}
	if direct.IsGroup(chat) {
		if !v.Info.IsFromMe {
			in.SenderName = d.personLabel(direct.Canonical(v.Info.Sender), v.Info.PushName)
		}
	} else {
		saved, pushName := d.savedAndPush(chat)
		if !alt.IsEmpty() {
			s2, p2 := d.savedAndPush(alt)
			if saved == "" {
				saved = s2
			}
			if pushName == "" {
				pushName = p2
			}
		}
		in.Name = saved
		if !v.Info.IsFromMe {
			if pushName == "" {
				pushName = strings.TrimSpace(v.Info.PushName)
			}
			in.PushName = pushName
		}
	}
	in.ReplyTo, in.ReplyText = d.quoteFields(chat.String(), quote)
	saved, changed, err := d.app.SaveLive(in)
	if err != nil {
		fmt.Fprintf(os.Stderr, "save message: %v\n", err)
		return
	}
	if changed {
		d.broadcast(ipc.Live{
			ID:     in.ID,
			Chat:   saved.JID,
			Name:   saved.Label(),
			FromMe: in.FromMe,
			Text:   in.Text,
			At:     in.At,
			Edited: in.Edit,
		})
	}
	if direct.IsGroup(chat) && saved.Name == "" {
		go d.ensureGroupName(chat)
	}
	if changed && att != nil && att.Eager && !att.ViewOnce && att.Ref != "" {
		d.cacheLater(saved.JID, in.ID)
	}
}

func (d *Daemon) onViewOnce(v *events.UndecryptableMessage) {
	if v == nil || v.Info.ID == "" || !direct.Allowed(v.Info.Chat) {
		return
	}
	if !strings.Contains(strings.ToLower(string(v.UnavailableType)), "view") {
		return
	}
	if d.client != nil && d.client.IsConnected() {
		info := v.Info
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			if _, err := d.client.SendPeerMessage(ctx, d.client.BuildUnavailableMessageRequest(info.Chat, info.Sender, info.ID)); err != nil {
				fmt.Fprintf(os.Stderr, "view once: %v\n", err)
			}
		}()
	}
	chat := direct.Canonical(v.Info.Chat)
	alt := messageAlt(v.Info.MessageSource)
	if alt.String() == chat.String() {
		alt = types.EmptyJID
	}
	phone := direct.Phone(chat)
	if phone == "" {
		phone = direct.Phone(alt)
	}
	at := v.Info.Timestamp
	if at.IsZero() {
		at = time.Now()
	}
	in := store.Incoming{
		Message: store.Message{
			ID:     v.Info.ID,
			Chat:   chat.String(),
			Sender: direct.Canonical(v.Info.Sender).String(),
			FromMe: v.Info.IsFromMe,
			Text:   "view once",
			At:     at,
		},
		AltJID: jidString(alt),
		Phone:  phone,
		Unread: !v.Info.IsFromMe,
		Keep:   true,
	}
	if direct.IsGroup(chat) {
		if !v.Info.IsFromMe {
			in.SenderName = d.personLabel(direct.Canonical(v.Info.Sender), v.Info.PushName)
		}
	} else {
		saved, pushName := d.savedAndPush(chat)
		if !alt.IsEmpty() {
			s2, p2 := d.savedAndPush(alt)
			if saved == "" {
				saved = s2
			}
			if pushName == "" {
				pushName = p2
			}
		}
		in.Name = saved
		if !v.Info.IsFromMe {
			if pushName == "" {
				pushName = strings.TrimSpace(v.Info.PushName)
			}
			in.PushName = pushName
		}
	}
	saved, changed, err := d.app.SaveLive(in)
	if err != nil {
		fmt.Fprintf(os.Stderr, "view once: %v\n", err)
		return
	}
	if !changed {
		return
	}
	d.broadcast(ipc.Live{
		ID: in.ID, Chat: saved.JID, Name: saved.Label(), FromMe: in.FromMe, Text: in.Text, At: in.At,
	})
}

func (d *Daemon) onHistory(v *events.HistorySync) {
	if v == nil || v.Data == nil {
		return
	}
	n := 0
	for _, conv := range v.Data.GetConversations() {
		jid, err := types.ParseJID(conv.GetID())
		if err != nil || !direct.Allowed(jid) {
			continue
		}
		jid = direct.Canonical(jid)
		var msgs []store.Message
		alt := historyAlt(jid, conv.GetPnJID(), conv.GetLidJID())
		phone := direct.Phone(jid)
		if p := direct.Phone(alt); p != "" {
			phone = p
		}
		var name, push string
		if direct.IsGroup(jid) {
			name = strings.TrimSpace(conv.GetName())
			if name == "" {
				name = strings.TrimSpace(conv.GetDisplayName())
			}
		} else {
			name, push = d.savedAndPush(jid)
			if !alt.IsEmpty() {
				s2, p2 := d.savedAndPush(alt)
				if name == "" {
					name = s2
				}
				if push == "" {
					push = p2
				}
			}
			if push == "" {
				push = strings.TrimSpace(conv.GetName())
				if push == "" {
					push = strings.TrimSpace(conv.GetDisplayName())
				}
			}
		}
		own := ownJID(d.client)
		for _, hm := range conv.GetMessages() {
			web := hm.GetMessage()
			parsed, err := d.client.ParseWebMessage(jid, web)
			if err != nil || parsed == nil || !direct.Allowed(parsed.Info.Chat) {
				continue
			}
			applyHistoryReactions(d.app, jid.String(), own, web)
			text, att, quote := msgtext.Inspect(parsed.Message, parsed.IsViewOnce)
			if text == "" {
				continue
			}
			a := messageAlt(parsed.Info.MessageSource)
			if !a.IsEmpty() && a.String() != jid.String() {
				alt = a
			}
			if p := direct.Phone(a); p != "" {
				phone = p
			}
			at := parsed.Info.Timestamp
			msg := store.Message{
				ID:     parsed.Info.ID,
				Chat:   jid.String(),
				Sender: direct.Canonical(parsed.Info.Sender).String(),
				FromMe: parsed.Info.IsFromMe,
				Text:   text,
				At:     at,
			}
			if direct.IsGroup(jid) && !parsed.Info.IsFromMe {
				msg.SenderName = d.personLabel(direct.Canonical(parsed.Info.Sender), parsed.Info.PushName)
			}
			if att != nil {
				msg.Kind = att.Kind
				msg.Caption = att.Caption
				msg.Mime = att.Mime
				msg.FileName = att.FileName
				msg.FileSize = att.Size
				msg.MediaState = att.State()
				msg.MediaRef = att.Ref
			}
			msg.ReplyTo, msg.ReplyText = d.quoteFields(jid.String(), quote)
			msgs = append(msgs, msg)
		}
		if !direct.IsGroup(jid) && name == "" && !alt.IsEmpty() {
			s2, p2 := d.savedAndPush(alt)
			name = s2
			if push == "" {
				push = p2
			}
		}
		var lastAt time.Time
		if ts := conv.GetConversationTimestamp(); ts > 0 {
			lastAt = time.Unix(int64(ts), 0)
		}
		unread := int(conv.GetUnreadCount())
		if conv.GetMarkedAsUnread() && unread == 0 {
			unread = 1
		}
		if err := d.app.SaveHistory(jid.String(), jidString(alt), name, push, phone, unread, lastAt, "", msgs); err != nil {
			fmt.Fprintf(os.Stderr, "history: %v\n", err)
			continue
		}
		if d.finishBackfill(jid.String()) {
			d.broadcast(ipc.Live{Chat: jid.String(), Refresh: true})
		}
		if name == "" && direct.IsGroup(jid) {
			go d.ensureGroupName(jid)
		}
		n += len(msgs)
	}
	for _, pn := range v.Data.GetPushnames() {
		jid, err := types.ParseJID(pn.GetID())
		if err != nil || !direct.Is(jid) || pn.GetPushname() == "" {
			continue
		}
		jid = direct.Canonical(jid)
		if err := d.app.Remember(jid.String(), "", "", pn.GetPushname(), direct.Phone(jid)); err != nil {
			fmt.Fprintf(os.Stderr, "push name: %v\n", err)
		}
	}
	if n > 0 {
		fmt.Fprintf(os.Stderr, "history: %d messages\n", n)
	}
}

func (d *Daemon) tombstone(chat types.JID, id string, fromMe, tell bool) {
	if id == "" || !direct.Allowed(chat) {
		return
	}
	saved, ok, err := d.app.MarkDeleted(direct.Canonical(chat).String(), id)
	if err != nil {
		fmt.Fprintf(os.Stderr, "delete message: %v\n", err)
		return
	}
	if !ok || !tell {
		return
	}
	d.broadcast(ipc.Live{ID: id, Chat: saved.JID, Name: saved.Label(), FromMe: fromMe, Text: "deleted", Deleted: true, At: time.Now()})
}

func (d *Daemon) onChatPresence(v *events.ChatPresence) {
	if v == nil || v.IsFromMe || !direct.Allowed(v.Chat) {
		return
	}
	chat := direct.Canonical(v.Chat)
	name := ""
	if direct.IsGroup(chat) {
		name = d.personLabel(direct.Canonical(v.Sender), "")
	}
	d.broadcast(ipc.Live{Chat: chat.String(), Name: name, Presence: string(v.State)})
}

func (d *Daemon) revoke(query, id string) ipc.Response {
	if err := d.requireLinked(); err != nil {
		return fail(err)
	}
	if d.client == nil || !d.client.IsConnected() {
		return fail(fmt.Errorf("not connected"))
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return fail(fmt.Errorf("no message"))
	}
	chat, err := d.resolve(query)
	if err != nil {
		return fail(err)
	}
	msg, err := d.app.Media(chat.JID, id)
	if err != nil {
		return fail(err)
	}
	if msg.Deleted {
		return fail(fmt.Errorf("already deleted"))
	}
	jid, err := types.ParseJID(chat.JID)
	if err != nil {
		return fail(err)
	}
	sender := types.EmptyJID
	if !msg.FromMe && msg.Sender != "" {
		sender, err = types.ParseJID(msg.Sender)
		if err != nil {
			return fail(err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := d.client.SendMessage(ctx, jid, d.client.BuildRevoke(jid, sender, msg.ID)); err != nil {
		return fail(err)
	}
	saved, ok, err := d.app.MarkDeleted(chat.JID, id)
	if err != nil {
		return fail(err)
	}
	if ok {
		d.broadcast(ipc.Live{ID: id, Chat: saved.JID, Name: saved.Label(), FromMe: msg.FromMe, Text: "deleted", Deleted: true, At: time.Now()})
	}
	return ipc.Response{OK: true, Chat: &saved}
}

func (d *Daemon) forget(chat types.JID, id string, fromMe, tell bool) {
	if id == "" || !direct.Allowed(chat) {
		return
	}
	saved, ok, err := d.app.DeleteMessage(direct.Canonical(chat).String(), id)
	if err != nil {
		fmt.Fprintf(os.Stderr, "delete message: %v\n", err)
		return
	}
	if !ok || !tell {
		return
	}
	d.broadcast(ipc.Live{ID: id, Chat: saved.JID, Name: saved.Label(), FromMe: fromMe, Text: "[deleted]", At: time.Now()})
}

func (d *Daemon) onDeleteForMe(v *events.DeleteForMe) {
	if v == nil {
		return
	}
	d.forget(v.ChatJID, v.MessageID, v.IsFromMe, !v.FromFullSync)
}

func (d *Daemon) onClearChat(v *events.ClearChat) {
	if v == nil || !direct.Allowed(v.JID) {
		return
	}
	if err := d.app.ClearChat(direct.Canonical(v.JID).String()); err != nil {
		fmt.Fprintf(os.Stderr, "clear chat: %v\n", err)
	}
}

func (d *Daemon) onDeleteChat(v *events.DeleteChat) {
	if v == nil || !direct.Allowed(v.JID) {
		return
	}
	if err := d.app.DeleteChat(direct.Canonical(v.JID).String()); err != nil {
		fmt.Fprintf(os.Stderr, "delete chat: %v\n", err)
	}
}

func (d *Daemon) onMarkRead(v *events.MarkChatAsRead) {
	if v == nil || v.Action == nil || !direct.Allowed(v.JID) {
		return
	}
	jid := direct.Canonical(v.JID).String()
	n := 0
	if !v.Action.GetRead() {
		n = 1
	}
	if err := d.app.SetUnread(jid, n); err != nil {
		fmt.Fprintf(os.Stderr, "mark read: %v\n", err)
	}
}

func (d *Daemon) onPushName(v *events.PushName) {
	jid := direct.Canonical(v.JID)
	if !direct.Is(jid) || v.NewPushName == "" {
		return
	}
	alt := ""
	if direct.Is(v.JIDAlt) {
		other := direct.Canonical(v.JIDAlt)
		if other.String() != jid.String() {
			alt = other.String()
		}
	}
	if err := d.app.Remember(jid.String(), alt, "", v.NewPushName, direct.Phone(jid)); err != nil {
		fmt.Fprintf(os.Stderr, "push name: %v\n", err)
	}
}

func (d *Daemon) onContact(v *events.Contact) {
	if v.Action == nil || !direct.Is(v.JID) {
		return
	}
	jid := direct.Canonical(v.JID)
	saved := strings.TrimSpace(v.Action.GetFullName())
	if saved == "" {
		saved = strings.TrimSpace(v.Action.GetFirstName())
	}
	altJID := types.EmptyJID
	phone := direct.Phone(jid)
	if pn := v.Action.GetPnJID(); pn != "" {
		if parsed, err := types.ParseJID(pn); err == nil && direct.Is(parsed) {
			parsed = direct.Canonical(parsed)
			if parsed.String() != jid.String() {
				altJID = parsed
			}
			if phone == "" {
				phone = direct.Phone(parsed)
			}
		}
	}
	if altJID.IsEmpty() {
		if lid := v.Action.GetLidJID(); lid != "" {
			if parsed, err := types.ParseJID(lid); err == nil && direct.Is(parsed) {
				parsed = direct.Canonical(parsed)
				if parsed.String() != jid.String() {
					altJID = parsed
				}
			}
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if altJID.IsEmpty() {
		altJID = d.otherUser(ctx, jid)
	}
	if saved == "" {
		saved, _ = contactFields(d.contactInfo(ctx, jid))
		if saved == "" && !altJID.IsEmpty() {
			saved, _ = contactFields(d.contactInfo(ctx, altJID))
		}
	}
	d.storeContact(jid, saved, "", phone, jidString(altJID))
}

func (d *Daemon) quoteFields(chat string, q *msgtext.Quote) (string, string) {
	if q == nil || q.ID == "" {
		return "", ""
	}
	text := q.Text
	if existing, err := d.app.Media(chat, q.ID); err == nil {
		if p := msgtext.Snip(strings.Join(strings.Fields(existing.Preview()), " "), 80); p != "" {
			text = p
		}
	}
	return q.ID, text
}

func (d *Daemon) onGroupInfo(v *events.GroupInfo) {
	if v == nil || v.Name == nil || strings.TrimSpace(v.Name.Name) == "" || !direct.IsGroup(v.JID) {
		return
	}
	jid := direct.Canonical(v.JID).String()
	if err := d.app.Remember(jid, "", v.Name.Name, "", ""); err != nil {
		fmt.Fprintf(os.Stderr, "group name: %v\n", err)
	}
}

func (d *Daemon) ensureGroupName(jid types.JID) {
	jid = direct.Canonical(jid)
	id := jid.String()
	d.mu.Lock()
	if d.groupWait == nil {
		d.groupWait = map[string]struct{}{}
	}
	if _, busy := d.groupWait[id]; busy {
		d.mu.Unlock()
		return
	}
	d.groupWait[id] = struct{}{}
	d.mu.Unlock()
	defer func() {
		d.mu.Lock()
		delete(d.groupWait, id)
		d.mu.Unlock()
	}()
	if chat, ok, err := d.app.Chat(id); err != nil || (ok && chat.Name != "") {
		return
	}
	ctx, cancel := context.WithTimeout(d.ctx, 15*time.Second)
	defer cancel()
	info, err := d.client.GetGroupInfo(ctx, jid)
	if err != nil || info == nil || info.Name == "" {
		return
	}
	if err := d.app.Remember(id, "", info.Name, "", ""); err != nil {
		fmt.Fprintf(os.Stderr, "group name: %v\n", err)
	}
}

func (d *Daemon) onReceipt(v *events.Receipt) {
	if v == nil || !direct.Allowed(v.Chat) {
		return
	}
	chat := direct.Canonical(v.Chat).String()
	if v.Type == types.ReceiptTypeReadSelf {
		if err := d.app.MarkRead(chat); err != nil {
			fmt.Fprintf(os.Stderr, "read receipt: %v\n", err)
		}
		return
	}
	if !direct.Is(v.Chat) {
		return
	}
	state := ""
	switch v.Type {
	case types.ReceiptTypeDelivered:
		state = "delivered"
	case types.ReceiptTypeRead:
		state = "read"
	default:
		return
	}
	ids := make([]string, 0, len(v.MessageIDs))
	for _, id := range v.MessageIDs {
		ids = append(ids, string(id))
	}
	updated, err := d.app.SetReceipt(chat, ids, state, v.Timestamp)
	if err != nil {
		fmt.Fprintf(os.Stderr, "receipt: %v\n", err)
		return
	}
	for _, m := range updated {
		d.broadcast(ipc.Live{ID: m.ID, Chat: m.Chat, FromMe: true, Receipt: m.Receipt, At: m.At})
	}
}

func (d *Daemon) syncContacts() {
	if d.client == nil || d.client.Store == nil || d.client.Store.Contacts == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	all, err := d.client.Store.Contacts.GetAllContacts(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "contacts: %v\n", err)
		return
	}
	for jid, info := range all {
		if !direct.Is(jid) {
			continue
		}
		jid = direct.Canonical(jid)
		saved, push := contactFields(info, true)
		if saved == "" && push == "" {
			continue
		}
		alt := d.otherUser(ctx, jid)
		phone := direct.Phone(jid)
		if phone == "" {
			phone = direct.Phone(alt)
		}
		d.storeContact(jid, saved, push, phone, jidString(alt))
	}
}

func (d *Daemon) syncGroups() {
	if d.client == nil {
		return
	}
	ctx, cancel := context.WithTimeout(d.ctx, 30*time.Second)
	defer cancel()
	groups, err := d.client.GetJoinedGroups(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "groups: %v\n", err)
		return
	}
	for _, info := range groups {
		if info == nil || !direct.IsGroup(info.JID) {
			continue
		}
		jid := direct.Canonical(info.JID)
		name := strings.TrimSpace(info.Name)
		if err := d.app.Remember(jid.String(), "", name, "", ""); err != nil {
			fmt.Fprintf(os.Stderr, "group: %v\n", err)
			continue
		}
		at := info.GroupCreated
		if at.IsZero() {
			at = time.Now()
		}
		if err := d.app.SeenIfEmpty(jid.String(), at); err != nil {
			fmt.Fprintf(os.Stderr, "group: %v\n", err)
		}
	}
}

func (d *Daemon) storeContact(j types.JID, saved, push, phone, alt string) {
	if !direct.Is(j) {
		return
	}
	j = direct.Canonical(j)
	if saved == "" && push == "" && alt == "" && phone == "" {
		return
	}
	if err := d.app.Remember(j.String(), alt, saved, push, phone); err != nil {
		fmt.Fprintf(os.Stderr, "contact: %v\n", err)
		return
	}
	if saved == "" {
		return
	}
	ids := []string{j.String()}
	if alt != "" {
		ids = append(ids, alt)
	}
	if err := d.app.RenameSender(ids, saved); err != nil {
		fmt.Fprintf(os.Stderr, "contact: %v\n", err)
	}
}

func (d *Daemon) savedAndPush(j types.JID) (string, string) {
	if j.IsEmpty() || d.client == nil || d.client.Store == nil || d.client.Store.Contacts == nil {
		return "", ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	saved, push := contactFields(d.contactInfo(ctx, j))
	other := d.otherUser(ctx, j)
	if !other.IsEmpty() {
		s2, p2 := contactFields(d.contactInfo(ctx, other))
		if saved == "" {
			saved = s2
		}
		if push == "" {
			push = p2
		}
	}
	return saved, push
}

func (d *Daemon) personLabel(j types.JID, fallback string) string {
	saved, push := d.savedAndPush(j)
	if saved != "" {
		return saved
	}
	if push != "" {
		return push
	}
	if s := strings.TrimSpace(fallback); s != "" {
		return s
	}
	if p := direct.Phone(j); p != "" {
		return "+" + p
	}
	if j.User != "" {
		return j.User
	}
	return "someone"
}

func (d *Daemon) contactInfo(ctx context.Context, j types.JID) (types.ContactInfo, bool) {
	if j.IsEmpty() || d.client == nil || d.client.Store == nil || d.client.Store.Contacts == nil {
		return types.ContactInfo{}, false
	}
	info, err := d.client.Store.Contacts.GetContact(ctx, j)
	if err != nil || !info.Found {
		return types.ContactInfo{}, false
	}
	return info, true
}

func (d *Daemon) otherUser(ctx context.Context, j types.JID) types.JID {
	if j.IsEmpty() || d.client == nil || d.client.Store == nil {
		return types.EmptyJID
	}
	other, err := d.client.Store.GetAltJID(ctx, j)
	if err != nil || !direct.Is(other) {
		return types.EmptyJID
	}
	other = direct.Canonical(other)
	if other.String() == j.String() {
		return types.EmptyJID
	}
	return other
}

func contactFields(info types.ContactInfo, ok bool) (string, string) {
	if !ok {
		return "", ""
	}
	saved := strings.TrimSpace(info.FullName)
	if saved == "" {
		saved = strings.TrimSpace(info.FirstName)
	}
	if saved == "" {
		saved = strings.TrimSpace(info.BusinessName)
	}
	return saved, strings.TrimSpace(info.PushName)
}

func (d *Daemon) subscribe() (<-chan ipc.Live, func()) {
	ch := make(chan ipc.Live, 64)
	d.mu.Lock()
	d.next++
	id := d.next
	d.subs[id] = ch
	d.mu.Unlock()
	return ch, func() {
		d.mu.Lock()
		delete(d.subs, id)
		d.mu.Unlock()
	}
}

func (d *Daemon) broadcast(ev ipc.Live) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, ch := range d.subs {
		select {
		case ch <- ev:
		default:
		}
	}
}

func (d *Daemon) setQR(code string) {
	d.qr.Store(code)
}

func (d *Daemon) currentQR() string {
	v := d.qr.Load()
	if v == nil {
		return ""
	}
	code, _ := v.(string)
	if !d.waiting.Load() {
		return ""
	}
	return code
}

func (d *Daemon) linked() bool {
	return d.client != nil && d.client.Store != nil && d.client.Store.ID != nil
}

func (d *Daemon) requireLinked() error {
	if !d.linked() {
		return fmt.Errorf("not linked yet. Finish pairing in the wp up terminal")
	}
	return nil
}

func historyAlt(chat types.JID, ids ...string) types.JID {
	for _, id := range ids {
		if id == "" {
			continue
		}
		parsed, err := types.ParseJID(id)
		if err != nil || !direct.Is(parsed) {
			continue
		}
		parsed = direct.Canonical(parsed)
		if parsed.String() != chat.String() {
			return parsed
		}
	}
	return types.EmptyJID
}

func messageAlt(info types.MessageSource) types.JID {
	if !info.SenderAlt.IsEmpty() && direct.Is(info.SenderAlt) {
		return direct.Canonical(info.SenderAlt)
	}
	if !info.RecipientAlt.IsEmpty() && direct.Is(info.RecipientAlt) {
		return direct.Canonical(info.RecipientAlt)
	}
	return types.EmptyJID
}

func jidString(j types.JID) string {
	if j.IsEmpty() {
		return ""
	}
	return j.String()
}

func ownJID(c *whatsmeow.Client) string {
	if c == nil || c.Store == nil || c.Store.ID == nil {
		return ""
	}
	return direct.Canonical(*c.Store.ID).String()
}

func splitSend(args []string, matches func(string) (int, error)) (query, text string, err error) {
	if len(args) < 2 {
		return "", "", fmt.Errorf("usage: wp send CHAT MESSAGE")
	}
	var ambiguous error
	for i := len(args) - 1; i >= 1; i-- {
		q := strings.Join(args[:i], " ")
		n, err := matches(q)
		if err != nil {
			return "", "", err
		}
		if n == 1 {
			return q, strings.Join(args[i:], " "), nil
		}
		if n > 1 && ambiguous == nil {
			ambiguous = fmt.Errorf("more than one chat matches %q", q)
		}
	}
	if ambiguous != nil {
		return "", "", ambiguous
	}
	return "", "", fmt.Errorf("no chat matching %q", args[0])
}

func fail(err error) ipc.Response {
	return ipc.Response{Error: err.Error()}
}

func ambiguous(matches []store.Chat) error {
	var b strings.Builder
	b.WriteString("more than one chat matches:")
	limit := len(matches)
	if limit > 8 {
		limit = 8
	}
	for i := 0; i < limit; i++ {
		fmt.Fprintf(&b, "\n  %s", matches[i].Label())
	}
	if len(matches) > limit {
		fmt.Fprintf(&b, "\n  and %d more", len(matches)-limit)
	}
	return errors.New(b.String())
}

func clamp(n, def, max int) int {
	if n <= 0 {
		return def
	}
	if n > max {
		return max
	}
	return n
}

func running(sock string) bool {
	conn, err := net.DialTimeout("unix", sock, 200*time.Millisecond)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

func digitsOnly(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func phoneDigits(q string) (string, bool) {
	var b strings.Builder
	for _, r := range q {
		switch {
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '+' || r == ' ' || r == '-' || r == '(' || r == ')' || r == '\t':
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

func formatCode(code string) string {
	code = strings.ReplaceAll(code, "-", "")
	if len(code) == 8 {
		return code[:4] + "-" + code[4:]
	}
	return code
}
