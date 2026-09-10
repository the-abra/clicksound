package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"clicksound/pkg/audio"
	"clicksound/pkg/daemon"
	"clicksound/pkg/keyboard"
	"clicksound/pkg/logger"
)

// currentVersion can be overridden at build time with
// -ldflags "-X clicksound/cmd.currentVersion=v1.2.3".
var currentVersion = "v1.1.0"

const (
	repoURL     = "https://github.com/the-abra/clicksound"
	releasesAPI = "https://api.github.com/repos/the-abra/clicksound/releases/latest"
)

// coreDir returns the directory containing the real executable, resolving
// symlinks. It returns "" if the executable path cannot be determined.
func coreDir() string {
	exePath, err := os.Executable()
	if err != nil {
		return ""
	}
	realPath, err := filepath.EvalSymlinks(exePath)
	if err != nil {
		realPath = exePath
	}
	return filepath.Dir(realPath)
}

func soundsDir() string {
	candidates := make([]string, 0, 3)
	if dir := coreDir(); dir != "" {
		candidates = append(candidates, filepath.Join(dir, "sounds"))
	}
	// `go run` executes a temporary binary, so fall back to the working
	// directory (which is the project root in the common case).
	if wd, err := os.Getwd(); err == nil {
		candidates = append(candidates, filepath.Join(wd, "sounds"))
	}

	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate
		}
	}
	if len(candidates) > 0 {
		return candidates[0]
	}
	return filepath.Join("sounds")
}

type soundEntry struct {
	Name string
	Path string
}

// listSounds returns the bundled .mp3 themes, sorted by name.
func listSounds() ([]soundEntry, error) {
	entries, err := os.ReadDir(soundsDir())
	if err != nil {
		return nil, err
	}
	var sounds []soundEntry
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := filepath.Ext(e.Name())
		if !strings.EqualFold(ext, ".mp3") {
			continue
		}
		sounds = append(sounds, soundEntry{
			Name: strings.TrimSuffix(e.Name(), ext),
			Path: filepath.Join(soundsDir(), e.Name()),
		})
	}
	sort.Slice(sounds, func(i, j int) bool { return sounds[i].Name < sounds[j].Name })
	return sounds, nil
}

func list() {
	sounds, err := listSounds()
	if err != nil {
		logger.Fatal("Cannot read sounds directory: %v", err)
	}

	activePID, activeRunning := daemon.Status(daemon.PIDPath())
	active := ""
	if activeRunning {
		active = daemon.ActiveSound(activePID)
	}

	fmt.Println()
	logger.Header("  Available Sounds")
	fmt.Println()
	for i, s := range sounds {
		marker, suffix := " ", ""
		if active != "" && sameFile(active, s.Path) {
			marker = logger.Green("*")
			suffix = " " + logger.Dim("(active)")
		}
		fmt.Printf("  %s %s  %s%s\n",
			marker,
			logger.Cyan(padRight(strconv.Itoa(i+1), 2)),
			logger.Bold(s.Name),
			suffix,
		)
	}
	fmt.Println()
	logger.Info("%d sound(s) in %s", len(sounds), logger.Dim(soundsDir()))
	fmt.Println()
}

// resolveSound turns a user-supplied argument into an absolute file path. It
// accepts a list index, a bare theme name (with or without extension), a path,
// or an absolute/relative file path.
func resolveSound(arg string) (string, error) {
	// A list index (e.g. "3").
	if n, err := strconv.Atoi(arg); err == nil {
		sounds, err := listSounds()
		if err != nil {
			return "", fmt.Errorf("cannot read sounds directory: %w", err)
		}
		if n < 1 || n > len(sounds) {
			return "", fmt.Errorf("sound #%d is out of range (1-%d)", n, len(sounds))
		}
		return sounds[n-1].Path, nil
	}

	// An explicit path: absolute, or containing a separator.
	if filepath.IsAbs(arg) || strings.ContainsRune(arg, filepath.Separator) {
		if path, err := existingFile(arg); err == nil {
			return path, nil
		}
		return "", fmt.Errorf("file not found: %s", arg)
	}

	// A bare name that happens to exist in the working directory.
	if path, err := existingFile(arg); err == nil {
		return path, nil
	}

	// A bundled theme, with or without extension.
	for _, candidate := range []string{
		filepath.Join(soundsDir(), arg),
		filepath.Join(soundsDir(), arg+".mp3"),
	} {
		if path, err := existingFile(candidate); err == nil {
			return path, nil
		}
	}

	return "", fmt.Errorf("sound %q not found", arg)
}

