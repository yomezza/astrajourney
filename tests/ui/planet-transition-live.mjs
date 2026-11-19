// Runs only against a nonce-marked server and disposable DB owned by the Go test.
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { chromium } from "playwright";
const base = new URL(process.env.TERMINAL_TEST_URL);
assert.ok(["localhost", "127.0.0.1"].includes(base.hostname));
assert.ok(process.env.TERMINAL_TEST_NONCE?.startsWith("terminal-test-"));
assert.equal(
  (await fetch(base)).headers.get("x-cosmos-test-run"),
  process.env.TERMINAL_TEST_NONCE,
);
const input = JSON.parse(readFileSync(0, "utf8"));
const browser = await chromium.launch({
  ...(process.env.CHROMIUM_EXECUTABLE_PATH
    ? { executablePath: process.env.CHROMIUM_EXECUTABLE_PATH }
    : {}),
  ...(process.env.CHROMIUM_NO_SANDBOX === "1"
    ? { args: ["--no-sandbox"] }
    : {}),
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
  const errors = [];
  page.on("pageerror", (e) => errors.push(e.message));
  await page.goto(base.href);
  // The fixture already owns a live visit: Planet Mode restores it automatically.
  const root = page.locator("#planet-host"),
    canvas = root.locator("canvas");
  await root.getByText("Соседняя локация готова", { exact: true }).waitFor();
  assert.equal(await canvas.getAttribute("data-sector"), "1");
  await root.getByRole("button", { name: "Остаться", exact: true }).click();
  await root.locator(".visit-transition").waitFor({ state: "hidden" });
  assert.equal(await canvas.getAttribute("data-sector"), "1");
  // Re-select the actual exit, using Shift to bypass overlapping scenery.
  const exit = await page.evaluate(async () => {
    const { api } = await import(new URL("./core/api.js", document.querySelector('script[type="module"][src$="bootstrap.js"]').src));
    const visit = await api("planet/visit");
    const epoch = document.querySelector(".visit-canvas").dataset.connectionEpoch;
    const response = await fetch(`/planet/api/v1/scene?visit=${visit.visit}&epoch=${epoch}`);
    if (!response.ok) throw Error(`Scene request failed: ${response.status}`);
    return (await response.json()).exits.find(exit => exit.neighbor === 2);
  });
  assert.ok(exit, "sector 2 exit");
  const position = await canvas.evaluate((element, exit) => {
    const { width, height } = element.getBoundingClientRect();
    const { xq, yq, zoom } = element.dataset;
    return {
      x: width / 2 + ((exit.x_q - Number(xq)) / 1024) * Number(zoom),
      y: height / 2 + ((exit.y_q - Number(yq)) / 1024) * Number(zoom),
    };
  }, exit);
  await canvas.click({ position, modifiers: ["Shift"] });
  await root.getByText("Соседняя локация готова", { exact: true }).waitFor();
  await root
    .getByRole("button", { name: "Перейти в соседний сектор", exact: true })
    .click();
  await page.waitForFunction(() => {
    const c = document.querySelector(".visit-canvas");
    return c?.dataset.ready === "true" && c.dataset.sector === "2";
  });
  if (process.env.PLANET_TEST_SCREENSHOT)
    await root.screenshot({ path: process.env.PLANET_TEST_SCREENSHOT });
  await page.reload();

  await page.waitForFunction(() => {
    const c = document.querySelector(".visit-canvas");
    return c?.dataset.ready === "true" && c.dataset.sector === "2";
  });
  assert.equal(
    await root
      .getByRole("button", { name: "Вернуться в космос", exact: true })
      .count(),
    0,
  );
  const denied = await page.evaluate(async () => {
    const { api } = await import(new URL("./core/api.js", document.querySelector('script[type="module"][src$="bootstrap.js"]').src));
    const { uuid } = await import(new URL("./core/uuid.js", document.querySelector('script[type="module"][src$="bootstrap.js"]').src));
    const visit = await api("planet/visit");
    const response = await fetch("/api/planet/return", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ operation: uuid(), visit: visit.visit }),
    });
    return {
      status: response.status,
      state: (await api("planet/visit")).state,
    };
  });
  assert.equal(denied.status, 403);
  assert.equal(denied.state, "active");
  assert.deepEqual(errors, []);
  console.log(
    "PASS: browser crossing offer, cancel, explicit reselect, confirm, scene swap, reload in sector without original module, arbitrary return blocked; no page errors",
  );
  await context.close();
} finally {
  await browser.close();
}
