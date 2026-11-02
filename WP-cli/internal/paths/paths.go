package paths

import (
	"os"
	"path/filepath"
)

func Dir() (string, error) {
	if v := os.Getenv("XDG_DATA_HOME"); v != "" {
		return filepath.Join(v, "wp"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share", "wp"), nil
}

func Sock() (string, error) {
	if v := os.Getenv("XDG_RUNTIME_DIR"); v != "" {
		return filepath.Join(v, "wp.sock"), nil
	}
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "wp.sock"), nil
}
