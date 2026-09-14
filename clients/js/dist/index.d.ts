type ConnectionState = "disconnected" | "connecting" | "connected";
interface KNetClientConfig {
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
/** Browser WebSocket client for the knet wire protocol. Mirrors clients/unity/KNetClient.cs. */
declare class KNetClient {
    private readonly config;
    private ws;
    private state;
    private reconnectAttempt;
    private reconnectTimer;
    private manuallyDisconnected;
    private readonly commandHandlers;
    private readonly listeners;
    private readonly rpc;
    constructor(config: KNetClientConfig);
    getState(): ConnectionState;
    on<K extends keyof EventMap>(event: K, handler: EventMap[K]): void;
    off<K extends keyof EventMap>(event: K, handler: EventMap[K]): void;
    private emit;
    onCommand(commandId: number, handler: CommandHandler): void;
    offCommand(commandId: number, handler: CommandHandler): void;
    connect(): Promise<void>;
    private openSocket;
    private scheduleReconnect;
    private handleFrame;
    send(commandId: number, payload: Uint8Array): Promise<void>;
    sendString(commandId: number, text: string): Promise<void>;
    sendJson(commandId: number, value: unknown): Promise<void>;
    callRpc<T = unknown>(method: string, params?: unknown): Promise<T>;
    disconnect(): void;
}

declare const HEADER_SIZE = 5;
declare const MAX_PAYLOAD_SIZE: number;
declare const ReservedCommands: {
    readonly JsonRpc: 4294967295;
    readonly JsonRpcError: 4294967294;
    readonly Ping: 4294967293;
};
declare function encodeFrame(commandId: number, payload: Uint8Array): Uint8Array;
interface DecodedFrame {
    version: number;
    commandId: number;
    payload: Uint8Array;
}
declare function decodeFrame(data: ArrayBufferLike): DecodedFrame;

export { type ConnectionState, type DecodedFrame, HEADER_SIZE, KNetClient, type KNetClientConfig, MAX_PAYLOAD_SIZE, ReservedCommands, decodeFrame, encodeFrame };
