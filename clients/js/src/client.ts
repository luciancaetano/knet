import { decodeFrame, encodeFrame, ReservedCommands, UnsupportedVersionError } from "./protocol.js";
import { JsonRpcDispatcher } from "./jsonrpc.js";

export type ConnectionState = "disconnected" | "connecting" | "connected";

export interface KNetClientConfig {
  url: string;
  autoReconnect?: boolean;
  reconnectDelayMs?: number;
  maxReconnectAttempts?: number;
  connectionTimeoutMs?: number;
}

type CommandHandler = (payload: Uint8Array) => void;

interface EventMap {
  connected: () => void;
  disconnected: (code: number, reason: string) => void;
  error: (message: string) => void;
}

const textEncoder = new TextEncoder();
const textDecoder = new TextDecoder();

/** Browser WebSocket client for the knet wire protocol. Mirrors clients/unity/KNetClient.cs. */
export class KNetClient {
  private readonly config: Required<KNetClientConfig>;
  private ws: WebSocket | null = null;
  private state: ConnectionState = "disconnected";
  private reconnectAttempt = 0;
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null;
  private manuallyDisconnected = false;

  private readonly commandHandlers = new Map<number, Set<CommandHandler>>();
  private readonly listeners: { [K in keyof EventMap]: Set<EventMap[K]> } = {
    connected: new Set(),
    disconnected: new Set(),
    error: new Set(),
  };
  private readonly rpc = new JsonRpcDispatcher();

  constructor(config: KNetClientConfig) {
    this.config = {
      autoReconnect: true,
      reconnectDelayMs: 3000,
      maxReconnectAttempts: 0,
      connectionTimeoutMs: 10_000,
      ...config,
    };
  }

  getState(): ConnectionState {
    return this.state;
  }

  on<K extends keyof EventMap>(event: K, handler: EventMap[K]): void {
    this.listeners[event].add(handler);
  }

  off<K extends keyof EventMap>(event: K, handler: EventMap[K]): void {
    this.listeners[event].delete(handler);
  }

  private emit<K extends keyof EventMap>(event: K, ...args: Parameters<EventMap[K]>): void {
    for (const handler of this.listeners[event]) {
      (handler as (...a: Parameters<EventMap[K]>) => void)(...args);
    }
  }

  onCommand(commandId: number, handler: CommandHandler): void {
    let set = this.commandHandlers.get(commandId);
    if (!set) {
      set = new Set();
      this.commandHandlers.set(commandId, set);
    }
    set.add(handler);
  }

  offCommand(commandId: number, handler: CommandHandler): void {
    this.commandHandlers.get(commandId)?.delete(handler);
  }

  connect(): Promise<void> {
    this.manuallyDisconnected = false;
    return this.openSocket();
  }

  private openSocket(): Promise<void> {
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

      ws.addEventListener("message", (event: MessageEvent) => {
        if (!(event.data instanceof ArrayBuffer)) return;
        this.handleFrame(event.data);
      });

      ws.addEventListener("error", () => {
        this.emit("error", `WebSocket error on ${this.config.url}`);
      });

      ws.addEventListener("close", (event: CloseEvent) => {
        clearTimeout(timeout);
        const wasConnecting = this.state === "connecting";
        this.state = "disconnected";
        this.emit("disconnected", event.code, event.reason);
        if (wasConnecting) reject(new Error(`connection to ${this.config.url} closed before opening`));
        this.scheduleReconnect();
      });
    });
  }

  private scheduleReconnect(): void {
    if (this.manuallyDisconnected || !this.config.autoReconnect) return;
    if (
      this.config.maxReconnectAttempts > 0 &&
      this.reconnectAttempt >= this.config.maxReconnectAttempts
    ) {
      return;
    }
    this.reconnectAttempt++;
    const delay = this.config.reconnectDelayMs * Math.min(this.reconnectAttempt, 5);
    this.reconnectTimer = setTimeout(() => {
      this.openSocket().catch(() => {
        /* scheduleReconnect already re-armed via the close handler */
      });
    }, delay);
  }

  private handleFrame(data: ArrayBuffer): void {
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

  async send(commandId: number, payload: Uint8Array): Promise<void> {
    if (!this.ws || this.state !== "connected") {
      throw new Error("client is not connected");
    }
    this.ws.send(encodeFrame(commandId, payload));
  }

  sendString(commandId: number, text: string): Promise<void> {
    return this.send(commandId, textEncoder.encode(text));
  }

  sendJson(commandId: number, value: unknown): Promise<void> {
    return this.sendString(commandId, JSON.stringify(value));
  }

  async callRpc<T = unknown>(method: string, params?: unknown): Promise<T> {
    const { id, body } = this.rpc.buildRequest(method, params);
    const result = this.rpc.wait<T>(id);
    await this.sendString(ReservedCommands.JsonRpc, body);
    return result;
  }

  disconnect(): void {
    this.manuallyDisconnected = true;
    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer);
      this.reconnectTimer = null;
    }
    this.ws?.close();
  }
}
