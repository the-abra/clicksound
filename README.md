# ClickSound

<img style="padding-top: 0;" width="96" align="right" src="https://static.wikia.nocookie.net/minecraft/images/6/63/Note_Block_animate.gif" alt="ICON">

ClickSound is a simple Linux application that detects keyboard events and plays a sound (e.g., `pop.mp3`) every time a key is pressed. It runs in the background and provides an interactive way to add sound feedback to your typing experience.

**Note to Developers:** This is a Go port optimized for Linux. It reads keyboard events directly from `/dev/input/event*` and plays audio via ALSA using the [Oto](https://github.com/ebitengine/oto) engine — no PulseAudio or PipeWire dependency required.

## Features

- Real-time keyboard event detection via Linux evdev
- Concurrent sound playback — no delay between key presses
- Colorized CLI output with Unicode symbols
- 7 preinstalled sound themes (mechanical, bubble pop, steampunk, etc.)
- Support for external sound files via absolute path
- Runs as a background daemon
- Zero-latency — sounds are pre-decoded into memory


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
mv clicksound /usr/share/clicksound
chmod +x /usr/share/clicksound/clicksound
ln -s /usr/share/clicksound/clicksound /usr/local/bin/clicksound
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
│   │   └── player.go        # MP3 decoding and playback
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
| `clicksound list` | List all preinstalled sounds |
| `clicksound start <sound>` | Start with a preinstalled sound |
| `clicksound start /path/to/file.mp3` | Start with an external sound file |
| `clicksound stop` | Stop the background daemon |
| `clicksound help` | Show help message |
| `clicksound update` | Check for updates |

### License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.
