// src/protocol.ts
var CURRENT_VERSION = 1;
var HEADER_SIZE = 5;
var MAX_PAYLOAD_SIZE = 10 * 1024 * 1024;
var ReservedCommands = {
  JsonRpc: 4294967295,
  JsonRpcError: 4294967294,
  Ping: 4294967293
};
var UnsupportedVersionError = class extends Error {
  constructor(version) {
    super(`unsupported protocol version: ${version}`);
    this.version = version;
  }
};
function encodeFrame(commandId, payload) {
  if (payload.length > MAX_PAYLOAD_SIZE) {
    throw new Error(`payload size ${payload.length} exceeds maximum ${MAX_PAYLOAD_SIZE} bytes`);
  }
  const out = new Uint8Array(HEADER_SIZE + payload.length);
  const view = new DataView(out.buffer);
  view.setUint8(0, CURRENT_VERSION);
  view.setUint32(1, commandId, false);
  out.set(payload, HEADER_SIZE);
  return out;
}
function decodeFrame(data) {
  if (data.byteLength < HEADER_SIZE) {
    throw new Error("data too short");
  }
  const view = new DataView(data);
  const version = view.getUint8(0);
  if (version !== CURRENT_VERSION) {
    throw new UnsupportedVersionError(version);
  }
  const commandId = view.getUint32(1, false);
  const payload = new Uint8Array(data.slice(HEADER_SIZE));
  return { version, commandId, payload };
}

// src/jsonrpc.ts
var RPC_TIMEOUT_MS = 3e4;
var JsonRpcDispatcher = class {
  constructor() {
    this.nextId = 1;
    this.pending = /* @__PURE__ */ new Map();
  }
  buildRequest(method, params) {
    const id = this.nextId++;
    const body = JSON.stringify({ jsonrpc: "2.0", id, method, params });
    return { id, body };
  }
  wait(id) {
    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => {
        this.pending.delete(id);
        reject(new Error(`JSON-RPC call ${id} timed out after ${RPC_TIMEOUT_MS}ms`));
      }, RPC_TIMEOUT_MS);
      this.pending.set(id, { resolve, reject, timer });
    });
  }
  /** Feed a raw JSON-RPC response (success or error envelope) received from the server. */
  handleResponse(raw) {
    let msg;
    try {
      msg = JSON.parse(raw);
    } catch {
      return;
    }
    const call = this.pending.get(msg.id);
    if (!call) return;
    this.pending.delete(msg.id);
    clearTimeout(call.timer);
    if (msg.error) {
      call.reject(new Error(`[${msg.error.code}] ${msg.error.message}`));
    } else {
      call.resolve(msg.result);
    }
  }
};

// src/client.ts
var textEncoder = new TextEncoder();
var textDecoder = new TextDecoder();
var KNetClient = class {
  constructor(config) {
    this.ws = null;
    this.state = "disconnected";
    this.reconnectAttempt = 0;
    this.reconnectTimer = null;
    this.manuallyDisconnected = false;
    this.commandHandlers = /* @__PURE__ */ new Map();
    this.listeners = {
      connected: /* @__PURE__ */ new Set(),
      disconnected: /* @__PURE__ */ new Set(),
      error: /* @__PURE__ */ new Set()
    };
    this.rpc = new JsonRpcDispatcher();
    this.config = {
      autoReconnect: true,
      reconnectDelayMs: 3e3,
      maxReconnectAttempts: 0,
      connectionTimeoutMs: 1e4,
      ...config
    };
    this.onCommand(ReservedCommands.Ping, (payload) => {
      void this.send(ReservedCommands.Ping, payload);
    });
  }
  getState() {
    return this.state;
  }
  on(event, handler) {
    this.listeners[event].add(handler);
  }
  off(event, handler) {
    this.listeners[event].delete(handler);
  }
  emit(event, ...args) {
    for (const handler of this.listeners[event]) {
      handler(...args);
    }
  }
  onCommand(commandId, handler) {
    let set = this.commandHandlers.get(commandId);
    if (!set) {
      set = /* @__PURE__ */ new Set();
      this.commandHandlers.set(commandId, set);
    }
    set.add(handler);
  }
  offCommand(commandId, handler) {
    this.commandHandlers.get(commandId)?.delete(handler);
  }
  connect() {
    this.manuallyDisconnected = false;
    return this.openSocket();
  }
  openSocket() {
    this.state = "connecting";
    return new Promise((resolve, reject) => {
      const ws = new WebSocket(this.config.url);
      ws.binaryType = "arraybuffer";
      this.ws = ws;
      const timeout = setTimeout(() => {
        ws.close();
        reject(new Error(`connection to ${this.config.url} timed out`));
      }, this.config.connectionTimeoutMs);
      ws.addEventListener("open", () => {
        clearTimeout(timeout);
        this.state = "connected";
        this.reconnectAttempt = 0;
        this.emit("connected");
        resolve();
      });
      ws.addEventListener("message", (event) => {
        if (!(event.data instanceof ArrayBuffer)) return;
        this.handleFrame(event.data);
      });
      ws.addEventListener("error", () => {
        this.emit("error", `WebSocket error on ${this.config.url}`);
      });
      ws.addEventListener("close", (event) => {
        clearTimeout(timeout);
        const wasConnecting = this.state === "connecting";
        this.state = "disconnected";
        this.emit("disconnected", event.code, event.reason);
        if (wasConnecting) reject(new Error(`connection to ${this.config.url} closed before opening`));
        this.scheduleReconnect();
      });
    });
  }
  scheduleReconnect() {
    if (this.manuallyDisconnected || !this.config.autoReconnect) return;
    if (this.config.maxReconnectAttempts > 0 && this.reconnectAttempt >= this.config.maxReconnectAttempts) {
      return;
    }
    this.reconnectAttempt++;
    const delay = this.config.reconnectDelayMs * Math.min(this.reconnectAttempt, 5);
    this.reconnectTimer = setTimeout(() => {
      this.openSocket().catch(() => {
      });
    }, delay);
  }
  handleFrame(data) {
    let frame;
    try {
      frame = decodeFrame(data);
    } catch (err) {
      if (err instanceof UnsupportedVersionError) {
        this.emit("error", err.message);
      }
      return;
    }
    if (frame.commandId === ReservedCommands.JsonRpc || frame.commandId === ReservedCommands.JsonRpcError) {
      this.rpc.handleResponse(textDecoder.decode(frame.payload));
      return;
    }
    for (const handler of this.commandHandlers.get(frame.commandId) ?? []) {
      handler(frame.payload);
    }
  }
  async send(commandId, payload) {
    if (!this.ws || this.state !== "connected") {
      throw new Error("client is not connected");
    }
    this.ws.send(encodeFrame(commandId, payload));
  }
  sendString(commandId, text) {
    return this.send(commandId, textEncoder.encode(text));
  }
  sendJson(commandId, value) {
    return this.sendString(commandId, JSON.stringify(value));
  }
  async callRpc(method, params) {
    const { id, body } = this.rpc.buildRequest(method, params);
    const result = this.rpc.wait(id);
    await this.sendString(ReservedCommands.JsonRpc, body);
    return result;
  }
  disconnect() {
    this.manuallyDisconnected = true;
    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer);
      this.reconnectTimer = null;
    }
    this.ws?.close();
  }
};
export {
  HEADER_SIZE,
  KNetClient,
  MAX_PAYLOAD_SIZE,
  ReservedCommands,
  decodeFrame,
  encodeFrame
};
//# sourceMappingURL=index.js.map