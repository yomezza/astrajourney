import { uuid } from "../../core/uuid.js";
// A single transport per signed-in account. Mutations use WS only; uncertain
// outcomes are looked up by the original UUID and never automatically re-sent.
export class ChatClient {
  constructor(onEvent) {
    this.onEvent = onEvent;
    this.pending = new Map();
    this.closed = false;
    this.attempt = 0;
    this.controller = new AbortController();
  }
  async get(route, params = {}) {
    const r = await fetch(
      "/chat/api/v1/" + route + "?" + new URLSearchParams(params),
      {
        credentials: "same-origin",
        signal: this.controller.signal,
        cache: "no-store",
      },
    );
    const v = await r.json();
    if (!r.ok) throw Error(v.error || "chat_unavailable");
    return v;
  }
  connect() {
    if (
      this.closed ||
      this.socket?.readyState === WebSocket.OPEN ||
      this.socket?.readyState === WebSocket.CONNECTING
    )
      return;
    const url = new URL("/chat/ws/v1", location.href);
    url.protocol = location.protocol === "https:" ? "wss:" : "ws:";
    const socket = new WebSocket(url);
    this.socket = socket;
    let hello = false;
    socket.onmessage = ({ data }) => {
      if (this.closed || this.socket !== socket) return;
      let v;
      try {
        v = JSON.parse(data);
      } catch {
        return;
      }
      if (v.type === "hello") {
        hello = true;
        this.attempt = 0;
      }
      if (v.type === "ack") {
        const p = this.pending.get(v.request_id);
        if (p) {
          clearTimeout(p.timer);
          this.pending.delete(v.request_id);
          v.error ? p.reject(Error(v.error)) : p.resolve(v.result);
        }
      } else this.onEvent(v);
    };
    socket.onclose = () => {
      if (this.closed || this.socket !== socket) return;
      this.onEvent({
        type: "offline",
        error: hello ? "chat_unavailable" : "chat_connection_rejected",
      });
      for (const [id, p] of this.pending) {
        clearTimeout(p.timer);
        this.pending.delete(id);
        this.resolveUnknown(id, p);
      }
      if (hello || this.attempt < 5) {
        this.retry = setTimeout(
          () => this.connect(),
          Math.min(30000, 1000 * 2 ** this.attempt++),
        );
      }
    };
  }
  async resolveUnknown(id, p) {
    try {
      for (let n = 0; n < 12 && !this.closed; n++) {
        const op = await this.get("operation", { id });
        if (op.state === "completed") {
          p.resolve(op.result);
          return;
        }
        // A missing uncommitted row is inconclusive until the server's bounded
        // transaction deadline has elapsed. Never turn an early miss into a resend.
        if (op.state === "not_found" && n >= 10) {
          p.reject(
            Object.assign(Error("operation_not_committed"), { request: id }),
          );
          return;
        }
        await new Promise((resolve) => setTimeout(resolve, 1000));
      }
      p.reject(Object.assign(Error("operation_unknown"), { request: id }));
    } catch {
      p.reject(Object.assign(Error("operation_unknown"), { request: id }));
    }
  }
  command(type, payload, request = uuid()) {
    if (this.closed || this.socket?.readyState !== WebSocket.OPEN)
      return Promise.reject(Error("chat_unavailable"));
    return new Promise((resolve, reject) => {
      const p = { resolve, reject };
      p.timer = setTimeout(() => {
        this.pending.delete(request);
        this.resolveUnknown(request, p);
      }, 10000);
      this.pending.set(request, p);
      this.socket.send(JSON.stringify({ type, request_id: request, payload }));
    });
  }
  close() {
    this.closed = true;
    clearTimeout(this.retry);
    this.controller.abort();
    this.socket?.close(1000, "signout");
    for (const p of this.pending.values()) {
      clearTimeout(p.timer);
      p.reject(Error("auth_required"));
    }
    this.pending.clear();
  }
}