func existingFile(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", fmt.Errorf("%s is a directory", path)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return path, nil
	}
	return abs, nil
}

func checkSupported(path string) error {
	if strings.EqualFold(filepath.Ext(path), ".mp3") {
		return nil
	}
	return fmt.Errorf("unsupported audio format %q (only .mp3 is supported)", filepath.Ext(path))
}

type startOptions struct {
	volume int
	device string
}

func start(args []string) {
	opts, positional, err := parseStartFlags("start", args)
	if err != nil {
		logger.Error("%v", err)
		startUsage()
		return
	}
	if len(positional) < 1 {
		logger.Error("Missing sound name or path.")
		startUsage()
		return
	}

	sound, err := resolveSound(positional[0])
	if err != nil {
		logger.Error("%v", err)
		hintSounds(positional[0])
		return
	}
	if err := checkSupported(sound); err != nil {
		logger.Error("%v", err)
		return
	}

	if runningUnderGoRun() {
		logger.Warn("Running via 'go run': the daemon binary is temporary. Build first for a persistent daemon: %s", logger.Bold("go build -o clicksound ."))
	}

	if stopped, err := daemon.Stop(daemon.PIDPath(), 3*time.Second); err != nil {
		logger.Warn("Could not stop the previous daemon: %v", err)
	} else if stopped {
		logger.Info("Stopped the previous theme.")
	}

	if err := spawnDaemon(sound, opts); err != nil {
		logger.Fatal("%v", err)
	}

	logger.Success("Theme activated: %s", logger.Bold(filepath.Base(sound)))
	logger.Hint("Stop it any time with: %s", logger.Bold("clicksound stop"))
}

func spawnDaemon(sound string, opts startOptions) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable: %w", err)
	}

	logFile, err := os.OpenFile(daemon.LogPath(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open log file: %w", err)
	}
	defer logFile.Close()

	daemonArgs := []string{"daemon", sound, "--volume", strconv.Itoa(opts.volume)}
	if opts.device != "" {
		daemonArgs = append(daemonArgs, "--device", opts.device)
	}

	cmd := exec.Command(exe, daemonArgs...)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start daemon: %w", err)
	}

	// The child owns its PID file; wait for it so startup failures are
	// reported instead of silently detaching.
	pidPath := daemon.PIDPath()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if pid, running := daemon.Status(pidPath); running && pid == cmd.Process.Pid {
			return nil
		}
		if !daemon.IsRunning(cmd.Process.Pid) {
			break
		}
		time.Sleep(30 * time.Millisecond)
	}

	if lines := tailLog(6); lines != "" {
		return fmt.Errorf("daemon failed to start; recent log output:\n%s", lines)
	}
	return errors.New("daemon failed to start (see " + daemon.LogPath() + ")")
}

func stop() {
	stopped, err := daemon.Stop(daemon.PIDPath(), 3*time.Second)
	if err != nil {
		logger.Error("%v", err)
		return
	}
	if stopped {
		logger.Success("Stopped ClickSound daemon.")
		return
	}
	logger.Warn("ClickSound is not running.")
}

func restart(args []string) {
	if len(args) == 0 {
		// Keep the current theme if one is running.
		pid, running := daemon.Status(daemon.PIDPath())
		if !running {
			logger.Error("Nothing is running to restart.")
			startUsage()
			return
		}
		if sound := daemon.ActiveSound(pid); sound != "" {
			args = []string{sound}
		}
	}
	start(args)
}

