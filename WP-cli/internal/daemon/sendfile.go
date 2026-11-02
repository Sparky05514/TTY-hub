package daemon

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"google.golang.org/protobuf/proto"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"

	"wp/internal/ipc"
	"wp/internal/msgtext"
	"wp/internal/store"
)

const maxUpload = 64 << 20

func (d *Daemon) sendFile(req ipc.Request) ipc.Response {
	if err := d.requireLinked(); err != nil {
		return fail(err)
	}
	if !d.client.IsConnected() {
		return fail(fmt.Errorf("not connected"))
	}
	query, caption, err := fileTarget(req, d.matchCount)
	if err != nil {
		return fail(err)
	}
	data, name, err := readOutgoing(req.File)
	if err != nil {
		return fail(err)
	}
	kind, mime, app := classifyFile(req.File, data)
	chat, err := d.resolve(query)
	if err != nil {
		return fail(err)
	}
	jid, err := types.ParseJID(chat.JID)
	if err != nil {
		return fail(err)
	}
	quoted, err := d.quoted(chat.JID, req.Reply)
	if err != nil {
		return fail(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	up, err := d.client.Upload(ctx, data, app)
	if err != nil {
		return fail(err)
	}
	var info *waE2E.ContextInfo
	if quoted != nil {
		info = quoteContext(quoted)
	}
	resp, err := d.client.SendMessage(ctx, jid, mediaPayload(kind, mime, name, caption, up, info))
	if err != nil {
		return fail(err)
	}
	at := resp.Timestamp
	if at.IsZero() {
		at = time.Now()
	}
	replyTo, replyText := replyFields(quoted)
	msg := store.Message{
		ID:         resp.ID,
		Chat:       chat.JID,
		Sender:     ownJID(d.client),
		FromMe:     true,
		Text:       msgtext.MediaLabel(kind, name, int64(len(data)), "local"),
		Caption:    strings.TrimSpace(caption),
		Kind:       kind,
		Mime:       mime,
		FileName:   name,
		FileSize:   int64(len(data)),
		MediaState: "local",
		ReplyTo:    replyTo,
		ReplyText:  replyText,
		Receipt:    "sent",
		At:         at,
	}
	path, werr := writeMedia(msg, data)
	if werr != nil {
		msg.MediaState = "remote"
		msg.Text = msgtext.MediaLabel(kind, name, msg.FileSize, "")
		msg.MediaRef = uploadRef(up, app, kind)
	} else {
		msg.FilePath = path
	}
	in := store.Incoming{
		Message:  msg,
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
		d.broadcast(ipc.Live{
			ID: resp.ID, Chat: saved.JID, Name: saved.Label(), FromMe: true,
			Text: in.Preview(), At: at, Kind: in.Kind, Path: in.FilePath,
		})
	}
	if werr != nil {
		return fail(werr)
	}
	out := in.Message
	return ipc.Response{OK: true, Chat: &saved, Sent: &out}
}

func fileTarget(req ipc.Request, matches func(string) (int, error)) (string, string, error) {
	if req.Query != "" {
		caption := req.Text
		if caption == "" && len(req.Args) > 0 {
			caption = strings.Join(req.Args, " ")
		}
		return req.Query, caption, nil
	}
	return splitFileArgs(req.Args, matches)
}

func splitFileArgs(args []string, matches func(string) (int, error)) (string, string, error) {
	if len(args) == 0 {
		return "", "", fmt.Errorf("missing chat")
	}
	if len(args) == 1 {
		return args[0], "", nil
	}
	full := strings.Join(args, " ")
	n, err := matches(full)
	if err != nil {
		return "", "", err
	}
	if n == 1 {
		return full, "", nil
	}
	return splitSend(args, matches)
}

func readOutgoing(path string) ([]byte, string, error) {
	path = strings.TrimSpace(path)
	if path == "" || strings.Contains(path, "://") {
		return nil, "", fmt.Errorf("file must be on this machine")
	}
	path = filepath.Clean(path)
	st, err := os.Stat(path)
	if err != nil {
		return nil, "", err
	}
	if st.IsDir() || !st.Mode().IsRegular() {
		return nil, "", fmt.Errorf("file must be on this machine")
	}
	if st.Size() > maxUpload {
		return nil, "", fmt.Errorf("file is larger than 64 MiB")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", err
	}
	if len(data) > maxUpload {
		return nil, "", fmt.Errorf("file is larger than 64 MiB")
	}
	return data, filepath.Base(path), nil
}

func classifyFile(path string, data []byte) (kind, mime string, app whatsmeow.MediaType) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".jpg", ".jpeg":
		return "image", "image/jpeg", whatsmeow.MediaImage
	case ".png":
		return "image", "image/png", whatsmeow.MediaImage
	case ".gif":
		return "image", "image/gif", whatsmeow.MediaImage
	case ".webp":
		return "image", "image/webp", whatsmeow.MediaImage
	case ".mp4":
		return "video", "video/mp4", whatsmeow.MediaVideo
	case ".webm":
		return "video", "video/webm", whatsmeow.MediaVideo
	case ".mov":
		return "video", "video/quicktime", whatsmeow.MediaVideo
	case ".mp3":
		return "audio", "audio/mpeg", whatsmeow.MediaAudio
	case ".ogg", ".opus":
		return "audio", "audio/ogg", whatsmeow.MediaAudio
	case ".m4a":
		return "audio", "audio/mp4", whatsmeow.MediaAudio
	case ".pdf":
		return "file", "application/pdf", whatsmeow.MediaDocument
	}
	sniffed := http.DetectContentType(data)
	switch {
	case strings.HasPrefix(sniffed, "image/"):
		return "image", sniffed, whatsmeow.MediaImage
	case strings.HasPrefix(sniffed, "video/"):
		return "video", sniffed, whatsmeow.MediaVideo
	case strings.HasPrefix(sniffed, "audio/"):
		return "audio", sniffed, whatsmeow.MediaAudio
	default:
		if sniffed == "" {
			sniffed = "application/octet-stream"
		}
		return "file", sniffed, whatsmeow.MediaDocument
	}
}

