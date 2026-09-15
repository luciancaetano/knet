package protocol

import (
	"encoding/binary"
	"errors"
	"fmt"
)

const (
	// Version1 is the initial wire-format version.
	Version1 byte = 1

	// CurrentVersion is the protocol version written by Encode.
	CurrentVersion = Version1

	// HeaderSize is the number of bytes used for the version+command-ID
	// header (1B version + 4B big-endian commandID).
	// Exported so transport layers can compute connection-level read limits
	// without duplicating the value.
	HeaderSize = 5

	// MaxPayloadSize is the maximum allowed payload size in bytes (10 MB).
	// Exported so transport layers can enforce this limit at the wire level
	// before any protocol buffer is allocated.
	MaxPayloadSize = 10 * 1024 * 1024
)

// ErrUnsupportedVersion is returned by Decode when the frame's version byte
// does not match a version this build understands.
var ErrUnsupportedVersion = errors.New("unsupported protocol version")

// Encode encodes a frame as: [1B version][4B BE commandID][payload].
// The version is always CurrentVersion.
func Encode(commandID uint32, payload []byte) ([]byte, error) {
	if len(payload) > MaxPayloadSize {
		return nil, fmt.Errorf("payload size %d exceeds maximum %d bytes", len(payload), MaxPayloadSize)
	}

	out := make([]byte, HeaderSize+len(payload))
	out[0] = CurrentVersion
	binary.BigEndian.PutUint32(out[1:HeaderSize], commandID)
	copy(out[HeaderSize:], payload)
	return out, nil
}

// Decode decodes a frame's version byte, commandID (big-endian) and payload
// (returned as an independent copy of the input slice).
//
// Returns ErrUnsupportedVersion if the version byte is not CurrentVersion, so
// callers can distinguish a version mismatch from a malformed frame.
//
// Returning a payload copy guarantees that:
//   - The caller may safely retain or modify the payload without affecting the
//     original network buffer.
//   - Concurrent handler goroutines receive distinct memory and cannot
//     accidentally corrupt each other's data.
func Decode(data []byte) (version byte, commandID uint32, payload []byte, err error) {
	if len(data) < HeaderSize {
		return 0, 0, nil, errors.New("data too short")
	}

	version = data[0]
	if version != CurrentVersion {
		return version, 0, nil, ErrUnsupportedVersion
	}

	payloadSize := len(data) - HeaderSize
	if payloadSize > MaxPayloadSize {
		return version, 0, nil, fmt.Errorf("payload size %d exceeds maximum %d bytes", payloadSize, MaxPayloadSize)
	}

	commandID = binary.BigEndian.Uint32(data[1:HeaderSize])
	payload = make([]byte, payloadSize)
	copy(payload, data[HeaderSize:])
	return version, commandID, payload, nil
}
