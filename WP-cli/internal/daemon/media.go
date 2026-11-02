package daemon

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.mau.fi/whatsmeow"

	"wp/internal/ipc"
	"wp/internal/msgtext"
	"wp/internal/paths"
	"wp/internal/store"
)

func (d *Daemon) openMedia(chat, id string) ipc.Response {
	if err := d.requireLinked(); err != nil {
		return fail(err)
	}
	if strings.TrimSpace(chat) == "" || strings.TrimSpace(id) == "" {
		return fail(fmt.Errorf("missing message"))
	}
	path, err := d.materialize(chat, id, false)
	if err != nil {
		return fail(err)
	}
	return ipc.Response{OK: true, Path: path}
}

func (d *Daemon) cacheLater(chat, id string) {
	go func() {
		select {
		case <-d.ctx.Done():
			return
		case d.mediaSem <- struct{}{}:
		}
		defer func() { <-d.mediaSem }()
		_, _ = d.materialize(chat, id, true)
	}()
}

func (d *Daemon) materialize(chat, id string, tell bool) (string, error) {
	msg, err := d.app.Media(chat, id)
	if err != nil {
		return "", err
	}
	if msg.FilePath != "" && msg.MediaState == "local" {
		if st, err := os.Stat(msg.FilePath); err == nil && !st.IsDir() {
			return msg.FilePath, nil
		}
	}
	if msg.Kind == "" {
		return "", fmt.Errorf("no media")
	}
	if msg.MediaRef == "" {
		d.markUnavailable(msg, tell)
		return "", fmt.Errorf("%s", msgtext.MediaLabel(msg.Kind, msg.FileName, msg.FileSize, "unavailable"))
	}
	ctx, cancel := context.WithTimeout(d.ctx, 3*time.Minute)
	defer cancel()
	data, err := d.download(ctx, msg.MediaRef)
	if err != nil {
		d.markUnavailable(msg, tell)
		return "", fmt.Errorf("%s", msgtext.MediaLabel(msg.Kind, msg.FileName, msg.FileSize, "unavailable"))
	}
	path, err := writeMedia(msg, data)
	if err != nil {
		return "", err
	}
	size := int64(len(data))
	updated, err := d.app.SetFile(msg.Chat, msg.ID, path, "local", size)
	if err != nil {
		return "", err
	}
	if tell {
		d.announce(updated)
	}
	return path, nil
}

func (d *Daemon) markUnavailable(msg store.Message, tell bool) {
	updated, err := d.app.SetFile(msg.Chat, msg.ID, "", "unavailable", 0)
	if err == nil && tell {
		d.announce(updated)
	}
}

func (d *Daemon) announce(m store.Message) {
	d.broadcast(ipc.Live{
		ID:     m.ID,
		Chat:   m.Chat,
		FromMe: m.FromMe,
		Text:   m.Preview(),
		At:     m.At,
		Kind:   m.Kind,
		Path:   m.FilePath,
	})
}

func (d *Daemon) download(ctx context.Context, raw string) ([]byte, error) {
	if d.client == nil {
		return nil, fmt.Errorf("session is not connected")
	}
	ref, err := msgtext.DecodeRef(raw)
	if err != nil {
		return nil, err
	}
	key, err := decodeKey(ref.Key)
	if err != nil {
		return nil, err
	}
	sha, err := decodeKey(ref.SHA)
	if err != nil {
		return nil, err
	}
	enc, err := decodeKey(ref.Enc)
	if err != nil {
		return nil, err
	}
	return d.client.DownloadMediaWithPath(ctx, ref.Path, enc, sha, key, whatsmeow.MediaType(ref.Type), ref.MMS, false)
}

func decodeKey(s string) ([]byte, error) {
	if s == "" {
		return nil, nil
	}
	return base64.StdEncoding.DecodeString(s)
}

func writeMedia(m store.Message, data []byte) (string, error) {
	root, err := paths.Dir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(root, "media", safeName(m.Chat))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(dir, safeName(m.ID)+extension(m.Mime, m.FileName))
	rel, err := filepath.Rel(filepath.Join(root, "media"), path)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("refusing to write outside the media directory")
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", err
	}
	return path, nil
}

func safeName(s string) string {
	s = strings.ReplaceAll(s, string(os.PathSeparator), "_")
	s = strings.ReplaceAll(s, "/", "_")
	s = strings.ReplaceAll(s, "\\", "_")
	s = strings.ReplaceAll(s, "..", "_")
	if s == "" || s == "." {
		return "file"
	}
	return s
}

func extension(mime, name string) string {
	if ext := strings.ToLower(filepath.Ext(name)); ext != "" && len(ext) <= 8 && !strings.Contains(ext, string(os.PathSeparator)) {
		return ext
	}
	mime = strings.ToLower(strings.TrimSpace(mime))
	if i := strings.IndexByte(mime, ';'); i >= 0 {
		mime = strings.TrimSpace(mime[:i])
	}
	switch mime {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	case "audio/ogg", "audio/opus":
		return ".ogg"
	case "audio/mpeg":
		return ".mp3"
	case "video/mp4":
		return ".mp4"
	case "video/webm":
		return ".webm"
	case "application/pdf":
		return ".pdf"
	default:
		return ".bin"
	}
}
