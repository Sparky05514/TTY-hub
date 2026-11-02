package tui

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBinaryStaleComparesInode(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a")
	b := filepath.Join(dir, "b")
	if err := os.WriteFile(a, []byte("a"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte("b"), 0o600); err != nil {
		t.Fatal(err)
	}
	ia, err := os.Stat(a)
	if err != nil {
		t.Fatal(err)
	}
	ib, err := os.Stat(b)
	if err != nil {
		t.Fatal(err)
	}
	if !sameInode(ia, ia) {
		t.Fatal("a file should match itself")
	}
	if sameInode(ia, ib) {
		t.Fatal("two files should differ")
	}
	if binaryStale(a, ia, ia) {
		t.Fatal("the same inode is current")
	}
	if !binaryStale(a, ia, ib) {
		t.Fatal("a different inode is stale")
	}
	if !binaryStale(a+" (deleted)", nil, ia) {
		t.Fatal("a deleted binary is stale")
	}
	if binaryStale(a, nil, ia) {
		t.Fatal("an unreadable peer should be left alone")
	}
}
