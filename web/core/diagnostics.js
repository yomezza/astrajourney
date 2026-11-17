import { uuid } from "./uuid.js";

const records = [];
// Deliberate allowlist: no arbitrary error messages, bodies, headers, URLs,
// session cookies, handoff tickets or Chat text enter this buffer.
const fields = new Set([
  "requestId", "visit", "connectionEpoch", "sessionEpoch", "phase", "code",
  "durationMs", "delayMs", "attempt", "current", "renderer", "socket",
  "monitor", "polling", "retry", "inertElements", "wasClean", "initiator",
]);
export const requestID = () => uuid();
export function trace(event, values = {}) {
  const record = { time: new Date().toISOString(), event };
  for (const [key, value] of Object.entries(values))
    if (fields.has(key) && ["string", "number", "boolean"].includes(typeof value))
      record[key] = typeof value === "string" ? value.slice(0, 80) : value;
  records.push(record);
  if (records.length > 400) records.shift();
  console.debug("Astra diagnostic", record);
}
// Export through dynamic import in DevTools; the buffer is only in page memory.
export function events() { return records.map((record) => ({ ...record })); }
export function clear() { records.length = 0; }
