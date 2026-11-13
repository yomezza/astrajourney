import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { chromium } from "playwright";
const base = new URL(process.env.TERMINAL_TEST_URL),
  input = JSON.parse(readFileSync(0, "utf8"));
assert.ok(["127.0.0.1", "localhost"].includes(base.hostname));
assert.equal(
  (await fetch(base)).headers.get("x-cosmos-test-run"),
  process.env.TERMINAL_TEST_NONCE,
);
const browser = await chromium.launch({
  executablePath: process.env.CHROMIUM_EXECUTABLE_PATH,
  args: process.env.CHROMIUM_NO_SANDBOX === "1" ? ["--no-sandbox"] : [],
});
try {
  const context = await browser.newContext({
    viewport: { width: 1440, height: 1000 },
  });
  await context.addCookies([
    { name: "session", value: input.cookie, url: base.href },
  ]);
  const page = await context.newPage();
  page.setDefaultTimeout(15000);
  let connections = 0;
  const failures = [],
    errors = [];
  page.on("pageerror", (e) => errors.push(e.message));
  page.on("websocket", (ws) => {
    if (!ws.url().includes("/planet/ws/")) return;
    connections++;
    ws.on("framereceived", (event) => {
      const p = JSON.parse(String(event.payload));
      if (
        p.type === "error" &&
        ["save_failed", "runtime_fence_lost", "session_replaced"].includes(
          p.payload.code,
        )
      )
        failures.push(p.payload);
    });
  });
  await page.goto(base.href);
  await page.locator("#startup-screen").waitFor({ state: "hidden" });
  await page.evaluate(async (site) => {
    const { openVisit } = await import("/features/planet/visit.js");
    await openVisit(site);
  }, input.site);
  const canvas = page.locator('#planet-host canvas[data-ready="true"]');
  await canvas.waitFor({ state: "visible" });
  const start = await canvas.evaluate((c) => ({
    x: Number(c.dataset.xq),
    y: Number(c.dataset.yq),
  }));
  const epoch = await canvas.getAttribute("data-connection-epoch");
  let traveled = 0,
    previous = start;
  for (let step = 0; step < 70; step++) {
    assert.deepEqual(
      failures,
      [],
      "server failed while saving a walking character",
    );
    assert.equal(connections, 1, "unexpected reconnect while walking");
    const state = await canvas.evaluate((c) => ({
      x: Number(c.dataset.xq),
      y: Number(c.dataset.yq),
      zoom: Number(c.dataset.zoom),
    }));
    traveled += Math.hypot(state.x - previous.x, state.y - previous.y);
    previous = state;
    // Oscillate along the same verified starter corridor, using real canvas clicks.
    const target = { x: start.x + (step % 2 ? 0 : 2 * 1024), y: start.y },
      box = await canvas.boundingBox();
    await canvas.click({
      position: {
        x: box.width / 2 + ((target.x - state.x) / 1024) * state.zoom,
        y: box.height / 2 + ((target.y - state.y) / 1024) * state.zoom,
      },
    });
    await page.waitForTimeout(1000);
  }
  assert.ok(
    traveled > 30 * 1024,
    "character stopped moving after the first save",
  );
  assert.equal(await canvas.getAttribute("data-connection-epoch"), epoch);
  assert.deepEqual(errors, []);
  console.log(
    "PASS: 70s real browser walking across PostgreSQL checkpoints, one injected slow save, no reconnects, continued server movement",
  );
  await context.close();
} finally {
  await browser.close();
}
