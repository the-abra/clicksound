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

// isTTY checks whether the given file descriptor is a terminal.
func isTTY(f *os.File) bool {
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return (info.Mode() & os.ModeCharDevice) != 0
}

// colorize wraps text in ANSI codes if stdout is a TTY.
func colorize(color, text string) string {
	if !isTTY(os.Stdout) {
		return text
	}
	return color + text + reset
}

// colorizeErr wraps text in ANSI codes if stderr is a TTY.
func colorizeErr(color, text string) string {
	if !isTTY(os.Stderr) {
		return text
	}
	return color + text + reset
}

// Bold returns bold text (TTY-aware).
func Bold(text string) string {
	return colorize(bold, text)
}

// Dim returns dim text (TTY-aware).
func Dim(text string) string {
	return colorize(dim, text)
}

// Cyan returns cyan text (TTY-aware).
func Cyan(text string) string {
	return colorize(cyan, text)
}

// Magenta returns magenta text (TTY-aware).
func Magenta(text string) string {
	return colorize(magenta, text)
}

// Green returns green text (TTY-aware).
func Green(text string) string {
	return colorize(green, text)
}

// Yellow returns yellow text (TTY-aware).
func Yellow(text string) string {
	return colorize(yellow, text)
}

// Blue returns blue text (TTY-aware).
func Blue(text string) string {
	return colorize(blue, text)
}

// Info prints an informational message to stdout.
func Info(format string, a ...any) {
	msg := fmt.Sprintf(format, a...)
	sym := colorize(cyan, SymInfo)
	fmt.Printf("%s %s\n", sym, msg)
}

// Success prints a success message to stdout.
func Success(format string, a ...any) {
	msg := fmt.Sprintf(format, a...)
	sym := colorize(green, SymSuccess)
	fmt.Printf("%s %s\n", sym, msg)
}

// Warn prints a warning message to stderr.
func Warn(format string, a ...any) {
	msg := fmt.Sprintf(format, a...)
	sym := colorizeErr(yellow, SymWarn)
	fmt.Fprintf(os.Stderr, "%s %s\n", sym, msg)
}

// Error prints an error message to stderr.
func Error(format string, a ...any) {
	msg := fmt.Sprintf(format, a...)
	sym := colorizeErr(red, SymError)
	fmt.Fprintf(os.Stderr, "%s %s\n", sym, msg)
}

// Fatal prints an error message to stderr and exits with code 1.
func Fatal(format string, a ...any) {
	Error(format, a...)
	os.Exit(1)
}

// Bullet prints a bulleted list item to stdout.
func Bullet(text string) {
	sym := colorize(dim, SymBullet)
	fmt.Printf("  %s %s\n", sym, text)
}

// Header prints a bold colored header line.
func Header(text string) {
	fmt.Println(colorize(bold+cyan, text))
}
