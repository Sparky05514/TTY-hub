package tui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"

	"wp/internal/ipc"
	"wp/internal/paths"
)

func call(req ipc.Request) (ipc.Response, error) {
	return callWait(req, 35*time.Second)
}

func callWait(req ipc.Request, wait time.Duration) (ipc.Response, error) {
	conn, err := dial()
	if err != nil {
		return ipc.Response{}, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(wait))
	enc := json.NewEncoder(conn)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(req); err != nil {
		return ipc.Response{}, err
	}
	var resp ipc.Response
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return ipc.Response{}, err
	}
	if !resp.OK {
		if resp.Error == "" {
			resp.Error = "request failed"
		}
		return resp, fmt.Errorf("%s", resp.Error)
	}
	return resp, nil
}

func notify(title, body string) {
	bin, err := exec.LookPath("notify-send")
	if err != nil {
		return
	}
	cmd := exec.Command(bin, "-a", "wp", title, oneLine(body))
	if err := cmd.Start(); err != nil {
		return
	}
	go func() { _ = cmd.Wait() }()
}

func clipboardImage() (string, error) {
	bin, err := exec.LookPath("wl-paste")
	if err != nil {
		return "", fmt.Errorf("no image")
	}
	for _, mime := range []string{"image/png", "image/jpeg"} {
		cmd := exec.Command(bin, "-t", mime)
		var buf bytes.Buffer
		cmd.Stdout = &buf
		if err := cmd.Run(); err != nil || buf.Len() == 0 {
			continue
		}
		ext := ".png"
		if mime == "image/jpeg" {
			ext = ".jpg"
		}
		f, err := os.CreateTemp("", "wp-paste-*"+ext)
		if err != nil {
			return "", err
		}
		if _, err := f.Write(buf.Bytes()); err != nil {
			f.Close()
			os.Remove(f.Name())
			return "", err
		}
		if err := f.Close(); err != nil {
			os.Remove(f.Name())
			return "", err
		}
		return f.Name(), nil
	}
	return "", fmt.Errorf("no image")
}

func openPath(path string) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("missing file")
	}
	cmd := exec.Command("xdg-open", path)
	return cmd.Start()
}

var voicePlayer struct {
	sync.Mutex
	cmd    *exec.Cmd
	paused bool
}

func playAudio(path string) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("missing file")
	}
	stopVoice()
	players := [][]string{
		{"mpv", "--no-video", "--really-quiet", path},
		{"ffplay", "-nodisp", "-autoexit", "-loglevel", "quiet", path},
		{"pw-play", path},
	}
	for _, args := range players {
		bin, err := exec.LookPath(args[0])
		if err != nil {
			continue
		}
		cmd := exec.Command(bin, args[1:]...)
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		if err := cmd.Start(); err != nil {
			continue
		}
		voicePlayer.Lock()
		voicePlayer.cmd = cmd
		voicePlayer.paused = false
		voicePlayer.Unlock()
		go func() {
			_ = cmd.Wait()
			voicePlayer.Lock()
			if voicePlayer.cmd == cmd {
				voicePlayer.cmd = nil
				voicePlayer.paused = false
			}
			voicePlayer.Unlock()
		}()
		return nil
	}
	return openPath(path)
}

func toggleVoice() (bool, bool) {
	voicePlayer.Lock()
	defer voicePlayer.Unlock()
	if voicePlayer.cmd == nil || voicePlayer.cmd.Process == nil {
		return false, false
	}
	if voicePlayer.cmd.ProcessState != nil && voicePlayer.cmd.ProcessState.Exited() {
		voicePlayer.cmd = nil
		voicePlayer.paused = false
		return false, false
	}
	if voicePlayer.paused {
		_ = voicePlayer.cmd.Process.Signal(syscall.SIGCONT)
		voicePlayer.paused = false
		return true, false
	}
	_ = voicePlayer.cmd.Process.Signal(syscall.SIGSTOP)
	voicePlayer.paused = true
	return true, true
}

func stopVoice() {
	voicePlayer.Lock()
	defer voicePlayer.Unlock()
	if voicePlayer.cmd == nil || voicePlayer.cmd.Process == nil {
		voicePlayer.cmd = nil
		voicePlayer.paused = false
		return
	}
	if voicePlayer.paused {
		_ = voicePlayer.cmd.Process.Signal(syscall.SIGCONT)
	}
	_ = voicePlayer.cmd.Process.Kill()
	voicePlayer.cmd = nil
	voicePlayer.paused = false
}

