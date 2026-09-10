// Package audio pre-decodes MP3 files into PCM and plays them back through ALSA
// using the Oto engine.
package audio

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"math"
	"os"
	"sync/atomic"
	"time"

	"github.com/ebitengine/oto/v3"
	"github.com/hajimehoshi/go-mp3"
)

const (
	// defaultVoices is the maximum number of sounds that may overlap. Presses
	// beyond this retrigger the oldest voice instead of allocating more work.
	defaultVoices = 8
	// mp3Channels is fixed: go-mp3 always decodes to 16-bit little-endian
	// stereo, regardless of the source file.
	mp3Channels = 2
	// deviceBufferSize keeps output latency low so clicks feel immediate.
	deviceBufferSize = 40 * time.Millisecond
	// voiceBufferDuration is how much audio each oto voice buffers ahead. Oto
	// defaults to 500ms per voice; a click is only tens of milliseconds, so
	// right-sizing this cuts per-voice memory and latency by roughly an order
	// of magnitude while an in-memory source still refills instantly.
	voiceBufferDuration = 50 * time.Millisecond
	// maxSoundDuration bounds decoded PCM memory. Sounds longer than this are
	// rejected instead of risking an out-of-memory condition.
	maxSoundDuration = 2 * time.Minute
)

// Player owns a decoded sound and a fixed pool of reusable Oto voices.
//
// A voice is a single oto.Player over a bytes.Reader. Because oto players
// implement io.Seeker, an existing voice can be rewound and replayed without
// allocating a reader or a player. Play therefore runs in O(1) with zero
// per-press allocations and a bounded number of concurrent voices.
type Player struct {
	// SoundFile is the path the sound was loaded from.
	SoundFile string

	voices     []*oto.Player
	sampleRate int
	pcm        []byte
	next       atomic.Uint64
	// volume stores math.Float64bits so Volume/SetVolume are race-free.
	volume atomic.Uint64
}

type config struct {
	voices     int
	volume     float64
	bufferSize time.Duration
}

// Option customises a Player.
type Option func(*config)

// WithVoices sets the maximum number of overlapping sounds (minimum 1).
func WithVoices(n int) Option {
	return func(c *config) {
		if n > 0 {
			c.voices = n
		}
	}
}

// WithVolume sets the playback volume in the range [0, 1].
func WithVolume(v float64) Option {
	return func(c *config) { c.volume = clampVolume(v) }
}

// NewPlayer decodes soundFile and prepares the Oto context and voice pool.
func NewPlayer(soundFile string, opts ...Option) (*Player, error) {
	cfg := config{
		voices:     defaultVoices,
		volume:     1,
		bufferSize: deviceBufferSize,
	}
	for _, opt := range opts {
		opt(&cfg)
	}

	pcm, sampleRate, channels, err := decodeFile(soundFile)
	if err != nil {
		return nil, err
	}

	otoCtx, ready, err := oto.NewContext(&oto.NewContextOptions{
		SampleRate:   sampleRate,
		ChannelCount: channels,
		Format:       oto.FormatSignedInt16LE,
		BufferSize:   cfg.bufferSize,
	})
	if err != nil {
		return nil, fmt.Errorf("initialize audio: %w", err)
	}
	<-ready
	if err := otoCtx.Err(); err != nil {
		return nil, fmt.Errorf("initialize audio: %w", err)
	}

	p := &Player{
		SoundFile:  soundFile,
		sampleRate: sampleRate,
		pcm:        pcm,
	}
	p.volume.Store(math.Float64bits(cfg.volume))

	// Every voice reads the same immutable PCM slice through its own reader,
	// which is safe because the slice is never written after construction.
	voiceBuffer := voiceBufferBytes(sampleRate, channels)
	p.voices = make([]*oto.Player, 0, cfg.voices)
	for i := 0; i < cfg.voices; i++ {
		voice := otoCtx.NewPlayer(bytes.NewReader(pcm))
		voice.SetBufferSize(voiceBuffer)
		voice.SetVolume(cfg.volume)
		p.voices = append(p.voices, voice)
	}
	return p, nil
}

