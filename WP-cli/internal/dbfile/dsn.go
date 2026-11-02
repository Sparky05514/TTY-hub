package dbfile

import (
	"net/url"
	"path/filepath"
)

// DSN is a modernc.org/sqlite connection string with foreign keys and WAL.
func DSN(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	u := url.URL{
		Scheme:   "file",
		Path:     filepath.ToSlash(abs),
		RawQuery: "_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)",
	}
	return u.String()
}
