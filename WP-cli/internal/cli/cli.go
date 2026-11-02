package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"wp/internal/daemon"
	"wp/internal/ipc"
	"wp/internal/paths"
	"wp/internal/store"
	"wp/internal/tui"
)

func Main(args []string) int {
	if len(args) == 0 {
		return tui.Run()
	}
	switch args[0] {
	case "help", "-h", "--help":
		usage(os.Stdout)
		return 0
	case "up":
		return cmdUp(args[1:])
	case "status":
		return cmdStatus(args[1:])
	case "chats":
		return cmdChats(args[1:])
	case "read":
		return cmdRead(args[1:])
	case "send":
		return cmdSend(args[1:])
	case "react":
		return cmdReact(args[1:])
	case "find":
		return cmdFind(args[1:])
	case "watch":
		return cmdWatch(args[1:])
	case "logout":
		return cmdLogout(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", args[0])
		usage(os.Stderr)
		return 2
	}
}

func cmdUp(args []string) int {
	fs := flag.NewFlagSet("up", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	phone := fs.String("phone", "", "country code and number, for a pairing code instead of a QR")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		usage(os.Stderr)
		return 2
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := daemon.Run(ctx, *phone); err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintf(os.Stderr, "wp: %v\n", err)
		return 1
	}
	return 0
}

func cmdStatus(args []string) int {
	_, asJSON, rest, code := withFlags("status", args, 0)
	if code != 0 {
		return code
	}
	if len(rest) != 0 {
		usage(os.Stderr)
		return 2
	}
	resp, err := roundTrip(ipc.Request{Cmd: "status"})
	if err != nil {
		return callErr(err, asJSON)
	}
	if asJSON {
		return printJSON(resp)
	}
	if !resp.OK {
		fmt.Fprintln(os.Stderr, resp.Error)
		return 1
	}
	st := resp.Status
	if st.Linked {
		fmt.Printf("linked     %s\n", st.JID)
	} else {
		fmt.Printf("linked     no\n")
	}
	if st.PushName != "" {
		fmt.Printf("push name  %s\n", st.PushName)
	}
	fmt.Printf("connected  %s\n", yesNo(st.Connected))
	if st.Pairing {
		fmt.Printf("pairing    scan the QR code in the wp up terminal\n")
	}
	fmt.Printf("chats      %d\n", st.Chats)
	return 0
}

func cmdChats(args []string) int {
	n, asJSON, rest, code := withFlags("chats", args, 30)
	if code != 0 {
		return code
	}
	if len(rest) != 0 {
		usage(os.Stderr)
		return 2
	}
	resp, err := roundTrip(ipc.Request{Cmd: "chats", Limit: n})
	if err != nil {
		return callErr(err, asJSON)
	}
	if asJSON {
		return printJSON(resp)
	}
	if !resp.OK {
		fmt.Fprintln(os.Stderr, resp.Error)
		return 1
	}
	if len(resp.Chats) == 0 {
		fmt.Println("no direct chats yet")
		return 0
	}
	for _, c := range resp.Chats {
		preview := oneLine(c.LastText, 48)
		if c.Unread > 0 {
			preview = fmt.Sprintf("(%d) %s", c.Unread, preview)
		}
		fmt.Printf("%-12s  %-22s  %s\n", formatTime(c.LastAt), trunc(c.Label(), 22), preview)
	}
	return 0
}

func cmdRead(args []string) int {
	n, asJSON, rest, code := withFlags("read", args, 50)
	if code != 0 {
		return code
	}
	if len(rest) == 0 {
		usage(os.Stderr)
		return 2
	}
	resp, err := roundTrip(ipc.Request{
		Cmd:   "read",
		Query: strings.Join(rest, " "),
		Limit: n,
	})
	if err != nil {
		return callErr(err, asJSON)
	}
	if asJSON {
		return printJSON(resp)
	}
	if !resp.OK {
		fmt.Fprintln(os.Stderr, resp.Error)
		return 1
	}
	if resp.Chat == nil {
		fmt.Fprintln(os.Stderr, "wp: empty response")
		return 1
	}
	name := resp.Chat.Label()
	if len(resp.Messages) == 0 {
		fmt.Printf("no messages with %s\n", name)
		return 0
	}
	for _, m := range resp.Messages {
		who := name
		if m.FromMe {
			who = "5514"
		}
		if m.ReplyText != "" {
			fmt.Printf("%-12s  %-12s  %s\n", "", "", "reply: "+oneLine(m.ReplyText, 60))
		}
		line := m.Text
		if m.FromMe && m.Receipt != "" {
			line += "  " + m.Receipt
		}
		fmt.Printf("%-12s  %-12s  %s\n", formatTime(m.At), trunc(who, 12), line)
		if m.Caption != "" {
			fmt.Printf("%-12s  %-12s  %s\n", "", "", m.Caption)
		}
		if s := formatReactions(m.Reactions); s != "" {
			fmt.Printf("%-12s  %-12s  %s\n", "", "", s)
		}
	}
	return 0
}

func cmdReact(args []string) int {
	fs := flag.NewFlagSet("react", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	clear := fs.Bool("clear", false, "remove your reaction")
	asJSON := fs.Bool("json", false, "print JSON")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	rest := fs.Args()
	if len(rest) < 2 {
		usage(os.Stderr)
		return 2
	}
	chat := rest[0]
	msgID := rest[1]
	emoji := ""
	if !*clear {
		if len(rest) < 3 {
			usage(os.Stderr)
			return 2
		}
		emoji = strings.Join(rest[2:], " ")
	}
	resp, err := roundTrip(ipc.Request{Cmd: "react", Query: chat, Text: msgID, Reply: emoji})
	if err != nil {
		return callErr(err, *asJSON)
	}
	if *asJSON {
		return printJSON(resp)
	}
	if !resp.OK {
		fmt.Fprintln(os.Stderr, resp.Error)
		return 1
	}
	if emoji == "" {
		fmt.Println("cleared")
		return 0
	}
	fmt.Printf("reacted %s\n", emoji)
	return 0
}

func cmdSend(args []string) int {
	var file string
	filtered := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		if args[i] == "--file" {
			if i+1 >= len(args) {
				usage(os.Stderr)
				return 2
			}
			file = args[i+1]
			i++
			continue
		}
		filtered = append(filtered, args[i])
	}
	fs := flag.NewFlagSet("send", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	asJSON := fs.Bool("json", false, "print JSON")
	if err := fs.Parse(filtered); err != nil {
		return 2
	}
	if file != "" {
		if fs.NArg() < 1 {
			usage(os.Stderr)
			return 2
		}
		resp, err := roundTrip(ipc.Request{Cmd: "send", Args: fs.Args(), File: file})
		if err != nil {
			return callErr(err, *asJSON)
		}
		if *asJSON {
			return printJSON(resp)
		}
		if !resp.OK {
			fmt.Fprintln(os.Stderr, resp.Error)
			return 1
		}
		fmt.Println("sent")
		return 0
	}
	if fs.NArg() < 2 {
		usage(os.Stderr)
		return 2
	}
	resp, err := roundTrip(ipc.Request{Cmd: "send", Args: fs.Args()})
	if err != nil {
		return callErr(err, *asJSON)
	}
	if *asJSON {
		return printJSON(resp)
	}
	if !resp.OK {
		fmt.Fprintln(os.Stderr, resp.Error)
		return 1
	}
	label := fs.Arg(0)
	if resp.Chat != nil {
		label = resp.Chat.Label()
	}
	fmt.Printf("sent to %s\n", label)
	return 0
}

func cmdFind(args []string) int {
	n, asJSON, rest, code := withFlags("find", args, 50)
	if code != 0 {
		return code
	}
	if len(rest) == 0 {
		usage(os.Stderr)
		return 2
	}
	resp, err := roundTrip(ipc.Request{
		Cmd:   "find",
		Query: strings.Join(rest, " "),
		Limit: n,
	})
	if err != nil {
		return callErr(err, asJSON)
	}
	if asJSON {
		return printJSON(resp)
	}
	if !resp.OK {
		fmt.Fprintln(os.Stderr, resp.Error)
		return 1
	}
	if len(resp.Hits) == 0 {
		fmt.Println("no matches")
		return 0
	}
	for _, h := range resp.Hits {
		who := h.ChatName
		if h.FromMe {
			who = "me → " + h.ChatName
		}
		line := h.Text
		if h.Caption != "" {
			line += " — " + h.Caption
		}
		fmt.Printf("%-12s  %-16s  %s\n", formatTime(h.At), trunc(who, 16), oneLine(line, 60))
	}
	return 0
}

func cmdWatch(args []string) int {
	_, asJSON, rest, code := withFlags("watch", args, 0)
	if code != 0 {
		return code
	}
	if len(rest) != 0 {
		usage(os.Stderr)
		return 2
	}
	return stream(ipc.Request{Cmd: "watch"}, asJSON, func(ev ipc.Live) error {
		if asJSON {
			enc := json.NewEncoder(os.Stdout)
			enc.SetEscapeHTML(false)
			return enc.Encode(ev)
		}
		who := ev.Name
		if ev.FromMe {
			who = "me → " + ev.Name
		}
		fmt.Printf("%-12s  %-16s  %s\n", formatTime(ev.At), trunc(who, 16), ev.Text)
		return nil
	})
}

func cmdLogout(args []string) int {
	fs := flag.NewFlagSet("logout", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		usage(os.Stderr)
		return 2
	}
	resp, err := roundTrip(ipc.Request{Cmd: "logout"})
	if err != nil {
		return callErr(err, false)
	}
	if !resp.OK {
		fmt.Fprintln(os.Stderr, resp.Error)
		return 1
	}
	fmt.Println("unlinked")
	return 0
}

func withFlags(name string, args []string, defaultLimit int) (n int, asJSON bool, rest []string, code int) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	np := fs.Int("n", defaultLimit, "maximum rows")
	js := fs.Bool("json", false, "print JSON")
	if err := fs.Parse(args); err != nil {
		return 0, false, nil, 2
	}
	return *np, *js, fs.Args(), 0
}

func roundTrip(req ipc.Request) (ipc.Response, error) {
	conn, err := dial()
	if err != nil {
		return ipc.Response{}, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(35 * time.Second))
	enc := json.NewEncoder(conn)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(req); err != nil {
		return ipc.Response{}, err
	}
	var resp ipc.Response
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return ipc.Response{}, err
	}
	return resp, nil
}

