package knet

// Reserved command IDs for internal use.
const (
	// CmdJSONRPC is reserved for JSON-RPC 2.0 messages
	CmdJSONRPC uint32 = 0xFFFFFFFF

	// CmdJSONRPCError is reserved for JSON-RPC 2.0 error responses
	CmdJSONRPCError uint32 = 0xFFFFFFFE

	// CmdInvalidCommand is reserved for server→client "unrecognised command ID" payloads.
	// The current design silently drops unknown client commands instead of replying
	// with this ID, so it is reserved for symmetry with the client Docs/protocol and
	// may be emitted in future.
	CmdInvalidCommand uint32 = 0xFFFFFFFD

	// CmdError is reserved for server→client "command processing error" payloads.
	// Reserved for parity with the client protocol definitions even though the
	// server does not currently emit it.
	CmdError uint32 = 0xFFFFFFFC
)

// Standard error messages
const (
	// Protocol errors
	ErrInvalidMessageFormat = "Invalid message format"
	ErrUnknownCommand       = "unknown command"
	ErrParseError           = "Parse error"
	ErrInvalidRequest       = "Invalid Request"
	ErrMethodNotFound       = "Method not found"
	ErrInternalError        = "Internal error"

	// Connection errors
	ErrClientNotFound       = "client not found"
	ErrConnectionClosed     = "client connection is closed"
	ErrContextCancelled     = "client context cancelled"
	ErrFailedToEncode       = "failed to encode message"
	ErrServerAlreadyRunning = "server already running"
)

// JSON-RPC error codes (following JSON-RPC 2.0 specification)
const (
	JSONRPCParseError     = -32700
	JSONRPCInvalidRequest = -32600
	JSONRPCMethodNotFound = -32601
	JSONRPCInvalidParams  = -32602
	JSONRPCInternalError  = -32603
)

// JSON-RPC version
const (
	JSONRPCVersion = "2.0"
)
