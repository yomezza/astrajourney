// The Planet server can take up to scene_load_timeout (20s by default)
// before sending its first welcome. A 6s client-side watchdog incorrectly
// tore down valid connections and reopened the startup overlay repeatedly.
export const WELCOME_GRACE_MS = 35_000;
export const STREAM_PROBE_MS = 12_000;
export const STREAM_TIMEOUT_MS = 30_000;

// Pure, clock-injected decision function for the browser's connection monitor.
// An idle-but-open socket is probed before it is discarded; client-side
// stalls and long scene initialization should not force a reconnect loop.
export function livenessAction(now, openedAt, lastFrame, welcomed, lastProbeAt = 0) {
  if (!welcomed) {
    return now - openedAt >= WELCOME_GRACE_MS ? "timeout" : "wait";
  }
  const silence = now - lastFrame;
  if (silence >= STREAM_TIMEOUT_MS) return "timeout";
  if (
    silence >= STREAM_PROBE_MS &&
    (!lastProbeAt || now - lastProbeAt >= STREAM_PROBE_MS)
  ) return "probe";
  return "wait";
}