func stream(req ipc.Request, asJSON bool, fn func(ipc.Live) error) int {
	conn, err := dial()
	if err != nil {
		return callErr(err, asJSON)
	}
	defer conn.Close()
	enc := json.NewEncoder(conn)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(req); err != nil {
		return callErr(err, asJSON)
	}
	dec := json.NewDecoder(conn)
	var resp ipc.Response
	if err := dec.Decode(&resp); err != nil {
		return callErr(err, asJSON)
	}
	if !resp.OK {
		if asJSON {
			return printJSON(resp)
		}
		fmt.Fprintln(os.Stderr, resp.Error)
		return 1
	}
	for {
		var ev ipc.Live
		if err := dec.Decode(&ev); err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
				return 0
			}
			fmt.Fprintf(os.Stderr, "wp: %v\n", err)
			return 1
		}
		if err := fn(ev); err != nil {
			fmt.Fprintf(os.Stderr, "wp: %v\n", err)
			return 1
		}
	}
}

func dial() (net.Conn, error) {
	sock, err := paths.Sock()
	if err != nil {
		return nil, err
	}
	conn, err := net.DialTimeout("unix", sock, 2*time.Second)
	if err != nil {
		return nil, fmt.Errorf("wp is not running. Start it with: wp up")
	}
	return conn, nil
}

