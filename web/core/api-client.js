import { requestID, trace } from "./diagnostics.js";
export class ApiError extends Error {
  constructor(message, { status = 0, kind = "http", code = "", cause } = {}) {
    super(message, { cause });
    this.name = "ApiError";
    this.status = status;
    this.kind = kind;
    this.code = code;
  }
}

export async function request(path, body, signal) {
  let response;
 const requestId = requestID(), began = performance.now();
  try {
    response = await fetch("/api/" + path, {
      signal,
      method: body === undefined ? "GET" : "POST",
      headers: body === undefined ? {} : { "Content-Type": "application/json" },
      body: body === undefined ? undefined : JSON.stringify(body),
    });
  } catch (error) {
    trace("http_failed", { requestId, phase: "fetch", code: error.name, durationMs: Math.round(performance.now()-began) });
    if (error.name === "AbortError") throw error;
    throw new ApiError("Не удалось подключиться", {
      kind: "network",
      cause: error,
    });
  }
  trace("http_response", { requestId, phase: path.split("?")[0], code: response.status, durationMs: Math.round(performance.now()-began) });
  let data;
  try {
    data = JSON.parse(await response.text());
  } catch (error) {
    throw new ApiError("Ошибка сервера", {
      status: response.status,
      kind: response.ok ? "protocol" : "http",
      cause: error,
    });
  }
  if (!response.ok)
    throw new ApiError(data?.error || "Ошибка сервера", {
      status: response.status,
      code: typeof data?.code === "string" ? data.code : "",
    });
  return data;
}