func status() {
	pid, running := daemon.Status(daemon.PIDPath())
	if !running {
		logger.Warn("ClickSound is not running.")
		logger.Hint("Start it with: %s", logger.Bold("clicksound start <sound>"))
		return
	}

	fmt.Println()
	logger.Success("ClickSound is running (pid %d)", pid)
	if sound := daemon.ActiveSound(pid); sound != "" {
		logger.Info("  Sound:  %s", logger.Bold(filepath.Base(sound)))
	}
	if device := daemon.ActiveDevice(pid); device != "" {
		logger.Info("  Device: %s", logger.Dim(device))
	} else {
		logger.Info("  Device: %s", logger.Dim("auto-detected"))
	}
	logger.Info("  Log:    %s", logger.Dim(daemon.LogPath()))
	fmt.Println()
}

func devices() {
	devs, err := keyboard.ListDevices()
	if err != nil {
		logger.Fatal("Cannot list input devices: %v", err)
	}

	fmt.Println()
	logger.Header("  Input Devices")
	fmt.Println()
	for _, d := range devs {
		marker, label := logger.Dim("○"), logger.Dim("other")
		if d.Keyboard {
			marker, label = logger.Green("●"), logger.Green("keyboard")
		}
		name := d.Name
		if name == "" {
			name = "(unnamed)"
		}
		fmt.Printf("  %s %-20s %-28s %s\n",
			marker,
			logger.Cyan(d.Path),
			truncate(name, 28),
			label+logger.Dim(fmt.Sprintf(" score=%d", d.Score)),
		)
	}
	fmt.Println()
	logger.Info("Keyboards are detected by their key capabilities, not just by name.")
	logger.Hint("Select one with: %s", logger.Bold("clicksound start <sound> --device <path>"))
	fmt.Println()
}

func logs(args []string) {
	fs := flag.NewFlagSet("logs", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	n := fs.Int("n", 30, "number of lines to show")
	fs.IntVar(n, "lines", 30, "number of lines to show")
	if _, err := parseInterspersed(fs, args); err != nil {
		logger.Error("%v", err)
		return
	}

	path := daemon.LogPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			logger.Warn("No log output yet at %s", path)
			return
		}
		logger.Fatal("Cannot read log file: %v", err)
	}

	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if *n > 0 && len(lines) > *n {
		lines = lines[len(lines)-*n:]
	}
	for _, line := range lines {
		fmt.Println(line)
	}
}

func update() {
	logger.Info("Current version: %s", logger.Bold(currentVersion))

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, releasesAPI, nil)
	if err != nil {
		logger.Warn("Update check failed: %v", err)
		return
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "clicksound")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		logger.Warn("Could not reach GitHub: %v", err)
		logger.Hint("Check manually: %s/releases", repoURL)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		logger.Warn("Update check unavailable (HTTP %d).", resp.StatusCode)
		logger.Hint("Check manually: %s/releases", repoURL)
		return
	}

	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&release); err != nil {
		logger.Warn("Could not parse release info: %v", err)
		return
	}

	if release.TagName == "" || sameVersion(release.TagName, currentVersion) {
		logger.Success("You are on the latest version (%s).", currentVersion)
		return
	}
	logger.Warn("A newer version is available: %s", logger.Bold(release.TagName))
	logger.Hint("Download: %s/releases", repoURL)
}

