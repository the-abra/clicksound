// Package logger provides small, TTY-aware, colorized output helpers.
package logger

import (
	"fmt"
	"os"
)

// ANSI color codes
const (
	reset   = "\033[0m"
	bold    = "\033[1m"
	dim     = "\033[2m"
	red     = "\033[31m"
	green   = "\033[32m"
	yellow  = "\033[33m"
	blue    = "\033[34m"
	magenta = "\033[35m"
	cyan    = "\033[36m"
	white   = "\033[37m"
)

// Symbols for TUI output
const (
	SymInfo    = "ℹ"
	SymSuccess = "✔"
	SymWarn    = "⚠"
	SymError   = "✖"
	SymBullet  = "•"
	SymArrow   = "→"
)

// colorEnabled reports whether ANSI colors should be emitted. It honours the
// NO_COLOR (https://no-color.org) and FORCE_COLOR conventions and otherwise
// enables colors only for a TTY.
func colorEnabled() bool {
	if _, ok := os.LookupEnv("NO_COLOR"); ok {
		return false
	}
	if _, ok := os.LookupEnv("FORCE_COLOR"); ok {
		return true
	}
	return isTTY(os.Stdout) || isTTY(os.Stderr)
}

// isTTY checks whether the given file descriptor is a terminal.
func isTTY(f *os.File) bool {
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return (info.Mode() & os.ModeCharDevice) != 0
}

func colorize(color, text string) string {
	if !colorEnabled() {
		return text
	}
	return color + text + reset
}

// Bold returns bold text (TTY-aware).
func Bold(text string) string { return colorize(bold, text) }

// Dim returns dim text (TTY-aware).
func Dim(text string) string { return colorize(dim, text) }

// Red returns red text (TTY-aware).
func Red(text string) string { return colorize(red, text) }

// Green returns green text (TTY-aware).
func Green(text string) string { return colorize(green, text) }

// Yellow returns yellow text (TTY-aware).
func Yellow(text string) string { return colorize(yellow, text) }

// Blue returns blue text (TTY-aware).
func Blue(text string) string { return colorize(blue, text) }

// Magenta returns magenta text (TTY-aware).
func Magenta(text string) string { return colorize(magenta, text) }

// Cyan returns cyan text (TTY-aware).
func Cyan(text string) string { return colorize(cyan, text) }

// Info prints an informational message to stdout.
func Info(format string, a ...any) {
	fmt.Printf("%s %s\n", colorize(cyan, SymInfo), fmt.Sprintf(format, a...))
}

// Success prints a success message to stdout.
func Success(format string, a ...any) {
	fmt.Printf("%s %s\n", colorize(green, SymSuccess), fmt.Sprintf(format, a...))
}

// Warn prints a warning message to stderr.
func Warn(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "%s %s\n", colorize(yellow, SymWarn), fmt.Sprintf(format, a...))
}

// Error prints an error message to stderr.
func Error(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "%s %s\n", colorize(red, SymError), fmt.Sprintf(format, a...))
}

// Fatal prints an error message to stderr and exits with code 1.
func Fatal(format string, a ...any) {
	Error(format, a...)
	os.Exit(1)
}

// Bullet prints a bulleted list item to stdout.
func Bullet(text string) {
	fmt.Printf("  %s %s\n", colorize(dim, SymBullet), text)
}

// Header prints a bold colored header line.
func Header(text string) {
	fmt.Println(colorize(bold+cyan, text))
}

// Hint prints an indented, dimmed suggestion line to stderr.
func Hint(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "  %s %s\n", colorize(dim, SymArrow), colorize(dim, fmt.Sprintf(format, a...)))
}
