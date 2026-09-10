package daemon

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWriteReadRemove(t *testing.T) {
	path := filepath.Join(t.TempDir(), "clicksound.pid")
	if err := Write(path, 4242); err != nil {
		t.Fatalf("Write: %v", err)
	}
	pid, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if pid != 4242 {
		t.Fatalf("pid = %d, want 4242", pid)
	}
	Remove(path)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("pid file still exists after Remove: %v", err)
	}
}

func TestStatusRemovesStalePID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "clicksound.pid")
	if err := Write(path, 1<<30); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if _, running := Status(path); running {
		t.Fatal("Status reported a stale pid as running")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("stale pid file was not removed: %v", err)
	}
}

func TestIsRunning(t *testing.T) {
	if !IsRunning(os.Getpid()) {
		t.Fatal("IsRunning(self) = false, want true")
	}
	if IsRunning(-1) {
		t.Fatal("IsRunning(-1) = true, want false")
	}
	if IsRunning(0) {
		t.Fatal("IsRunning(0) = true, want false")
	}
}

func TestStopWhenNotRunning(t *testing.T) {
	path := filepath.Join(t.TempDir(), "none.pid")
	stopped, err := Stop(path, 20*time.Millisecond)
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if stopped {
		t.Fatal("Stop reported stopping a daemon that was not running")
	}
}

func TestParsersFromCmdline(t *testing.T) {
	parts := []string{
		"/usr/local/bin/clicksound", "daemon", "/sounds/pop.mp3",
		"--volume", "50", "--device", "/dev/input/event3",
	}
	if got := soundFromCmdline(parts); got != "/sounds/pop.mp3" {
		t.Errorf("soundFromCmdline = %q, want %q", got, "/sounds/pop.mp3")
	}
	if got := deviceFromCmdline(parts); got != "/dev/input/event3" {
		t.Errorf("deviceFromCmdline = %q, want %q", got, "/dev/input/event3")
	}

	eqForm := []string{"/bin/clicksound", "daemon", "/sounds/x.mp3", "--device=/dev/input/event7"}
	if got := deviceFromCmdline(eqForm); got != "/dev/input/event7" {
		t.Errorf("deviceFromCmdline (--device=) = %q, want %q", got, "/dev/input/event7")
	}

	if got := soundFromCmdline([]string{"clicksound", "status"}); got != "" {
		t.Errorf("soundFromCmdline without daemon = %q, want empty", got)
	}
	if got := deviceFromCmdline(nil); got != "" {
		t.Errorf("deviceFromCmdline(nil) = %q, want empty", got)
	}
}

func TestLogPathRespectsXDGStateHome(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)
	want := filepath.Join(dir, appDirName, logFileName)
	if got := LogPath(); got != want {
		t.Fatalf("LogPath = %q, want %q", got, want)
	}
	if _, err := os.Stat(filepath.Dir(want)); err != nil {
		t.Fatalf("log directory was not created: %v", err)
	}
}

func TestPIDPathUsesRuntimeDir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", dir)
	want := filepath.Join(dir, pidFileName)
	if got := PIDPath(); got != want {
		t.Fatalf("PIDPath = %q, want %q", got, want)
	}
}
