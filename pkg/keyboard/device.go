// Package keyboard discovers Linux input devices that behave like keyboards and
// streams their key-press events using the evdev interface.
//
// Device discovery is capability based: instead of guessing from the device
// name, it asks the kernel (EVIOCGBIT) which event types and key codes a device
// supports. A device that reports the common typing keys (KEY_A, KEY_Z, …) is a
// keyboard; a device that only reports BTN_LEFT or KEY_POWER is not. Name
// matching is kept only as a fallback for when /dev/input is not readable.
package keyboard

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"unsafe"

	"golang.org/x/sys/unix"
)

// evdev event types (linux/input-event-codes.h).
const (
	evKey = 0x01
)

// Device name heuristics treat these substrings as keyboard-like.
var keyboardNameHints = []string{"keyboard", "kbd", "keys"}

// Key codes used to recognise a typing device. Values come from
// linux/input-event-codes.h and are stable across architectures.
const (
	keyEsc       = 1
	key9         = 10
	key0         = 11
	keyBackspace = 14
	keyTab       = 15
	keyEnter     = 28
	keyLeftCtrl  = 29
	keyA         = 30
	keyLeftShift = 42
	keyZ         = 44
	keyLeftAlt   = 56
	keySpace     = 57
)

// typingKeyWeights scores how strongly a key code indicates a keyboard. Alpha
// keys are weighted highest because they cannot be produced by a mouse, power
// button or media remote.
var typingKeyWeights = []struct {
	code   uint16
	weight int
}{
	{keyA, 4},
	{keyZ, 4},
	{keyEnter, 2},
	{keySpace, 2},
	{keyLeftShift, 1},
	{keyLeftCtrl, 1},
	{keyLeftAlt, 1},
	{keyTab, 1},
	{keyBackspace, 1},
	{keyEsc, 1},
	{key0, 1},
	{key9, 1},
}

// minKeyboardScore is the capability score at which a device is considered a
// keyboard even without a helpful name.
const minKeyboardScore = 5

// evMax and keyMax bound the capability bitmaps queried from the kernel
// (EV_MAX and KEY_MAX in linux/input.h).
const (
	evMax  = 0x1f
	keyMax = 0x2ff
)

// Device describes a discovered input event device.
type Device struct {
	// Path is the /dev/input/eventN path.
	Path string
	// Name is the human readable device name reported by the kernel.
	Name string
	// Score is the capability-based confidence that this is a keyboard.
	Score int
	// Keyboard reports whether the device is believed to be a keyboard.
	Keyboard bool
	// keys is the EV_KEY capability bitmap; nil when unavailable.
	keys []byte
}

// hasKey reports whether the device advertises the given key code.
func (d Device) hasKey(code uint16) bool {
	idx := int(code) / 8
	return idx < len(d.keys) && d.keys[idx]&(1<<(uint(code)%8)) != 0
}

// FindDevice returns the most keyboard-like device available.
func FindDevice() (string, error) {
	devices, err := ListDevices()
	if err != nil {
		return "", err
	}
	for _, d := range devices {
		if d.Keyboard && d.Score > 0 {
			return d.Path, nil
		}
	}
	return "", errors.New("no keyboard-like input device found (read access to /dev/input may require root or the 'input' group)")
}

// FindKeyboards returns every device that looks like a keyboard, best first.
// Listening to all of them means an external keyboard works even when a laptop
// keyboard is also present.
func FindKeyboards() ([]string, error) {
	devices, err := ListDevices()
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, d := range devices {
		if d.Keyboard && d.Score > 0 {
			paths = append(paths, d.Path)
		}
	}
	if len(paths) == 0 {
		return nil, errors.New("no keyboard-like input device found (read access to /dev/input may require root or the 'input' group)")
	}
	return paths, nil
}

// ListDevices enumerates the input event devices, probing their capabilities
// where permissions allow. The result is ordered best-first.
func ListDevices() ([]Device, error) {
	entries, err := os.ReadDir("/dev/input")
	if err != nil {
		return nil, fmt.Errorf("read /dev/input: %w", err)
	}

	devices := make([]Device, 0, len(entries))
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), "event") {
			continue
		}
		devices = append(devices, probe(filepath.Join("/dev/input", e.Name())))
	}

	sort.SliceStable(devices, func(i, j int) bool {
		a, b := devices[i], devices[j]
		if a.Keyboard != b.Keyboard {
			return a.Keyboard
		}
		if a.Score != b.Score {
			return a.Score > b.Score
		}
		return a.Path < b.Path
	})
	return devices, nil
}

