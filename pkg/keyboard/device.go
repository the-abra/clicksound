package keyboard

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// FindDevice looks through /sys/class/input/event*/device/name
// to locate a device with 'keyboard' or 'kbd' in its name.
func FindDevice() (string, error) {
	files, err := os.ReadDir("/sys/class/input")
	if err != nil {
		return "", err
	}

	var fallback string

	for _, f := range files {
		if strings.HasPrefix(f.Name(), "event") {
			nameBytes, err := os.ReadFile(filepath.Join("/sys/class/input", f.Name(), "device/name"))
			if err != nil {
				continue
			}
			name := strings.ToLower(strings.TrimSpace(string(nameBytes)))
			// Commonly keyboard device names have "keyboard" or "kbd" 
			if strings.Contains(name, "keyboard") || strings.Contains(name, "kbd") {
				return filepath.Join("/dev/input", f.Name()), nil
			}
			// Sometimes it has event but no 'keyboard' in name. We store the first one just in case.
			if fallback == "" {
				fallback = filepath.Join("/dev/input", f.Name())
			}
		}
	}

	return "", fmt.Errorf("no keyboard device found")
}

// Listen reads events directly from the input device file descriptor indefinitely.
// Note: This implementation assumes a 64-bit Linux kernel where struct input_event
// is 24 bytes (16 bytes timeval + 2 bytes type + 2 bytes code + 4 bytes value).
func Listen(devicePath string, onKeyPress func()) error {
	file, err := os.Open(devicePath)
	if err != nil {
		return err
	}
	defer file.Close()

	buf := make([]byte, 24)
	for {
		n, err := file.Read(buf)
		if err != nil {
			return err
		}
		// In case we actually read a 24-byte event struct
		if n == 24 {
			eventType := binary.LittleEndian.Uint16(buf[16:18])
			eventValue := binary.LittleEndian.Uint32(buf[20:24])

			// EV_KEY type is 1, value 1 means key press.
			if eventType == 1 && eventValue == 1 {
				onKeyPress()
			}
		} else if n == 16 {
			// fallback for 32-bit systems (8 bytes timeval + 2 + 2 + 4 = 16)
			// eventType at 8:10, eventValue at 12:16
			eventType := binary.LittleEndian.Uint16(buf[8:10])
			eventValue := binary.LittleEndian.Uint32(buf[12:16])
			if eventType == 1 && eventValue == 1 {
				onKeyPress()
			}
		}
	}
}