func dial() (net.Conn, error) {
	sock, err := paths.Sock()
	if err != nil {
		return nil, err
	}
	conn, err := net.DialTimeout("unix", sock, 2*time.Second)
	if err != nil {
		return nil, fmt.Errorf("session is not running")
	}
	return conn, nil
}

func sessionUp() bool {
	conn, err := dial()
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// ensureSession starts wp up in the background when nothing is listening.
// A running daemon built from an older binary is stopped first. Quitting the
// interface leaves the session connected.
func ensureSession() error {
	if sessionUp() {
		replaced, err := replaceStaleDaemon()
		if err != nil {
			return err
		}
		if !replaced {
			return nil
		}
	}
	return startDaemon()
}

func replaceStaleDaemon() (bool, error) {
	conn, err := dial()
	if err != nil {
		return false, nil
	}
	pid, ok := peerPID(conn)
	conn.Close()
	if !ok {
		return false, nil
	}
	running, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", pid))
	if err != nil {
		return false, nil
	}
	exe, err := os.Executable()
	if err != nil {
		return false, err
	}
	current, err := os.Stat(exe)
	if err != nil {
		return false, nil
	}
	var previous os.FileInfo
	if !strings.HasSuffix(running, " (deleted)") {
		previous, _ = os.Stat(running)
	}
	if !binaryStale(running, previous, current) {
		return false, nil
	}
	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
		return false, err
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !sessionUp() {
			return true, nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false, fmt.Errorf("session did not stop")
}

func peerPID(conn net.Conn) (int, bool) {
	uc, ok := conn.(*net.UnixConn)
	if !ok {
		return 0, false
	}
	raw, err := uc.SyscallConn()
	if err != nil {
		return 0, false
	}
	var (
		pid int
		ctl error
	)
	err = raw.Control(func(fd uintptr) {
		cred, err := unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
		if err != nil {
			ctl = err
			return
		}
		pid = int(cred.Pid)
	})
	if err != nil || ctl != nil || pid <= 0 {
		return 0, false
	}
	return pid, true
}

func binaryStale(runningPath string, runningInfo, currentInfo os.FileInfo) bool {
	if strings.HasSuffix(runningPath, " (deleted)") {
		return true
	}
	if runningInfo == nil || currentInfo == nil {
		return false
	}
	return !sameInode(runningInfo, currentInfo)
}

func sameInode(a, b os.FileInfo) bool {
	as, aok := a.Sys().(*syscall.Stat_t)
	bs, bok := b.Sys().(*syscall.Stat_t)
	if !aok || !bok {
		return false
	}
	return as.Dev == bs.Dev && as.Ino == bs.Ino
}

func startDaemon() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	dir, err := paths.Dir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	logf, err := os.OpenFile(filepath.Join(dir, "daemon.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, "up")
	cmd.Stdout = logf
	cmd.Stderr = logf
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		logf.Close()
		return err
	}
	go func() {
		_ = cmd.Wait()
		logf.Close()
	}()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if sessionUp() {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("session did not start. See %s", filepath.Join(dir, "daemon.log"))
}

func watch() (<-chan ipc.Live, error) {
	conn, err := dial()
	if err != nil {
		return nil, err
	}
	enc := json.NewEncoder(conn)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(ipc.Request{Cmd: "watch"}); err != nil {
		conn.Close()
		return nil, err
	}
	dec := json.NewDecoder(conn)
	var resp ipc.Response
	if err := dec.Decode(&resp); err != nil {
		conn.Close()
		return nil, err
	}
	if !resp.OK {
		conn.Close()
		if resp.Error == "" {
			resp.Error = "watch failed"
		}
		return nil, fmt.Errorf("%s", resp.Error)
	}
	ch := make(chan ipc.Live, 32)
	go func() {
		defer close(ch)
		defer conn.Close()
		for {
			var ev ipc.Live
			if err := dec.Decode(&ev); err != nil {
				return
			}
			ch <- ev
		}
	}()
	return ch, nil
}
