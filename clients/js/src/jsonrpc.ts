const RPC_TIMEOUT_MS = 30_000;

interface PendingCall {
  resolve: (result: unknown) => void;
  reject: (err: Error) => void;
  timer: ReturnType<typeof setTimeout>;
}

interface JsonRpcResponse {
  jsonrpc: "2.0";
  id: number;
  result?: unknown;
  error?: { code: number; message: string };
}

/** Tracks in-flight JSON-RPC 2.0 calls and matches responses by id. */
export class JsonRpcDispatcher {
  private nextId = 1;
  private pending = new Map<number, PendingCall>();

  buildRequest(method: string, params: unknown): { id: number; body: string } {
    const id = this.nextId++;
    const body = JSON.stringify({ jsonrpc: "2.0", id, method, params });
    return { id, body };
  }

  wait<T>(id: number): Promise<T> {
    return new Promise<T>((resolve, reject) => {
      const timer = setTimeout(() => {
        this.pending.delete(id);
        reject(new Error(`JSON-RPC call ${id} timed out after ${RPC_TIMEOUT_MS}ms`));
      }, RPC_TIMEOUT_MS);
      this.pending.set(id, { resolve: resolve as (r: unknown) => void, reject, timer });
    });
  }

  /** Feed a raw JSON-RPC response (success or error envelope) received from the server. */
  handleResponse(raw: string): void {
    let msg: JsonRpcResponse;
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
}
