package protocol

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// TestEncode tests the Encode function with various inputs
func TestEncode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		commandID uint32
		payload   []byte
		wantError bool
	}{
		{
			name:      "simple command with payload",
			commandID: 0x01,
			payload:   []byte("hello"),
			wantError: false,
		},
		{
			name:      "command with empty payload",
			commandID: 0x100,
			payload:   []byte{},
			wantError: false,
		},
		{
			name:      "command with nil payload",
			commandID: 0x200,
			payload:   nil,
			wantError: false,
		},
		{
			name:      "max command ID",
			commandID: 0xFFFFFFFF,
			payload:   []byte("test"),
			wantError: false,
		},
		{
			name:      "zero command ID",
			commandID: 0,
			payload:   []byte("zero"),
			wantError: false,
		},
		{
			name:      "binary payload",
			commandID: 0x42,
			payload:   []byte{0x00, 0xFF, 0x01, 0xFE},
			wantError: false,
		},
		{
			name:      "payload at max size",
			commandID: 0x01,
			payload:   make([]byte, MaxPayloadSize),
			wantError: false,
		},
		{
			name:      "payload exceeds max size",
			commandID: 0x01,
			payload:   make([]byte, MaxPayloadSize+1),
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result, err := Encode(tt.commandID, tt.payload)

			if (err != nil) != tt.wantError {
				t.Errorf("Encode() error = %v, wantError %v", err, tt.wantError)
				return
			}

			if tt.wantError {
				return
			}

			// Verify header size
			expectedLen := HeaderSize + len(tt.payload)
			if len(result) != expectedLen {
				t.Errorf("result length = %d, want %d", len(result), expectedLen)
			}

			// Verify command ID in header
			gotCmd := binary.BigEndian.Uint32(result[:HeaderSize])
			if gotCmd != tt.commandID {
				t.Errorf("encoded command ID = %v, want %v", gotCmd, tt.commandID)
			}

			// Verify payload
			gotPayload := result[HeaderSize:]
			if !bytes.Equal(gotPayload, tt.payload) {
				t.Errorf("encoded payload = %v, want %v", gotPayload, tt.payload)
			}
		})
	}
}

// TestDecode tests the Decode function with various inputs
func TestDecode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		data        []byte
		wantCmd     uint32
		wantPayload []byte
		wantError   bool
	}{
		{
			name:        "valid data with payload",
			data:        []byte{0x00, 0x00, 0x00, 0x01, 0x68, 0x65, 0x6C, 0x6C, 0x6F}, // cmd=1, payload="hello"
			wantCmd:     0x01,
			wantPayload: []byte("hello"),
			wantError:   false,
		},
		{
			name:        "valid data with empty payload",
			data:        []byte{0x00, 0x00, 0x01, 0x00}, // cmd=256, no payload
			wantCmd:     0x0100,
			wantPayload: []byte{},
			wantError:   false,
		},
		{
			name:        "max command ID",
			data:        []byte{0xFF, 0xFF, 0xFF, 0xFF, 0x41}, // cmd=max, payload="A"
			wantCmd:     0xFFFFFFFF,
			wantPayload: []byte{0x41},
			wantError:   false,
		},
		{
			name:        "zero command ID",
			data:        []byte{0x00, 0x00, 0x00, 0x00, 0x74, 0x65, 0x73, 0x74}, // cmd=0, payload="test"
			wantCmd:     0,
			wantPayload: []byte("test"),
			wantError:   false,
		},
		{
			name:        "data too short - empty",
			data:        []byte{},
			wantCmd:     0,
			wantPayload: nil,
			wantError:   true,
		},
		{
			name:        "data too short - 3 bytes",
			data:        []byte{0x00, 0x00, 0x01},
			wantCmd:     0,
			wantPayload: nil,
			wantError:   true,
		},
		{
			name:        "exactly header size",
			data:        []byte{0x00, 0x00, 0x00, 0x42},
			wantCmd:     0x42,
			wantPayload: []byte{},
			wantError:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			gotCmd, gotPayload, err := Decode(tt.data)

			if (err != nil) != tt.wantError {
				t.Errorf("Decode() error = %v, wantError %v", err, tt.wantError)
				return
			}

			if tt.wantError {
				return
			}

			if gotCmd != tt.wantCmd {
				t.Errorf("Decode() command = %v, want %v", gotCmd, tt.wantCmd)
			}

			if !bytes.Equal(gotPayload, tt.wantPayload) {
				t.Errorf("Decode() payload = %v, want %v", gotPayload, tt.wantPayload)
			}
		})
	}
}

