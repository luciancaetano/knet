package protocol

import (
	"encoding/binary"
	"errors"
	"fmt"
)

const (
	// HeaderSize is the number of bytes used for the command-ID header.
	// Exported so transport layers can compute connection-level read limits
	// without duplicating the value.
	HeaderSize = 4

	// MaxPayloadSize is the maximum allowed payload size in bytes (10 MB).
	// Exported so transport layers can enforce this limit at the wire level
	// before any protocol buffer is allocated.
	MaxPayloadSize = 10 * 1024 * 1024
)

// Encode encodes the commandID as the first 4 bytes (big-endian) followed by
// the payload.
func Encode(commandID uint32, payload []byte) ([]byte, error) {
	if len(payload) > MaxPayloadSize {
		return nil, fmt.Errorf("payload size %d exceeds maximum %d bytes", len(payload), MaxPayloadSize)
	}

	out := make([]byte, HeaderSize+len(payload))
	binary.BigEndian.PutUint32(out[:HeaderSize], commandID)
	copy(out[HeaderSize:], payload)
	return out, nil
}

// Decode decodes the first 4 bytes as commandID (big-endian) and returns the
// payload as an independent copy of the input slice.
//
// Returning a copy guarantees that:
//   - The caller may safely retain or modify the payload without affecting the
//     original network buffer.
//   - Concurrent handler goroutines receive distinct memory and cannot
//     accidentally corrupt each other's data.
func Decode(data []byte) (uint32, []byte, error) {
	if len(data) < HeaderSize {
		return 0, nil, errors.New("data too short")
	}

	payloadSize := len(data) - HeaderSize
	if payloadSize > MaxPayloadSize {
		return 0, nil, fmt.Errorf("payload size %d exceeds maximum %d bytes", payloadSize, MaxPayloadSize)
	}

	cmd := binary.BigEndian.Uint32(data[:HeaderSize])
	payload := make([]byte, payloadSize)
	copy(payload, data[HeaderSize:])
	return cmd, payload, nil
}
