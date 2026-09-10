package keyboard

import (
	"bytes"
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
	"time"
	"unsafe"
)

// keyBitmapFor builds an EV_KEY capability bitmap containing the given codes.
func keyBitmapFor(codes ...uint16) []byte {
	size := (keyMax + 1 + 7) / 8
	bits := make([]byte, size)
	for _, code := range codes {
		bits[int(code)/8] |= 1 << (uint(code) % 8)
	}
	return bits
}

func TestScoreKeys(t *testing.T) {
	all := keyBitmapFor(keyA, keyZ, keyEnter, keySpace, keyLeftShift, keyLeftCtrl, keyLeftAlt, keyTab, keyBackspace, keyEsc, key0, key9)
	if got, want := scoreKeys(all), 4+4+2+2+1+1+1+1+1+1+1+1; got != want {
		t.Fatalf("scoreKeys(all) = %d, want %d", got, want)
	}

	// A mouse with only button keys scores zero.
	if got := scoreKeys(keyBitmapFor(272, 273)); got != 0 {
		t.Fatalf("scoreKeys(mouse) = %d, want 0", got)
	}

	// A power button exposes just KEY_POWER (116).
	if got := scoreKeys(keyBitmapFor(116)); got != 0 {
		t.Fatalf("scoreKeys(power button) = %d, want 0", got)
	}
}

func TestClassify(t *testing.T) {
	tests := []struct {
		name         string
		keys         []byte
		wantKeyboard bool
	}{
		{
			name:         "capable keyboard with generic name",
			keys:         keyBitmapFor(keyA, keyZ, keyEnter, keySpace, keyLeftCtrl, keyLeftShift),
			wantKeyboard: true,
		},
		{
			name:         "mouse is not a keyboard",
			keys:         keyBitmapFor(272, 273, 274),
			wantKeyboard: false,
		},
		{
			name:         "power button is not a keyboard",
			keys:         keyBitmapFor(116),
			wantKeyboard: false,
		},
		{
			name:         "name-only fallback when caps unknown",
			keys:         nil,
			wantKeyboard: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			name := "Generic USB Device"
			if tt.keys == nil {
				name = "AT Translated Set 2 keyboard"
			}
			_, keyboard := classify(name, tt.keys)
			if keyboard != tt.wantKeyboard {
				t.Fatalf("classify(%q) keyboard = %v, want %v", name, keyboard, tt.wantKeyboard)
			}
		})
	}
}

func TestClassifyPrecedence(t *testing.T) {
	// A device whose name says "keyboard" but whose capabilities clearly
	// belong to a mouse must still be rejected.
	_, keyboard := classify("Gaming Mouse Keyboard Emulator", keyBitmapFor(272, 273))
	if keyboard {
		t.Fatal("capability check should override a misleading device name")
	}

	// A capable keyboard with no useful name must still be accepted.
	_, keyboard = classify("USB-HID Device", keyBitmapFor(keyA, keyZ, keyEnter, keySpace, keyLeftShift))
	if !keyboard {
		t.Fatal("capable keyboard with generic name should be detected")
	}
}

func TestLooksLikeKeyboard(t *testing.T) {
	for _, name := range []string{"AT Translated Set 2 keyboard", "USB KBD", "apple Internal Keys"} {
		if !looksLikeKeyboard(name) {
			t.Errorf("looksLikeKeyboard(%q) = false, want true", name)
		}
	}
	for _, name := range []string{"Power Button", "Video Bus", "Logitech Mouse"} {
		if looksLikeKeyboard(name) {
			t.Errorf("looksLikeKeyboard(%q) = true, want false", name)
		}
	}
}

func TestEviocgbitRequest(t *testing.T) {
	// EVIOCGBIT(0, 8) and EVIOCGBIT(EV_KEY, 8) are well-known ioctl numbers.
	if got, want := eviocgbit(0, 8), uintptr(0x80084520); got != want {
		t.Errorf("eviocgbit(0, 8) = %#x, want %#x", got, want)
	}
	if got, want := eviocgbit(evKey, 8), uintptr(0x80084521); got != want {
		t.Errorf("eviocgbit(EV_KEY, 8) = %#x, want %#x", got, want)
	}
}

