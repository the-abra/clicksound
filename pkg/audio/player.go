package audio

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/ebitengine/oto/v3"
	"github.com/hajimehoshi/go-mp3"
)

type Player struct {
	SoundFile  string
	audioBytes []byte
	otoCtx     *oto.Context
	sampleRate int
	channels   int
}

func NewPlayer(soundFile string) (*Player, error) {
	// Read the entire file into memory as bytes
	fileBytes, err := os.ReadFile(soundFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	// Decode the MP3 metadata to get SampleRate and Channels
	decoder, err := mp3.NewDecoder(bytes.NewReader(fileBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to decode mp3: %w", err)
	}

	sampleRate := decoder.SampleRate()
	// go-mp3 always outputs 16-bit stereo (2 channels) or mono (1 channel)
	// but ebitengine/oto assumes number of channels. We grab it from length logic or
	// known constants. Actually, go-mp3 always assumes 2 channels for its internal Output size,
	// but let's just use 2 channels by default.
	channels := 2

	// Read decoded PCM bytes into memory
	audioBytes, err := io.ReadAll(decoder)
	if err != nil {
		return nil, fmt.Errorf("failed to read decoded mp3 data: %w", err)
	}

	// Initialize Oto context
	op := &oto.NewContextOptions{
		SampleRate:   sampleRate,
		ChannelCount: channels,
		Format:       oto.FormatSignedInt16LE,
	}

	otoCtx, ready, err := oto.NewContext(op)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize oto context: %w", err)
	}

	// Wait for context to be ready
	<-ready

	return &Player{
		SoundFile:  soundFile,
		audioBytes: audioBytes,
		otoCtx:     otoCtx,
		sampleRate: sampleRate,
		channels:   channels,
	}, nil
}

// Play launches a new Oto player to play the pre-loaded PCM data asynchronously.
func (p *Player) Play() {
	go func() {
		// We create a new reader from the in-memory PCM bytes for every click
		reader := bytes.NewReader(p.audioBytes)
		player := p.otoCtx.NewPlayer(reader)

		player.Play()
		// Wait until the sound finishes playing
		for player.IsPlaying() {
			time.Sleep(time.Millisecond)
		}
		player.Close()
	}()
}
