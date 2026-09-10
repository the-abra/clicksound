// Package daemon manages the lifecycle files used to run ClickSound in the
// background: a PID file to find/stop the running daemon and a log file for its
// output.
package daemon

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	pidFileName = "clicksound.pid"
	logFileName = "clicksound.log"
	appDirName  = "clicksound"
)

// PIDPath returns the path of the runtime PID file. It prefers
// $XDG_RUNTIME_DIR (per-user, cleared on logout) and falls back to a
// per-UID file in the temporary directory.
func PIDPath() string {
	if dir := os.Getenv("XDG_RUNTIME_DIR"); dir != "" {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			return filepath.Join(dir, pidFileName)
		}
	}
	return filepath.Join(os.TempDir(), fmt.Sprintf("%s-%d.pid", appDirName, os.Getuid()))
}

// LogPath returns the path of the daemon log file, creating its directory when
// possible. It prefers $XDG_STATE_HOME and falls back to the temporary
// directory.
func LogPath() string {
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		if home, err := os.UserHomeDir(); err == nil {
			base = filepath.Join(home, ".local", "state")
		}
	}
	if base == "" {
		return filepath.Join(os.TempDir(), fmt.Sprintf("%s-%d.log", appDirName, os.Getuid()))
	}

	dir := filepath.Join(base, appDirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return filepath.Join(os.TempDir(), fmt.Sprintf("%s-%d.log", appDirName, os.Getuid()))
	}
	return filepath.Join(dir, logFileName)
}

// Write records pid in the PID file at path.
func Write(path string, pid int) error {
	if err := os.WriteFile(path, []byte(strconv.Itoa(pid)+"\n"), 0o644); err != nil {
		return fmt.Errorf("write pid file: %w", err)
	}
	return nil
}

// Read returns the pid stored at path.
func Read(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, fmt.Errorf("invalid pid file %q: %w", path, err)
	}
	return pid, nil
}

// Remove deletes the PID file, ignoring errors.
func Remove(path string) {
	_ = os.Remove(path)
}

// IsRunning reports whether a process with the given pid exists and is
// signalable. A process owned by another user returns EPERM, which still means
// it is running.
func IsRunning(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// Status reads the PID file and reports the pid and whether it is alive. A
// stale PID file is removed.
func Status(path string) (pid int, running bool) {
	pid, err := Read(path)
	if err != nil {
		return 0, false
	}
	if !IsRunning(pid) {
		Remove(path)
		return pid, false
	}
	return pid, true
}

// Stop terminates the daemon recorded in the PID file. It waits up to timeout
// for a graceful exit before force-killing. It reports whether a running
// daemon was actually stopped.
func Stop(path string, timeout time.Duration) (bool, error) {
	pid, running := Status(path)
	if !running {
		return false, nil
	}

	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
		if errors.Is(err, syscall.ESRCH) {
			Remove(path)
			return false, nil
		}
		return false, fmt.Errorf("signal process %d: %w", pid, err)
	}

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !IsRunning(pid) {
			Remove(path)
			return true, nil
		}
		time.Sleep(20 * time.Millisecond)
	}

	// The daemon ignored SIGTERM; escalate.
	_ = syscall.Kill(pid, syscall.SIGKILL)
	Remove(path)
	return true, nil
}

// ActiveSound returns the sound file passed to a running daemon by inspecting
// /proc/<pid>/cmdline. It returns "" when it cannot be determined.
func ActiveSound(pid int) string {
	return soundFromCmdline(cmdline(pid))
}

// ActiveDevice returns the --device value of a running daemon, or "" when it
// was not specified.
func ActiveDevice(pid int) string {
	return deviceFromCmdline(cmdline(pid))
}

// soundFromCmdline extracts the argument following the "daemon" subcommand.
func soundFromCmdline(parts []string) string {
	for i, part := range parts {
		if part == "daemon" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}

// deviceFromCmdline extracts the value of --device from a command line.
func deviceFromCmdline(parts []string) string {
	for i, part := range parts {
		if part == "--device" && i+1 < len(parts) {
			return parts[i+1]
		}
		if strings.HasPrefix(part, "--device=") {
			return strings.TrimPrefix(part, "--device=")
		}
	}
	return ""
}

func cmdline(pid int) []string {
	if pid <= 0 {
		return nil
	}
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	if err != nil {
		return nil
	}
	return strings.Split(string(data), "\x00")
}