func callErr(err error, asJSON bool) int {
	if asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetEscapeHTML(false)
		_ = enc.Encode(ipc.Response{Error: err.Error()})
		return 1
	}
	fmt.Fprintf(os.Stderr, "wp: %v\n", err)
	return 1
}

func printJSON(v any) int {
	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		fmt.Fprintf(os.Stderr, "wp: %v\n", err)
		return 1
	}
	if resp, ok := v.(ipc.Response); ok && !resp.OK {
		return 1
	}
	return 0
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
	return local.Format("2006-01-02")
}

func trunc(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n < 1 {
		return ""
	}
	return string(r[:n-1]) + "…"
}

func oneLine(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	return trunc(s, n)
}

func yesNo(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}

func formatReactions(rs []store.Reaction) string {
	if len(rs) == 0 {
		return ""
	}
	type agg struct {
		emoji string
		n     int
		mine  bool
	}
	order := make([]string, 0, len(rs))
	by := map[string]*agg{}
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
		}
	}
	parts := make([]string, 0, len(order))
	for _, e := range order {
		a := by[e]
		s := a.emoji
		if a.n > 1 {
			s = fmt.Sprintf("%s×%d", a.emoji, a.n)
		}
		if a.mine {
			s += "*"
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, "  ")
}

func usage(w io.Writer) {
	fmt.Fprintf(w, `wp is a direct-message client for one linked WhatsApp account.

  wp                       open the full-screen client
  wp up [--phone NUMBER]   keep the session connected without the interface
  wp status
  wp chats [-n N] [--json]
  wp read CHAT [-n N] [--json]
  wp send CHAT MESSAGE...
  wp send CHAT --file PATH [CAPTION...]
  wp react CHAT MSGID EMOJI
  wp react CHAT MSGID --clear
  wp find TEXT [-n N] [--json]
  wp watch [--json]
  wp logout

CHAT is a name, a phone number with country code, or a WhatsApp id.
wp send treats the longest matching name as the chat, so multi-word names
do not need quotes.

The session is stored in ~/.local/share/wp. Channels, status broadcasts,
and bots are ignored. WhatsApp can unlink or ban a number that uses an unofficial client.

`)
}
