package dbfile

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"go.mau.fi/whatsmeow/store/sqlstore"

	_ "modernc.org/sqlite"
)

func TestSessionSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.db")
	container, err := sqlstore.New(context.Background(), "sqlite", DSN(path), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { container.Close() })
	device, err := container.GetFirstDevice(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if device == nil {
		t.Fatal("missing device")
	}

	db, err := sql.Open("sqlite", DSN(path))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var fk int
	if err := db.QueryRow("PRAGMA foreign_keys").Scan(&fk); err != nil {
		t.Fatal(err)
	}
	if fk != 1 {
		t.Fatalf("foreign_keys = %d", fk)
	}
}
