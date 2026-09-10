# ClickSound

<img style="padding-top: 0;" width="96" align="right" src="https://static.wikia.nocookie.net/minecraft/images/6/63/Note_Block_animate.gif" alt="ICON">

ClickSound is a simple Linux application that detects keyboard events and plays a sound (e.g., `pop.mp3`) every time a key is pressed. It runs in the background and provides an interactive way to add sound feedback to your typing experience.

**Note to Developers:** This is a Go port optimized for Linux. It reads keyboard events directly from `/dev/input/event*` and plays audio via ALSA using the [Oto](https://github.com/ebitengine/oto) engine — no PulseAudio or PipeWire dependency required.

## Features

- **Capability-based keyboard detection** — devices are identified by the key
  codes they report (`EVIOCGBIT`), not by guessing from their name. Mice, power
  buttons and media sensors are ignored, and all connected keyboards are used
  so an external keyboard works alongside a laptop one.
- Correct evdev parsing on both 32-bit and 64-bit kernels (`struct input_event`
  is 16 or 24 bytes) — only key presses trigger a sound, not auto-repeat or
  release events.
- **Low-latency, allocation-free playback** — sounds are pre-decoded into memory
  and played through a bounded pool of reusable voices. Each voice is
  right-sized (50 ms of audio instead of Oto's 500 ms default), so rapid typing
  spawns no goroutines and allocates nothing on the hot path.
- **Bounded memory** — decoding is streamed and capped (2 minutes of audio),
  and the input listener reads events in fixed-size batches, so neither a huge
  sound file nor a burst of key events can grow memory without limit.
- Background daemon with a PID file and graceful `SIGTERM`/`SIGINT` shutdown.
- Colorized, TTY-aware CLI that honors `NO_COLOR`.
- 7 preinstalled sound themes (mechanical, bubble pop, steampunk, etc.)
- Volume control and explicit input-device selection.
- Support for external sound files via absolute path.


## Requirements

- **Linux** (kernel with evdev support)
- **Go 1.25+** (for building from source)
- **ALSA dev headers** — `sudo apt install libasound2-dev` (Debian/Ubuntu) or `sudo dnf install alsa-lib-devel` (Fedora)
- **Root access** — required to read `/dev/input/event*` devices (or use `pkexec`) (or use `sudo usermod -aG input $USER`)

## Installation

### Build from Source

```bash
git clone https://github.com/the-abra/clicksound.git
cd clicksound
go build -o clicksound .
```

## Usage

### CLI Setup (root privileges may be required)

```bash
ln -s $(pwd)/clicksound /usr/local/bin/clicksound
clicksound help
```

OR

```bash
mv clicksound /usr/local/share/clicksound
chmod +x /usr/local/share/clicksound/clicksound
ln -s /usr/local/share/clicksound/clicksound /usr/local/bin/clicksound
clicksound help
```

### File Structure

```
clicksound/
├── main.go                  # Entry point
├── cmd/
│   └── cli.go               # CLI commands and daemon logic
├── pkg/
│   ├── audio/
│   │   └── player.go        # MP3 decoding and voice-pool playback
│   ├── daemon/
│   │   └── daemon.go        # PID file, state and log paths
│   ├── keyboard/
│   │   └── device.go        # Keyboard detection and event listener
│   └── logger/
│       └── logger.go        # Colorized TTY-aware logger
├── sounds/                  # Preinstalled sound themes
│   ├── bubble-pop.mp3
│   ├── mech1.mp3
│   ├── mechsoft1.mp3
│   ├── mechsoft2.mp3
│   ├── pop.mp3
│   ├── single.mp3
│   └── steampunk.mp3
└── .github/workflows/
    └── CI.yml               # GitHub Actions CI pipeline
```


### Commands

| Command | Description |
|---|---|
| `clicksound list` | List preinstalled sounds (numbered, active one marked) |
| `clicksound start <sound>` | Start/switch by name, list number, or file path |
| `clicksound stop` | Stop the background daemon |
| `clicksound restart [sound]` | Restart the daemon, optionally with a new sound |
| `clicksound status` | Show what is running (pid, sound, device) |
| `clicksound devices` | List input devices and detected keyboards |
| `clicksound logs` | Show recent daemon output |
| `clicksound update` | Check for a newer release |
| `clicksound version` | Print the version |
| `clicksound help` | Show the help message |

### Options

| Option | Description |
|---|---|
| `--volume, -v <0-100>` | Playback volume (default `100`) |
| `--device, -d <path\|all>` | Listen on a specific `/dev/input/eventN` device, or all detected keyboards (default: auto) |

### Examples

```bash
clicksound list                      # see the numbered themes
clicksound start pop                 # start with a bundled theme
clicksound start 1 --volume 60       # start the first theme at 60% volume
clicksound start ~/my-click.mp3      # use your own MP3
clicksound start mech1 --device /dev/input/event5
clicksound status                    # what is running?
clicksound devices                   # which device will be used?
clicksound stop
```

## Development

```bash
# One-shot CLI commands work directly (the sounds/ directory is found
# relative to the working directory):
go run . list

# Build a real binary for the background daemon, since `go run` uses a
# temporary executable:
go build -o clicksound .

# Checks
gofmt -l .                 # formatting
go vet ./...               # vet
go test -race ./...        # tests
```

The CI workflow additionally runs `staticcheck`, verifies `go.mod` is tidy, and
publishes a test-coverage artifact and a stripped release binary.

## Performance notes

The hot path (a key press) is designed to be allocation-free and O(1):

- **Input**: evdev events are read 64 at a time into one reusable buffer and
  decoded in place (`BenchmarkParseEvents` runs at >10 GB/s with `0 allocs/op`).
  Only `EV_KEY` presses are acted on; releases and auto-repeat are dropped.
- **Playback**: the sound is decoded once at startup. A press selects a voice
  round-robin and rewinds it with `Seek`. Oto's `Play` (which allocates a
  goroutine and a channel) is only called when the voice is idle, so a retrigger
  while a click is still playing costs a mutex acquisition, not an allocation.
- **Memory**: PCM is the dominant allocation and is hard-capped. Oto's per-voice
  buffer is reduced from ~88 KB to ~9 KB per voice at 44.1 kHz stereo.

Everything the listener calls runs on the reader goroutine, so there is no
queue to overflow; when every voice is busy, the oldest is simply retriggered.

### License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.