func TestListenParsesEvents(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events")

	var buf bytes.Buffer
	events := []inputEvent{
		{Type: evKey, Code: keyA, Value: 1}, // press   -> reported
		{Type: evKey, Code: keyA, Value: 0}, // release -> ignored
		{Type: evKey, Code: keyA, Value: 2}, // repeat  -> ignored
		{Type: 0x02, Code: 0, Value: 1},     // EV_REL  -> ignored
		{Type: evKey, Code: keyZ, Value: 1}, // press   -> reported
	}
	for _, ev := range events {
		if err := binary.Write(&buf, binary.LittleEndian, ev); err != nil {
			t.Fatalf("encode event: %v", err)
		}
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatalf("write events: %v", err)
	}

	var pressed []uint16
	err := Listen(context.Background(), path, func(code uint16) {
		pressed = append(pressed, code)
	})
	// Reaching EOF without cancellation is an error, but the events must still
	// have been delivered.
	if err == nil {
		t.Fatal("Listen returned nil at EOF, want error")
	}
	if want := []uint16{keyA, keyZ}; len(pressed) != len(want) {
		t.Fatalf("pressed = %v, want %v", pressed, want)
	} else {
		for i := range want {
			if pressed[i] != want[i] {
				t.Fatalf("pressed = %v, want %v", pressed, want)
			}
		}
	}
}

func TestDecodeEventUsesTrailingPayload(t *testing.T) {
	size := int(unsafe.Sizeof(inputEvent{}))
	b := make([]byte, size)
	binary.LittleEndian.PutUint16(b[size-8:], evKey)
	binary.LittleEndian.PutUint16(b[size-6:], keyA)
	binary.LittleEndian.PutUint32(b[size-4:], 1)

	ev := decodeEvent(b)
	if ev.Type != evKey || ev.Code != keyA || ev.Value != 1 {
		t.Fatalf("decodeEvent = %+v, want type=%d code=%d value=1", ev, evKey, keyA)
	}
}

func TestParseEventsLeavesPartialRecord(t *testing.T) {
	size := int(unsafe.Sizeof(inputEvent{}))
	// One complete press followed by half of another record.
	data := make([]byte, size+size/2)
	binary.LittleEndian.PutUint16(data[size-8:], evKey)
	binary.LittleEndian.PutUint16(data[size-6:], keyA)
	binary.LittleEndian.PutUint32(data[size-4:], 1)

	var pressed []uint16
	consumed := parseEvents(data, func(code uint16) { pressed = append(pressed, code) })
	if consumed != size {
		t.Fatalf("consumed = %d, want %d", consumed, size)
	}
	if len(pressed) != 1 || pressed[0] != keyA {
		t.Fatalf("pressed = %v, want [%d]", pressed, keyA)
	}
}

func TestParseEventsEmpty(t *testing.T) {
	if got := parseEvents(nil, func(uint16) {}); got != 0 {
		t.Fatalf("consumed = %d, want 0", got)
	}
}

func TestParseEventsMany(t *testing.T) {
	size := int(unsafe.Sizeof(inputEvent{}))
	const count = 5
	data := make([]byte, size*count)
	for i := 0; i < count; i++ {
		off := i*size + size - 8
		binary.LittleEndian.PutUint16(data[off:], evKey)
		binary.LittleEndian.PutUint16(data[off+2:], keyZ)
		binary.LittleEndian.PutUint32(data[off+4:], 1)
	}

	var pressed int
	consumed := parseEvents(data, func(uint16) { pressed++ })
	if consumed != len(data) {
		t.Fatalf("consumed = %d, want %d", consumed, len(data))
	}
	if pressed != count {
		t.Fatalf("pressed = %d, want %d", pressed, count)
	}
}

func BenchmarkParseEvents(b *testing.B) {
	size := int(unsafe.Sizeof(inputEvent{}))
	data := make([]byte, size*readBatch)
	for i := 0; i < readBatch; i++ {
		off := i*size + size - 8
		binary.LittleEndian.PutUint16(data[off:], evKey)
		binary.LittleEndian.PutUint16(data[off+2:], keyA)
		binary.LittleEndian.PutUint32(data[off+4:], 1)
	}

	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		parseEvents(data, func(uint16) {})
	}
}

func TestListenAllNoDevices(t *testing.T) {
	if err := ListenAll(context.Background(), nil, func(uint16) {}); err == nil {
		t.Fatal("ListenAll with no devices returned nil error")
	}
}

func TestListenAllFailsFast(t *testing.T) {
	// A regular file reaches EOF immediately, so Listen returns an error and
	// ListenAll must propagate it instead of blocking forever.
	path := filepath.Join(t.TempDir(), "empty")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- ListenAll(context.Background(), []string{path, path}, func(uint16) {}) }()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("ListenAll = nil error, want failure")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ListenAll did not fail fast")
	}
}

func TestListenContextCancellation(t *testing.T) {
	// A pipe with no writer keeps Read blocked; cancelling the context must
	// unblock it and make Listen return nil.
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	defer w.Close()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- listenReader(ctx, r, "pipe", func(uint16) {}) }()

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("listenReader after cancel = %v, want nil", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("listenReader did not stop after cancellation")
	}
}
