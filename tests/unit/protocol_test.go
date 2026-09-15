package unit_test

import (
	"bytes"
	"testing"

	"github.com/luciancaetano/knet/internal/protocol"
)

// TestEncodeDecode tests basic encode/decode functionality
func TestEncodeDecode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		commandID uint32
		payload   []byte
	}{
		{
			name:      "simple message",
			commandID: 0x01,
			payload:   []byte("hello world"),
		},
		{
			name:      "empty payload",
			commandID: 0x100,
			payload:   []byte{},
		},
		{
			name:      "binary payload",
			commandID: 0xFFFF,
			payload:   []byte{0x00, 0x01, 0x02, 0xFF, 0xFE},
		},
		{
			name:      "max command id",
			commandID: 0xFFFFFFFF,
			payload:   []byte("test"),
		},
		{
			name:      "zero command id",
			commandID: 0x00,
			payload:   []byte("zero"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Encode
			encoded, err := protocol.Encode(tt.commandID, tt.payload)
			if err != nil {
				t.Fatalf("Encode() error = %v", err)
			}

			// Decode
			_, gotCmd, gotPayload, err := protocol.Decode(encoded)
			if err != nil {
				t.Fatalf("Decode() error = %v", err)
			}

			// Verify command ID
			if gotCmd != tt.commandID {
				t.Errorf("commandID = %v, want %v", gotCmd, tt.commandID)
			}

			// Verify payload
			if !bytes.Equal(gotPayload, tt.payload) {
				t.Errorf("payload = %v, want %v", gotPayload, tt.payload)
			}
		})
	}
}

// TestEncodeErrors tests error conditions during encoding
func TestEncodeErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		commandID uint32
		payload   []byte
		wantError bool
	}{
		{
			name:      "payload too large",
			commandID: 0x01,
			payload:   make([]byte, 11*1024*1024), // 11MB > 10MB limit
			wantError: true,
		},
		{
			name:      "maximum allowed payload",
			commandID: 0x01,
			payload:   make([]byte, 10*1024*1024), // exactly 10MB
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := protocol.Encode(tt.commandID, tt.payload)
			if (err != nil) != tt.wantError {
				t.Errorf("Encode() error = %v, wantError %v", err, tt.wantError)
			}
		})
	}
}

// TestDecodeErrors tests error conditions during decoding
func TestDecodeErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		data      []byte
		wantError bool
	}{
		{
			name:      "data too short - empty",
			data:      []byte{},
			wantError: true,
		},
		{
			name:      "data too short - 4 bytes",
			data:      []byte{0x01, 0x00, 0x01, 0x02},
			wantError: true,
		},
		{
			name:      "minimum valid data - header only",
			data:      []byte{0x01, 0x00, 0x00, 0x00, 0x01},
			wantError: false,
		},
		{
			name:      "valid data with payload",
			data:      []byte{0x01, 0x00, 0x00, 0x00, 0x01, 0xFF, 0xFE},
			wantError: false,
		},
		{
			name:      "unsupported version",
			data:      []byte{0x02, 0x00, 0x00, 0x00, 0x01, 0xFF, 0xFE},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, _, _, err := protocol.Decode(tt.data)
			if (err != nil) != tt.wantError {
				t.Errorf("Decode() error = %v, wantError %v", err, tt.wantError)
			}
		})
	}
}

// TestEncodeDecodeSymmetry ensures encode/decode are perfect inverses
func TestEncodeDecodeSymmetry(t *testing.T) {
	t.Parallel()

	// Test with random-ish data
	commandIDs := []uint32{0, 1, 0xFF, 0xFFFF, 0xFFFFFFFF}
	payloads := [][]byte{
		{},
		{0x00},
		{0xFF},
		[]byte("Hello, World!"),
		[]byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05},
		make([]byte, 1024), // 1KB of zeros
	}

	for _, cmdID := range commandIDs {
		for i, payload := range payloads {
			t.Run(t.Name(), func(t *testing.T) {
				encoded, err := protocol.Encode(cmdID, payload)
				if err != nil {
					t.Fatalf("Encode failed: %v", err)
				}

				_, decodedCmd, decodedPayload, err := protocol.Decode(encoded)
				if err != nil {
					t.Fatalf("Decode failed: %v", err)
				}

				if decodedCmd != cmdID {
					t.Errorf("test %d: commandID mismatch: got %v, want %v", i, decodedCmd, cmdID)
				}

				if !bytes.Equal(decodedPayload, payload) {
					t.Errorf("test %d: payload mismatch: got %v, want %v", i, decodedPayload, payload)
				}
			})
		}
	}
}

// BenchmarkEncode benchmarks the encoding process
func BenchmarkEncode(b *testing.B) {
	payload := []byte("benchmark payload data")
	commandID := uint32(0x01)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = protocol.Encode(commandID, payload)
	}
}

// BenchmarkDecode benchmarks the decoding process
func BenchmarkDecode(b *testing.B) {
	payload := []byte("benchmark payload data")
	commandID := uint32(0x01)
	encoded, _ := protocol.Encode(commandID, payload)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _, _ = protocol.Decode(encoded)
	}
}

// BenchmarkEncodeDecodeRoundtrip benchmarks full encode/decode cycle
func BenchmarkEncodeDecodeRoundtrip(b *testing.B) {
	payload := []byte("benchmark payload data")
	commandID := uint32(0x01)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		encoded, _ := protocol.Encode(commandID, payload)
		_, _, _, _ = protocol.Decode(encoded)
	}
}