func mediaPayload(kind, mime, name, caption string, up whatsmeow.UploadResponse, info *waE2E.ContextInfo) *waE2E.Message {
	caption = strings.TrimSpace(caption)
	switch kind {
	case "image":
		img := &waE2E.ImageMessage{
			Mimetype:      proto.String(mime),
			URL:           proto.String(up.URL),
			DirectPath:    proto.String(up.DirectPath),
			MediaKey:      up.MediaKey,
			FileEncSHA256: up.FileEncSHA256,
			FileSHA256:    up.FileSHA256,
			FileLength:    proto.Uint64(up.FileLength),
			ContextInfo:   info,
		}
		if caption != "" {
			img.Caption = proto.String(caption)
		}
		return &waE2E.Message{ImageMessage: img}
	case "video":
		vid := &waE2E.VideoMessage{
			Mimetype:         proto.String(mime),
			URL:              proto.String(up.URL),
			DirectPath:       proto.String(up.DirectPath),
			MediaKey:         up.MediaKey,
			FileEncSHA256:    up.FileEncSHA256,
			FileSHA256:       up.FileSHA256,
			FileLength:       proto.Uint64(up.FileLength),
			StreamingSidecar: up.StreamingSidecar,
			ContextInfo:      info,
		}
		if caption != "" {
			vid.Caption = proto.String(caption)
		}
		return &waE2E.Message{VideoMessage: vid}
	case "audio":
		return &waE2E.Message{AudioMessage: &waE2E.AudioMessage{
			Mimetype:      proto.String(mime),
			URL:           proto.String(up.URL),
			DirectPath:    proto.String(up.DirectPath),
			MediaKey:      up.MediaKey,
			FileEncSHA256: up.FileEncSHA256,
			FileSHA256:    up.FileSHA256,
			FileLength:    proto.Uint64(up.FileLength),
			PTT:           proto.Bool(false),
			ContextInfo:   info,
		}}
	default:
		doc := &waE2E.DocumentMessage{
			FileName:      proto.String(name),
			Mimetype:      proto.String(mime),
			URL:           proto.String(up.URL),
			DirectPath:    proto.String(up.DirectPath),
			MediaKey:      up.MediaKey,
			FileEncSHA256: up.FileEncSHA256,
			FileSHA256:    up.FileSHA256,
			FileLength:    proto.Uint64(up.FileLength),
			ContextInfo:   info,
		}
		if caption != "" {
			doc.Caption = proto.String(caption)
		}
		return &waE2E.Message{DocumentMessage: doc}
	}
}

func uploadRef(up whatsmeow.UploadResponse, app whatsmeow.MediaType, kind string) string {
	mms := "document"
	switch kind {
	case "image":
		mms = "image"
	case "video":
		mms = "video"
	case "audio":
		mms = "audio"
	}
	raw, err := json.Marshal(msgtext.MediaRef{
		Path: up.DirectPath,
		Key:  b64(up.MediaKey),
		SHA:  b64(up.FileSHA256),
		Enc:  b64(up.FileEncSHA256),
		Type: string(app),
		MMS:  mms,
	})
	if err != nil {
		return ""
	}
	return string(raw)
}

func b64(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	return base64.StdEncoding.EncodeToString(b)
}