// TestEncodeDecodeRoundTrip verifies that Encode and Decode are perfect inverses
func TestEncodeDecodeRoundTrip(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		commandID uint32
		payload   []byte
	}{
		{"empty payload", 0x01, []byte{}},
		{"nil payload", 0x02, nil},
		{"text payload", 0x03, []byte("Hello, World!")},
		{"binary payload", 0x04, []byte{0x00, 0x01, 0xFF, 0xFE, 0x42}},
		{"max command ID", 0xFFFFFFFF, []byte("test")},
		{"large payload", 0x05, make([]byte, 100*1024)}, // 100KB
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			encoded, err := Encode(tt.commandID, tt.payload)
			if err != nil {
				t.Fatalf("Encode() failed: %v", err)
			}

			decodedCmd, decodedPayload, err := Decode(encoded)
			if err != nil {
				t.Fatalf("Decode() failed: %v", err)
			}

			if decodedCmd != tt.commandID {
				t.Errorf("command ID = %v, want %v", decodedCmd, tt.commandID)
			}

			if !bytes.Equal(decodedPayload, tt.payload) {
				t.Errorf("payload mismatch: got %v, want %v", decodedPayload, tt.payload)
			}
		})
	}
}

// TestDecodePayloadIndependence verifies that the payload returned by Decode is
// an independent copy: mutating it must not affect the original input buffer.
func TestDecodePayloadIndependence(t *testing.T) {
	t.Parallel()

	original := []byte{0x00, 0x00, 0x00, 0x01, 0x41, 0x42, 0x43} // cmd=1, payload="ABC"
	originalCopy := make([]byte, len(original))
	copy(originalCopy, original)

	_, payload, err := Decode(original)
	if err != nil {
		t.Fatalf("Decode() failed: %v", err)
	}

	// Mutate the returned payload.
	if len(payload) > 0 {
		payload[0] = 0xFF
	}

	// The original buffer must remain untouched.
	if !bytes.Equal(original, originalCopy) {
		t.Error("Decode() mutated the original input buffer; it must return an independent copy")
	}
}

// TestEncodePreservesInput tests that Encode doesn't modify the input payload
func TestEncodePreservesInput(t *testing.T) {
	t.Parallel()

	payload := []byte{0x01, 0x02, 0x03, 0x04}
	payloadCopy := make([]byte, len(payload))
	copy(payloadCopy, payload)

	_, err := Encode(0x42, payload)
	if err != nil {
		t.Fatalf("Encode() failed: %v", err)
	}

	if !bytes.Equal(payload, payloadCopy) {
		t.Errorf("Encode() modified input payload: got %v, want %v", payload, payloadCopy)
	}
}

// TestEncodeBigEndian verifies that command IDs are encoded in big-endian format
func TestEncodeBigEndian(t *testing.T) {
	t.Parallel()

	tests := []struct {
		commandID uint32
		expected  []byte // First 4 bytes
	}{
		{0x01020304, []byte{0x01, 0x02, 0x03, 0x04}},
		{0x00000001, []byte{0x00, 0x00, 0x00, 0x01}},
		{0xFF000000, []byte{0xFF, 0x00, 0x00, 0x00}},
		{0xFFFFFFFF, []byte{0xFF, 0xFF, 0xFF, 0xFF}},
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			result, err := Encode(tt.commandID, nil)
			if err != nil {
				t.Fatalf("Encode() failed: %v", err)
			}

			if !bytes.Equal(result[:4], tt.expected) {
				t.Errorf("encoded bytes = %v, want %v", result[:4], tt.expected)
			}
		})
	}
}

// BenchmarkEncode benchmarks the encoding operation
func BenchmarkEncode(b *testing.B) {
	payload := []byte("benchmark test payload with some data")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = Encode(0x42, payload)
	}
}

// BenchmarkDecode benchmarks the decoding operation
func BenchmarkDecode(b *testing.B) {
	data, _ := Encode(0x42, []byte("benchmark test payload with some data"))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _ = Decode(data)
	}
}

// BenchmarkEncodeSmallPayload benchmarks encoding with small payloads
func BenchmarkEncodeSmallPayload(b *testing.B) {
	payload := []byte("hi")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = Encode(0x01, payload)
	}
}

// BenchmarkEncodeLargePayload benchmarks encoding with large payloads
func BenchmarkEncodeLargePayload(b *testing.B) {
	payload := make([]byte, 1024*1024) // 1MB
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = Encode(0x01, payload)
	}
}
