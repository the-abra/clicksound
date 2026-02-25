package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"clicksound/pkg/audio"
	"clicksound/pkg/keyboard"
	"clicksound/pkg/logger"
)

var currentVersion = "V1.0.0"

func getCoreDir() string {
	exePath, err := os.Executable()
	if err != nil {
		logger.Fatal("Failed to resolve executable path: %v", err)
	}
	realPath, err := filepath.EvalSymlinks(exePath)
	if err != nil {
		realPath = exePath
	}
	return filepath.Dir(realPath)
}

func help() {
	fmt.Println()
	logger.Header("  ClickSound " + logger.Dim(currentVersion))
	fmt.Printf("  %s\n\n", logger.Dim("https://github.com/the-abra/clicksound"))

	logger.Header("  Commands")
	fmt.Println()
	commands := []struct {
		name string
		desc string
	}{
		{"list", "List pre-installed sounds"},
		{"start <sound>", "Start or switch the active sound"},
		{"stop", "Stop the daemon"},
		{"help", "Show this help message"},
		{"update", "Check for updates"},
	}
	for _, c := range commands {
		fmt.Printf("    %s  %s  %s\n",
			logger.Green(logger.SymArrow),
			logger.Cyan(padRight(c.name, 16)),
			logger.Dim(c.desc),
		)
	}
	fmt.Println()
}

func padRight(s string, length int) string {
	for len(s) < length {
		s += " "
	}
	return s
}

func list() {
	coreDir := getCoreDir()
	soundsDir := filepath.Join(coreDir, "sounds")
	files, err := os.ReadDir(soundsDir)
	if err != nil {
		logger.Fatal("Cannot read sounds directory: %v", err)
	}

	fmt.Println()
	logger.Header("  Available Sounds")
	fmt.Println()
	count := 0
	for _, f := range files {
		if !f.IsDir() {
			name := f.Name()
			ext := filepath.Ext(name)
			base := strings.TrimSuffix(name, ext)
			logger.Bullet(logger.Bold(base) + logger.Dim(ext))
			count++
		}
	}
	fmt.Println()
	logger.Info("%d sound(s) found in %s", count, logger.Dim(soundsDir))
	fmt.Println()
}

func start(args []string) {
	if len(args) < 1 {
		logger.Error("Please provide a sound file name or path.")
		logger.Info("Usage: %s", logger.Cyan("clicksound start <sound-file>"))
		logger.Info("  Use a filename for preinstalled sounds, or an absolute path (starting with %s) for external files.", logger.Bold("/"))
		return
	}
	soundFile := args[0]

	var fullPath string
	if strings.HasPrefix(soundFile, "/") {
		// Absolute path → external sound file
		fullPath = soundFile
	} else {
		// Relative name → preinstalled sounds directory
		coreDir := getCoreDir()
		fullPath = filepath.Join(coreDir, "sounds", soundFile)
	}

	if _, err := os.Stat(fullPath); os.IsNotExist(err) {
		logger.Error("File not found: %s", logger.Bold(fullPath))
		return
	}

	stop() // kill any existing clicksound process

	daemonPath := filepath.Join(getCoreDir(), "clicksound")
	cmd := exec.Command(daemonPath, "daemon", fullPath)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	err := cmd.Start()
	if err != nil {
		logger.Fatal("Failed to start daemon: %v", err)
	}
	logger.Success("Theme activated: %s", logger.Bold(soundFile))
}

func stop() {
	cmd := exec.Command("pkill", "-f", "clicksound daemon")
	cmd.Run()
	logger.Success("Stopped clicksound daemon.")
}

func update() {
	logger.Warn("Update checking is not implemented in this port yet.")
}

func daemon(args []string) {
	if len(args) < 1 {
		logger.Fatal("Missing sound file path for daemon.")
	}
	soundFile := args[0]

	devPath, err := keyboard.FindDevice()
	if err != nil {
		logger.Fatal("Could not find keyboard device: %v", err)
	}

	logger.Info("Keyboard: %s", logger.Dim(devPath))
	logger.Info("Sound:    %s", logger.Dim(soundFile))
	logger.Success("Daemon started — listening for key presses…")

	player, err := audio.NewPlayer(soundFile)
	if err != nil {
		logger.Fatal("Audio player init failed: %v", err)
	}

	err = keyboard.Listen(devPath, func() {
		player.Play()
	})
	if err != nil {
		logger.Fatal("Keyboard listener error: %v", err)
	}
}

func Execute() {
	if len(os.Args) < 2 {
		help()
		os.Exit(0)
	}

	command := os.Args[1]
	switch command {
	case "list":
		list()
	case "start":
		start(os.Args[2:])
	case "stop":
		stop()
	case "daemon":
		daemon(os.Args[2:])
	case "help", "--h", "-h":
		help()
	case "update":
		update()
	default:
		logger.Error("Unknown command: %s", logger.Bold(command))
		help()
		os.Exit(1)
	}
}