// daemonRun is the internal long-running entry point started by `start`.
func daemonRun(args []string) {
	opts, positional, err := parseStartFlags("daemon", args)
	if err != nil {
		logger.Fatal("%v", err)
	}
	if len(positional) < 1 {
		logger.Fatal("Missing sound file path for daemon.")
	}
	sound := positional[0]

	pidPath := daemon.PIDPath()
	if pid, running := daemon.Status(pidPath); running && pid != os.Getpid() {
		logger.Fatal("Another ClickSound daemon is already running (pid %d).", pid)
	}

	paths, err := resolveDevices(opts.device)
	if err != nil {
		logger.Fatal("%v", err)
	}

	player, err := audio.NewPlayer(sound, audio.WithVolume(float64(opts.volume)/100))
	if err != nil {
		logger.Fatal("Audio player init failed: %v", err)
	}

	if err := daemon.Write(pidPath, os.Getpid()); err != nil {
		logger.Fatal("Could not write pid file: %v", err)
	}
	defer daemon.Remove(pidPath)

	ctx, stopSignals := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stopSignals()

	logger.Info("Sound:    %s (%s)", logger.Dim(sound), player.Duration().Round(time.Millisecond))
	logger.Info("Keyboard: %s", logger.Dim(strings.Join(paths, ", ")))
	logger.Success("Daemon started (pid %d) — listening for key presses…", os.Getpid())

	if err := keyboard.ListenAll(ctx, paths, func(uint16) { player.Play() }); err != nil {
		logger.Fatal("Keyboard listener error: %v", err)
	}
	logger.Info("Daemon stopped.")
}

func resolveDevices(selection string) ([]string, error) {
	switch {
	case selection == "all":
		return keyboard.FindKeyboards()
	case selection != "":
		if _, err := os.Stat(selection); err != nil {
			return nil, fmt.Errorf("device %q: %w", selection, err)
		}
		return []string{selection}, nil
	default:
		return keyboard.FindKeyboards()
	}
}

func parseStartFlags(name string, args []string) (startOptions, []string, error) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	volume := fs.Int("volume", 100, "playback volume (0-100)")
	fs.IntVar(volume, "v", 100, "playback volume (0-100)")
	device := fs.String("device", "", "input device path or 'all'")
	fs.StringVar(device, "d", "", "input device path or 'all'")

	positional, err := parseInterspersed(fs, args)
	if err != nil {
		return startOptions{}, nil, err
	}
	if *volume < 0 || *volume > 100 {
		return startOptions{}, nil, errors.New("volume must be between 0 and 100")
	}
	return startOptions{volume: *volume, device: *device}, positional, nil
}

// parseInterspersed parses flags even when they are mixed with positional
// arguments, so both `start --volume 50 pop` and `start pop --volume 50` work.
func parseInterspersed(fs *flag.FlagSet, args []string) ([]string, error) {
	var positional []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		rest := fs.Args()
		if len(rest) == 0 {
			break
		}
		positional = append(positional, rest[0])
		args = rest[1:]
	}
	return positional, nil
}

func help() {
	fmt.Println()
	logger.Header("  ClickSound " + logger.Dim(currentVersion))
	fmt.Printf("  %s\n\n", logger.Dim(repoURL))

	logger.Header("  Usage")
	fmt.Println()
	fmt.Printf("    clicksound %s\n\n", logger.Cyan("<command> [options]"))

	logger.Header("  Commands")
	fmt.Println()
	commands := []struct{ name, desc string }{
		{"list", "List pre-installed sounds"},
		{"start <sound>", "Start or switch the active sound"},
		{"stop", "Stop the running daemon"},
		{"restart [sound]", "Restart the daemon, optionally with a new sound"},
		{"status", "Show what is currently running"},
		{"devices", "List input devices and detected keyboards"},
		{"logs", "Show recent daemon output"},
		{"update", "Check for a newer release"},
		{"version", "Print the version"},
		{"help", "Show this help message"},
	}
	for _, c := range commands {
		fmt.Printf("    %s  %s  %s\n",
			logger.Green(logger.SymArrow),
			logger.Cyan(padRight(c.name, 18)),
			logger.Dim(c.desc),
		)
	}
	fmt.Println()

	logger.Header("  Options")
	fmt.Println()
	options := []struct{ name, desc string }{
		{"--volume, -v <0-100>", "Playback volume (default 100)"},
		{"--device, -d <path|all>", "Input device to listen on (default: auto)"},
		{"--help, -h", "Show this help message"},
		{"--version", "Print the version"},
	}
	for _, o := range options {
		fmt.Printf("    %s  %s  %s\n",
			logger.Green(logger.SymArrow),
			logger.Cyan(padRight(o.name, 24)),
			logger.Dim(o.desc),
		)
	}
	fmt.Println()

	logger.Header("  Examples")
	fmt.Println()
	examples := []string{
		"clicksound list",
		"clicksound start pop",
		"clicksound start 1 --volume 60",
		"clicksound start /path/to/sound.mp3 --device /dev/input/event5",
		"clicksound stop",
	}
	for _, e := range examples {
		fmt.Printf("    %s %s\n", logger.Dim("$"), e)
	}
	fmt.Println()
}