// Play triggers the sound. It never blocks the caller and is safe to call
// concurrently. Voices are used round-robin so rapid, overlapping presses mix,
// while the pool size bounds CPU and memory use.
//
// A new oto Player.Play() call allocates a goroutine and a channel every time.
// Seeking an already-playing voice restarts it in place, so Play only calls
// into oto when the chosen voice is idle. That keeps the common fast-typing
// path allocation-free.
func (p *Player) Play() {
	if p == nil || len(p.voices) == 0 {
		return
	}
	n := p.next.Add(1)
	voice := p.voices[(n-1)%uint64(len(p.voices))]

	// Seek resets the internal buffer and, when the voice is already playing,
	// restarts it from the beginning. Errors are impossible for a bytes.Reader.
	_, _ = voice.Seek(0, io.SeekStart)
	if !voice.IsPlaying() {
		voice.Play()
	}
}

// SetVolume adjusts the playback volume in the range [0, 1].
func (p *Player) SetVolume(v float64) {
	v = clampVolume(v)
	p.volume.Store(math.Float64bits(v))
	for _, voice := range p.voices {
		voice.SetVolume(v)
	}
}

// Volume returns the current playback volume.
func (p *Player) Volume() float64 { return math.Float64frombits(p.volume.Load()) }

// Duration is the length of the decoded sound.
func (p *Player) Duration() time.Duration {
	return pcmDuration(len(p.pcm), p.sampleRate, mp3Channels)
}

func clampVolume(v float64) float64 {
	switch {
	case v < 0:
		return 0
	case v > 1:
		return 1
	default:
		return v
	}
}

// decodeFile streams and decodes an MP3 file into raw 16-bit little-endian PCM.
// The file is read through a buffered reader so the compressed data is never
// held in memory in full, and decoding is capped to maxSoundDuration so a large
// file cannot exhaust memory.
func decodeFile(path string) (pcm []byte, sampleRate, channels int, err error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("open sound file: %w", err)
	}
	defer file.Close()

	decoder, err := mp3.NewDecoder(bufio.NewReaderSize(file, 64*1024))
	if err != nil {
		return nil, 0, 0, fmt.Errorf("decode mp3: %w", err)
	}

	sampleRate = decoder.SampleRate()
	limit := maxPCMBytes(sampleRate)
	pcm, err = io.ReadAll(io.LimitReader(decoder, limit+1))
	if err != nil {
		return nil, 0, 0, fmt.Errorf("read decoded audio: %w", err)
	}
	if int64(len(pcm)) > limit {
		return nil, 0, 0, fmt.Errorf("sound is too long (limit %s)", maxSoundDuration)
	}
	return pcm, sampleRate, mp3Channels, nil
}

// maxPCMBytes is the decoded size of maxSoundDuration of 16-bit stereo audio.
func maxPCMBytes(sampleRate int) int64 {
	if sampleRate <= 0 {
		return 0
	}
	return int64(sampleRate) * mp3Channels * 2 * int64(maxSoundDuration/time.Second)
}

// voiceBufferBytes returns the per-voice oto buffer size in bytes, aligned to a
// whole frame.
func voiceBufferBytes(sampleRate, channels int) int {
	bytesPerFrame := channels * 2
	if sampleRate <= 0 || bytesPerFrame <= 0 {
		return 0
	}
	n := sampleRate * bytesPerFrame * int(voiceBufferDuration/time.Millisecond) / 1000
	n = n / bytesPerFrame * bytesPerFrame
	if n < bytesPerFrame {
		n = bytesPerFrame
	}
	return n
}

// pcmDuration converts a 16-bit PCM byte count to a duration.
func pcmDuration(n, sampleRate, channels int) time.Duration {
	if n <= 0 || sampleRate <= 0 || channels <= 0 {
		return 0
	}
	bytesPerFrame := channels * 2 // 16-bit samples
	return time.Duration(int64(n) * int64(time.Second) / int64(sampleRate*bytesPerFrame))
}
