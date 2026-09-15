package bench

import "encoding/binary"

// Wire format mirrors internal/protocol (unexported to the root module, so
// benchmark re-implements the 5-byte header here): 1B version + 4B BE
// commandID + payload.
const protocolVersion byte = 1

func encodeFrame(commandID uint32, payload []byte) []byte {
	out := make([]byte, 5+len(payload))
	out[0] = protocolVersion
	binary.BigEndian.PutUint32(out[1:5], commandID)
	copy(out[5:], payload)
	return out
}

func decodeFrame(data []byte) (commandID uint32, payload []byte, ok bool) {
	if len(data) < 5 {
		return 0, nil, false
	}
	return binary.BigEndian.Uint32(data[1:5]), data[5:], true
}
