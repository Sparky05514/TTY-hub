package msgtext

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"go.mau.fi/whatsmeow/proto/waE2E"
)

// Attachment is a file carried by a direct message.
// Ref is empty when the file can no longer be fetched.
type Attachment struct {
	Kind     string
	Caption  string
	FileName string
	Mime     string
	Size     int64
	Eager    bool
	ViewOnce bool
	Ref      string
}

// State is "remote" while a fetch is possible and "unavailable" when it is not.
func (a Attachment) State() string {
	if a.Ref == "" {
		return "unavailable"
	}
	return "remote"
}

func (a Attachment) Label() string {
	return MediaLabel(a.Kind, a.FileName, a.Size, a.State())
}

// MediaLabel is the thread line for a file.
func MediaLabel(kind, name string, size int64, state string) string {
	switch kind {
	case "voice":
		if state == "unavailable" {
			return "voice message · unavailable"
		}
		return "voice message"
	case "file":
		base := "file"
		if strings.TrimSpace(name) != "" {
			base = "file · " + strings.TrimSpace(name)
		}
		return withSize(base, size, state)
	default:
		if kind == "" {
			kind = "file"
		}
		return withSize(kind, size, state)
	}
}

func withSize(base string, size int64, state string) string {
	if size > 0 {
		base = fmt.Sprintf("%s · %s", base, FormatSize(size))
	}
	if state == "unavailable" {
		return base + " · unavailable"
	}
	return base
}

// FormatSize renders a byte count the way the thread shows it.
func FormatSize(n int64) string {
	if n < 0 {
		n = 0
	}
	const kb = 1024
	const mb = 1024 * 1024
	if n < kb {
		return fmt.Sprintf("%d B", n)
	}
	if n < mb {
		v := float64(n) / kb
		if v < 10 {
			return fmt.Sprintf("%.1f KB", v)
		}
		return fmt.Sprintf("%d KB", int(v+0.5))
	}
	v := float64(n) / mb
	if v < 10 {
		return fmt.Sprintf("%.1f MB", v)
	}
	return fmt.Sprintf("%d MB", int(v+0.5))
}

// Quote is the message this one answers.
type Quote struct {
	ID     string
	Sender string
	Text   string
}

// Inspect returns the text to store, a file when there is one, and a quote when this message answers another.
// once is set when the view-once wrapper was already removed by the caller.
func Inspect(m *waE2E.Message, once bool) (string, *Attachment, *Quote) {
	if m == nil {
		return "", nil, nil
	}
	if m.GetViewOnceMessage() != nil || m.GetViewOnceMessageV2() != nil || m.GetViewOnceMessageV2Extension() != nil {
		once = true
	}
	m = unwrap(m)
	if m == nil || m.GetProtocolMessage() != nil || m.GetReactionMessage() != nil {
		return "", nil, nil
	}
	if m.GetImageMessage().GetViewOnce() || m.GetVideoMessage().GetViewOnce() || m.GetPtvMessage().GetViewOnce() {
		once = true
	}
	quote := quoteOf(m)
	if once {
		if att, ok := mediaOf(m); ok {
			att.Eager = false
			att.ViewOnce = true
			return viewOnceLabel(att), att, quote
		}
		return "view once", nil, quote
	}
	if att, ok := mediaOf(m); ok {
		return att.Label(), att, quote
	}
	return content(m), nil, quote
}

func viewOnceLabel(att *Attachment) string {
	word := att.Kind
	switch att.Kind {
	case "image":
		word = "photo"
	case "video":
		word = "video"
	}
	if word == "" {
		word = "photo"
	}
	return withSize("view once "+word, att.Size, att.State())
}

func quoteOf(m *waE2E.Message) *Quote {
	info := contextInfo(m)
	if info == nil || info.GetStanzaID() == "" {
		return nil
	}
	text := quoteSnippet(info.GetQuotedMessage())
	if text == "" {
		text = "message"
	}
	return &Quote{ID: info.GetStanzaID(), Sender: info.GetParticipant(), Text: Snip(text, 80)}
}

func contextInfo(m *waE2E.Message) *waE2E.ContextInfo {
	if m == nil {
		return nil
	}
	if c := m.GetExtendedTextMessage().GetContextInfo(); c != nil {
		return c
	}
	if c := m.GetImageMessage().GetContextInfo(); c != nil {
		return c
	}
	if c := m.GetVideoMessage().GetContextInfo(); c != nil {
		return c
	}
	if c := m.GetPtvMessage().GetContextInfo(); c != nil {
		return c
	}
	if c := m.GetDocumentMessage().GetContextInfo(); c != nil {
		return c
	}
	if c := m.GetAudioMessage().GetContextInfo(); c != nil {
		return c
	}
	if c := m.GetStickerMessage().GetContextInfo(); c != nil {
		return c
	}
	return nil
}

