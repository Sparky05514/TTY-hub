package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClassifyFile(t *testing.T) {
	kind, _, _ := classifyFile("photo.jpeg", nil)
	if kind != "image" {
		t.Fatalf("jpeg = %s", kind)
	}
	kind, _, _ = classifyFile("clip.mp4", nil)
	if kind != "video" {
		t.Fatalf("mp4 = %s", kind)
	}
	kind, mime, _ := classifyFile("notes.pdf", []byte("%PDF-1.4"))
	if kind != "file" || mime != "application/pdf" {
		t.Fatalf("pdf = %s %s", kind, mime)
	}
	kind, _, _ = classifyFile("blob.bin", []byte{0xff, 0xd8, 0xff, 0xe0})
	if kind != "image" {
		t.Fatalf("sniffed jpeg = %s", kind)
	}
}

func TestReadOutgoingRejects(t *testing.T) {
	if _, _, err := readOutgoing("https://example.com/a.jpg"); err == nil {
		t.Fatal("url should fail")
	}
	dir := t.TempDir()
	if _, _, err := readOutgoing(dir); err == nil {
		t.Fatal("directory should fail")
	}
	big := filepath.Join(dir, "big.bin")
	f, err := os.Create(big)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(maxUpload + 1); err != nil {
		t.Fatal(err)
	}
	f.Close()
	_, _, err = readOutgoing(big)
	if err == nil || !strings.Contains(err.Error(), "64 MiB") {
		t.Fatalf("oversize = %v", err)
	}
}