// probe inspects a single device path and returns what is known about it.
func probe(path string) Device {
	d := Device{Path: path, Name: sysfsName(path)}

	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		// Without read access we cannot query capabilities; fall back to the
		// kernel-provided name.
		d.Score, d.Keyboard = classify(d.Name, nil)
		return d
	}
	defer unix.Close(fd)

	if name, err := nameFromFD(fd); err == nil && name != "" {
		d.Name = name
	}

	// Reject devices that do not emit key events at all (e.g. mice, sensors).
	if types, err := eventTypes(fd); err == nil && types&(1<<evKey) == 0 {
		return d
	}

	keys, err := keyBitmap(fd)
	if err != nil {
		d.Score, d.Keyboard = classify(d.Name, nil)
		return d
	}

	d.keys = keys
	d.Score, d.Keyboard = classify(d.Name, keys)
	return d
}

// classify derives the keyboard score and flag from a device name and its
// EV_KEY bitmap. keys may be nil when capabilities are unavailable, in which
// case only the name is used.
func classify(name string, keys []byte) (score int, keyboard bool) {
	nameKeyboard := looksLikeKeyboard(name)
	if keys == nil {
		return nameScore(nameKeyboard), nameKeyboard
	}

	d := Device{keys: keys}
	score = scoreKeys(keys)
	if nameKeyboard {
		score += 3
	}
	// Trust the alpha-key capability above everything else: a "keyboard" that
	// cannot type is not a keyboard.
	keyboard = (d.hasKey(keyA) && d.hasKey(keyZ)) || (nameKeyboard && score >= minKeyboardScore)
	return score, keyboard
}

// scoreKeys computes the keyboard confidence from an EV_KEY bitmap.
func scoreKeys(keys []byte) int {
	score := 0
	for _, k := range typingKeyWeights {
		idx := int(k.code) / 8
		if idx < len(keys) && keys[idx]&(1<<(uint(k.code)%8)) != 0 {
			score += k.weight
		}
	}
	return score
}

// looksLikeKeyboard reports whether a device name hints at a keyboard.
func looksLikeKeyboard(name string) bool {
	lower := strings.ToLower(name)
	for _, hint := range keyboardNameHints {
		if strings.Contains(lower, hint) {
			return true
		}
	}
	return false
}

func nameScore(nameKeyboard bool) int {
	if nameKeyboard {
		return 3
	}
	return 0
}