func printVersion() {
	fmt.Printf("clicksound %s\n", currentVersion)
}

func startUsage() {
	logger.Info("Usage: %s", logger.Cyan("clicksound start <sound|#|/path/to.mp3> [--volume 0-100] [--device <path|all>]"))
	logger.Hint("Run %s to list pre-installed sounds.", logger.Bold("clicksound list"))
}

// runningUnderGoRun reports whether the current binary is the temporary
// executable produced by `go run`.
func runningUnderGoRun() bool {
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	sep := string(filepath.Separator)
	return strings.Contains(exe, sep+"go-build") && strings.Contains(exe, sep+"exe"+sep)
}

func hintSounds(query string) {
	sounds, err := listSounds()
	if err != nil {
		return
	}
	needle := strings.ToLower(strings.TrimSuffix(query, filepath.Ext(query)))
	var matches []string
	for _, s := range sounds {
		lower := strings.ToLower(s.Name)
		if needle != "" && (strings.Contains(lower, needle) || strings.Contains(needle, lower)) {
			matches = append(matches, s.Name)
		}
	}
	if len(matches) > 0 {
		logger.Hint("Did you mean: %s?", strings.Join(matches, ", "))
	}
	logger.Hint("Run %s to see all sounds.", logger.Bold("clicksound list"))
}

func tailLog(n int) string {
	data, err := os.ReadFile(daemon.LogPath())
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if n > 0 && len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return "  " + strings.Join(lines, "\n  ")
}

func sameVersion(a, b string) bool {
	normalize := func(s string) string {
		return strings.TrimPrefix(strings.ToLower(strings.TrimSpace(s)), "v")
	}
	return normalize(a) == normalize(b)
}

func sameFile(a, b string) bool {
	aInfo, aErr := os.Stat(a)
	bInfo, bErr := os.Stat(b)
	if aErr == nil && bErr == nil {
		return os.SameFile(aInfo, bInfo)
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

func truncate(s string, max int) string {
	if max <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	if max == 1 {
		return "…"
	}
	return string(runes[:max-1]) + "…"
}

func padRight(s string, length int) string {
	if len(s) >= length {
		return s
	}
	return s + strings.Repeat(" ", length-len(s))
}

// Execute is the CLI entry point.
func Execute() {
	if len(os.Args) < 2 {
		help()
		return
	}

	command := os.Args[1]
	args := os.Args[2:]

	switch command {
	case "help", "-h", "--help":
		help()
		return
	case "version", "-v", "--version":
		printVersion()
		return
	}

	switch command {
	case "list":
		list()
	case "start":
		start(args)
	case "stop":
		stop()
	case "restart":
		restart(args)
	case "status":
		status()
	case "devices":
		devices()
	case "logs":
		logs(args)
	case "update":
		update()
	case "daemon":
		daemonRun(args)
	default:
		logger.Error("Unknown command: %s", logger.Bold(command))
		logger.Hint("Run %s to see available commands.", logger.Bold("clicksound help"))
		os.Exit(1)
	}
}