func quoteSnippet(m *waE2E.Message) string {
	if m == nil {
		return ""
	}
	m = unwrap(m)
	if m == nil {
		return ""
	}
	if att, ok := mediaOf(m); ok {
		return oneLine(MediaLabel(att.Kind, att.FileName, att.Size, ""))
	}
	return oneLine(content(m))
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// Snip shortens s to n runes.
func Snip(s string, n int) string {
	r := []rune(s)
	if n < 1 || len(r) <= n {
		return s
	}
	if n == 1 {
		return "…"
	}
	return string(r[:n-1]) + "…"
}

func mediaOf(m *waE2E.Message) (*Attachment, bool) {
	if im := m.GetImageMessage(); im != nil {
		return fileAtt("image", im.GetCaption(), "", im.GetMimetype(), im.GetFileLength(), true, im, "image", imageKeys), true
	}
	if vm := m.GetVideoMessage(); vm != nil {
		return fileAtt("video", vm.GetCaption(), "", vm.GetMimetype(), vm.GetFileLength(), false, vm, "video", videoKeys), true
	}
	if vm := m.GetPtvMessage(); vm != nil {
		return fileAtt("video", vm.GetCaption(), "", vm.GetMimetype(), vm.GetFileLength(), false, vm, "video", videoKeys), true
	}
	if dm := m.GetDocumentMessage(); dm != nil {
		return fileAtt("file", dm.GetCaption(), dm.GetFileName(), dm.GetMimetype(), dm.GetFileLength(), false, dm, "document", documentKeys), true
	}
	if a := m.GetAudioMessage(); a != nil {
		if a.GetPTT() {
			return fileAtt("voice", "", "", a.GetMimetype(), a.GetFileLength(), true, a, "audio", audioKeys), true
		}
		return fileAtt("audio", "", "", a.GetMimetype(), a.GetFileLength(), false, a, "audio", audioKeys), true
	}
	if s := m.GetStickerMessage(); s != nil {
		return fileAtt("sticker", "", "", s.GetMimetype(), s.GetFileLength(), false, s, "image", imageKeys), true
	}
	return nil, false
}

const (
	imageKeys    = "WhatsApp Image Keys"
	videoKeys    = "WhatsApp Video Keys"
	audioKeys    = "WhatsApp Audio Keys"
	documentKeys = "WhatsApp Document Keys"
)

type downloadable interface {
	GetDirectPath() string
	GetMediaKey() []byte
	GetFileSHA256() []byte
	GetFileEncSHA256() []byte
}

func fileAtt(kind, caption, name, mime string, size uint64, eager bool, msg downloadable, mms, mediaType string) *Attachment {
	att := &Attachment{
		Kind:     kind,
		Caption:  strings.TrimSpace(caption),
		FileName: strings.TrimSpace(name),
		Mime:     mime,
		Size:     int64(size),
		Eager:    eager,
	}
	if msg.GetDirectPath() != "" {
		att.Ref = encodeRef(MediaRef{
			Path: msg.GetDirectPath(),
			Key:  b64(msg.GetMediaKey()),
			SHA:  b64(msg.GetFileSHA256()),
			Enc:  b64(msg.GetFileEncSHA256()),
			Type: mediaType,
			MMS:  mms,
		})
	}
	return att
}

func b64(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	return base64.StdEncoding.EncodeToString(b)
}

// MediaRef is the handle needed to download a file later.
type MediaRef struct {
	Path string `json:"path"`
	Key  string `json:"key,omitempty"`
	SHA  string `json:"sha,omitempty"`
	Enc  string `json:"enc,omitempty"`
	Type string `json:"type"`
	MMS  string `json:"mms"`
}

func encodeRef(r MediaRef) string {
	b, err := json.Marshal(r)
	if err != nil {
		return ""
	}
	return string(b)
}

// DecodeRef unpacks a stored download handle.
func DecodeRef(s string) (MediaRef, error) {
	var r MediaRef
	if strings.TrimSpace(s) == "" {
		return r, fmt.Errorf("missing media")
	}
	if err := json.Unmarshal([]byte(s), &r); err != nil {
		return r, err
	}
	if r.Path == "" || r.Type == "" || r.MMS == "" {
		return r, fmt.Errorf("missing media")
	}
	return r, nil
}