// sysfsName reads the device name from sysfs, which works without /dev access.
func sysfsName(path string) string {
	base := filepath.Base(path)
	data, err := os.ReadFile(filepath.Join("/sys/class/input", base, "device/name"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// nameFromFD asks the kernel for the device name via EVIOCGNAME.
func nameFromFD(fd int) (string, error) {
	buf := make([]byte, 256)
	if err := ioctl(fd, eviocgname(uintptr(len(buf))), unsafe.Pointer(&buf[0])); err != nil {
		return "", err
	}
	if i := bytes.IndexByte(buf, 0); i >= 0 {
		buf = buf[:i]
	}
	return string(bytes.TrimSpace(buf)), nil
}

// eventTypes returns the supported EV_* bitmap.
func eventTypes(fd int) (uint32, error) {
	size := (evMax + 1 + 7) / 8
	bits := make([]byte, size)
	if err := ioctl(fd, eviocgbit(0, uintptr(size)), unsafe.Pointer(&bits[0])); err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint32(bits), nil
}

// keyBitmap returns the supported KEY_* bitmap.
func keyBitmap(fd int) ([]byte, error) {
	size := (keyMax + 1 + 7) / 8
	bits := make([]byte, size)
	if err := ioctl(fd, eviocgbit(evKey, uintptr(size)), unsafe.Pointer(&bits[0])); err != nil {
		return nil, err
	}
	return bits, nil
}

// The _IOC* helpers mirror <linux/ioctl.h> and <linux/input.h>.
const iocRead = 2

func ioc(dir, typ, nr, size uintptr) uintptr {
	return (dir << 30) | (size << 16) | (typ << 8) | nr
}

func eviocgbit(ev, length uintptr) uintptr {
	return ioc(iocRead, 'E', 0x20+ev, length)
}

func eviocgname(length uintptr) uintptr {
	return ioc(iocRead, 'E', 0x06, length)
}

func ioctl(fd int, req uintptr, arg unsafe.Pointer) error {
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), req, uintptr(arg))
	if errno != 0 {
		return errno
	}
	return nil
}

// inputEvent is the kernel's struct input_event. Using unix.Timeval keeps the
// layout correct on both 32-bit and 64-bit kernels instead of hardcoding 16 or
// 24 byte records.
type inputEvent struct {
	Time  unix.Timeval
	Type  uint16
	Code  uint16
	Value int32
}

// Listen reads key-press events from devicePath until ctx is cancelled.
//
// Only EV_KEY events with value 1 (key press) are reported; releases (0) and
// auto-repeat (2) are ignored so each physical press produces one sound.
func Listen(ctx context.Context, devicePath string, onKey func(code uint16)) error {
	file, err := os.Open(devicePath)
	if err != nil {
		return fmt.Errorf("open %s: %w", devicePath, err)
	}
	return listenReader(ctx, file, devicePath, onKey)
}

// readBatch is the number of input_event records read per syscall. A single
// key produces several events (press, repeats, release), so batching cuts the
// syscall count without any per-event allocation.
const readBatch = 64

// listenReader is the testable core of Listen: it decodes input_event records
// from r until r fails or ctx is cancelled.
func listenReader(ctx context.Context, r io.ReadCloser, name string, onKey func(code uint16)) error {
	defer r.Close()

	// Closing the reader unblocks a pending Read when the context is done.
	stopped := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = r.Close()
		case <-stopped:
		}
	}()
	defer close(stopped)

	eventSize := int(unsafe.Sizeof(inputEvent{}))
	buf := make([]byte, eventSize*readBatch)
	// carried is the number of bytes of an incomplete trailing record that were
	// moved to the front of buf for the next read.
	carried := 0

	for {
		n, err := r.Read(buf[carried:])
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, fs.ErrClosed) {
				return nil
			}
			return fmt.Errorf("read %s: %w", name, err)
		}

		total := carried + n
		consumed := parseEvents(buf[:total], onKey)
		// Keep any incomplete trailing record for the next read.
		carried = copy(buf, buf[consumed:total])
	}
}

// parseEvents invokes onKey for every complete key-press record in data and
// returns the number of bytes consumed. A trailing partial record is left for
// the caller to prepend to the next read.
func parseEvents(data []byte, onKey func(code uint16)) int {
	size := int(unsafe.Sizeof(inputEvent{}))
	consumed := 0
	for consumed+size <= len(data) {
		ev := decodeEvent(data[consumed:])
		consumed += size
		if ev.Type == evKey && ev.Value == 1 && onKey != nil {
			onKey(ev.Code)
		}
	}
	return consumed
}

// eventPayloadSize is sizeof(type uint16 + code uint16 + value int32). The
// payload always sits at the end of the record, whatever the timeval size is.
const eventPayloadSize = 8

// decodeEvent decodes one input_event record without copying it into a struct.
func decodeEvent(b []byte) inputEvent {
	off := int(unsafe.Sizeof(inputEvent{})) - eventPayloadSize
	return inputEvent{
		Type:  binary.LittleEndian.Uint16(b[off : off+2]),
		Code:  binary.LittleEndian.Uint16(b[off+2 : off+4]),
		Value: int32(binary.LittleEndian.Uint32(b[off+4 : off+8])),
	}
}

// ListenAll listens on every device in paths concurrently. If any listener
// fails, the others are stopped and the error is returned. It returns nil once
// ctx is cancelled.
func ListenAll(ctx context.Context, paths []string, onKey func(code uint16)) error {
	if len(paths) == 0 {
		return errors.New("no input devices to listen on")
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup
	errCh := make(chan error, len(paths))
	for _, path := range paths {
		wg.Add(1)
		go func(path string) {
			defer wg.Done()
			if err := Listen(ctx, path, onKey); err != nil {
				errCh <- err
				cancel() // stop the sibling listeners
			}
		}(path)
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	<-done

	// Prefer reporting a listener error over a clean shutdown.
	select {
	case err := <-errCh:
		return err
	default:
		return nil
	}
}
