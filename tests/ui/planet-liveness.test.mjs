import assert from "node:assert/strict";
import { test } from "node:test";
import {
  livenessAction,
  STREAM_PROBE_MS,
  STREAM_TIMEOUT_MS,
  WELCOME_GRACE_MS,
} from "../../internal/server/web/features/planet/liveness.js";

test("Planet waits for slow scene initialization instead of closing after 6 seconds", () => {
  assert.equal(livenessAction(6_000, 0, 0, false), "wait");
  assert.equal(livenessAction(20_000, 0, 0, false), "wait");
  assert.equal(livenessAction(WELCOME_GRACE_MS - 1, 0, 0, false), "wait");
  assert.equal(livenessAction(WELCOME_GRACE_MS, 0, 0, false), "timeout");
});

test("Planet probes a silent live stream and only then times out", () => {
  assert.equal(livenessAction(6_000, 0, 0, true), "wait");
  assert.equal(livenessAction(STREAM_PROBE_MS - 1, 0, 0, true), "wait");
  assert.equal(livenessAction(STREAM_PROBE_MS, 0, 0, true), "probe");
  assert.equal(livenessAction(20_000, 0, 0, true, STREAM_PROBE_MS), "wait");
  assert.equal(livenessAction(24_000, 0, 0, true, STREAM_PROBE_MS), "probe");
  assert.equal(livenessAction(STREAM_TIMEOUT_MS - 1, 0, 0, true, 24_000), "wait");
  assert.equal(livenessAction(STREAM_TIMEOUT_MS, 0, 0, true, 24_000), "timeout");
});

test("incoming frames reset the silence clock and prevent false reconnects", () => {
  assert.equal(livenessAction(45_000, 0, 40_000, true, 12_000), "wait");
  assert.equal(livenessAction(56_000, 0, 40_000, true, 12_000), "probe");
  assert.equal(livenessAction(57_000, 0, 56_500, true, 56_000), "wait");
});
