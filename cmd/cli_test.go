package cmd

import (
	"flag"
	"testing"
	"unicode/utf8"
)

func TestParseInterspersed(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	n := fs.Int("n", 0, "number")

	positional, err := parseInterspersed(fs, []string{"a", "-n", "3", "b"})
	if err != nil {
		t.Fatalf("parseInterspersed: %v", err)
	}
	if *n != 3 {
		t.Fatalf("n = %d, want 3", *n)
	}
	if len(positional) != 2 || positional[0] != "a" || positional[1] != "b" {
		t.Fatalf("positional = %v, want [a b]", positional)
	}
}

func TestParseStartFlags(t *testing.T) {
	opts, positional, err := parseStartFlags("start", []string{"pop", "--volume", "40", "--device", "/dev/input/event2"})
	if err != nil {
		t.Fatalf("parseStartFlags: %v", err)
	}
	if opts.volume != 40 {
		t.Errorf("volume = %d, want 40", opts.volume)
	}
	if opts.device != "/dev/input/event2" {
		t.Errorf("device = %q, want /dev/input/event2", opts.device)
	}
	if len(positional) != 1 || positional[0] != "pop" {
		t.Errorf("positional = %v, want [pop]", positional)
	}

	// Flags before the positional argument, using shorthands and = syntax.
	opts, positional, err = parseStartFlags("start", []string{"--volume=70", "-d", "all", "mech1"})
	if err != nil {
		t.Fatalf("parseStartFlags: %v", err)
	}
	if opts.volume != 70 || opts.device != "all" {
		t.Errorf("opts = %+v, want volume=70 device=all", opts)
	}
	if len(positional) != 1 || positional[0] != "mech1" {
		t.Errorf("positional = %v, want [mech1]", positional)
	}
}

func TestParseStartFlagsDefaults(t *testing.T) {
	opts, _, err := parseStartFlags("start", []string{"pop"})
	if err != nil {
		t.Fatalf("parseStartFlags: %v", err)
	}
	if opts.volume != 100 {
		t.Errorf("default volume = %d, want 100", opts.volume)
	}
	if opts.device != "" {
		t.Errorf("default device = %q, want empty", opts.device)
	}
}

func TestParseStartFlagsRejectsBadVolume(t *testing.T) {
	for _, args := range [][]string{
		{"--volume", "101", "pop"},
		{"--volume", "-1", "pop"},
	} {
		if _, _, err := parseStartFlags("start", args); err == nil {
			t.Errorf("parseStartFlags(%v) = nil error, want failure", args)
		}
	}
}

func TestSameVersion(t *testing.T) {
	for _, tt := range []struct {
		a, b string
		want bool
	}{
		{"v1.2.3", "1.2.3", true},
		{"V1.1.0", "v1.1.0", true},
		{" v1.2.3 ", "1.2.3", true},
		{"v1.2.3", "v1.2.4", false},
	} {
		if got := sameVersion(tt.a, tt.b); got != tt.want {
			t.Errorf("sameVersion(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestCheckSupported(t *testing.T) {
	if err := checkSupported("/tmp/sound.MP3"); err != nil {
		t.Errorf("checkSupported(.MP3) = %v, want nil", err)
	}
	if err := checkSupported("/tmp/sound.wav"); err == nil {
		t.Error("checkSupported(.wav) = nil, want error")
	}
}

func TestHelpers(t *testing.T) {
	if got := padRight("ab", 4); got != "ab  " {
		t.Errorf("padRight = %q, want %q", got, "ab  ")
	}
	if got := padRight("abcd", 2); got != "abcd" {
		t.Errorf("padRight long = %q, want %q", got, "abcd")
	}
	if got := truncate("hello", 10); got != "hello" {
		t.Errorf("truncate short = %q, want %q", got, "hello")
	}
	if got := truncate("hello world", 8); utf8.RuneCountInString(got) > 8 || got != "hello w…" {
		t.Errorf("truncate long = %q, want %q", got, "hello w…")
	}
}
