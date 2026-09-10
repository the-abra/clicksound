package audio

import (
	"path/filepath"
	"testing"
	"time"
)

func TestPCMDuration(t *testing.T) {
	tests := []struct {
		name       string
		n          int
		sampleRate int
		channels   int
		want       time.Duration
	}{
		{"one second stereo", 44100 * 2 * 2, 44100, 2, time.Second},
		{"half second mono", 22050 * 1 * 2, 44100, 1, 500 * time.Millisecond},
		{"empty", 0, 44100, 2, 0},
		{"invalid rate", 100, 0, 2, 0},
		{"invalid channels", 100, 44100, 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := pcmDuration(tt.n, tt.sampleRate, tt.channels); got != tt.want {
				t.Fatalf("pcmDuration(%d, %d, %d) = %v, want %v", tt.n, tt.sampleRate, tt.channels, got, tt.want)
			}
		})
	}
}

func TestClampVolume(t *testing.T) {
	tests := []struct {
		in, want float64
	}{
		{-1, 0},
		{0, 0},
		{0.5, 0.5},
		{1, 1},
		{2, 1},
	}
	for _, tt := range tests {
		if got := clampVolume(tt.in); got != tt.want {
			t.Errorf("clampVolume(%v) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestDecodeFile(t *testing.T) {
	path := filepath.Join("..", "..", "sounds", "pop.mp3")
	pcm, sampleRate, channels, err := decodeFile(path)
	if err != nil {
		t.Fatalf("decodeFile(%q): %v", path, err)
	}
	if len(pcm) == 0 {
		t.Fatal("decoded PCM is empty")
	}
	if sampleRate <= 0 {
		t.Fatalf("sampleRate = %d, want > 0", sampleRate)
	}
	if channels != mp3Channels {
		t.Fatalf("channels = %d, want %d", channels, mp3Channels)
	}
	if d := pcmDuration(len(pcm), sampleRate, channels); d <= 0 {
		t.Fatalf("duration = %v, want > 0", d)
	}
}

func TestVoiceBufferBytes(t *testing.T) {
	got := voiceBufferBytes(44100, 2)
	if got != 44100*4*50/1000 {
		t.Fatalf("voiceBufferBytes(44100, 2) = %d, want %d", got, 44100*4*50/1000)
	}
	if got%(2*2) != 0 {
		t.Fatalf("buffer %d is not frame-aligned", got)
	}
	if voiceBufferBytes(0, 2) != 0 {
		t.Error("voiceBufferBytes with zero rate should be 0")
	}
	if voiceBufferBytes(44100, 0) != 0 {
		t.Error("voiceBufferBytes with zero channels should be 0")
	}
}

func TestMaxPCMBytes(t *testing.T) {
	if maxPCMBytes(0) != 0 {
		t.Error("maxPCMBytes(0) should be 0")
	}
	want := int64(44100) * mp3Channels * 2 * int64(maxSoundDuration/time.Second)
	if got := maxPCMBytes(44100); got != want {
		t.Fatalf("maxPCMBytes(44100) = %d, want %d", got, want)
	}
}

func TestDecodeFileMissing(t *testing.T) {
	if _, _, _, err := decodeFile(filepath.Join(t.TempDir(), "nope.mp3")); err == nil {
		t.Fatal("decodeFile on a missing file returned nil error")
	}
}

func TestWithVolumeClamps(t *testing.T) {
	cfg := config{volume: 0}
	WithVolume(-5)(&cfg)
	if cfg.volume != 0 {
		t.Fatalf("volume = %v, want 0", cfg.volume)
	}
	WithVolume(5)(&cfg)
	if cfg.volume != 1 {
		t.Fatalf("volume = %v, want 1", cfg.volume)
	}
}
