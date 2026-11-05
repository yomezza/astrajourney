// Only the Go fixture owns the server and disposable PostgreSQL behind this test.
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { chromium } from "playwright";
const input = JSON.parse(readFileSync(0, "utf8")),
  base = process.env.TERMINAL_TEST_URL;
assert.ok(/^http:\/\/127\.0\.0\.1:\d+$/.test(base));
assert.equal(
  (await fetch(base)).headers.get("x-cosmos-test-run"),
  process.env.TERMINAL_TEST_NONCE,
);
const browser = await chromium.launch({
  executablePath: process.env.CHROMIUM_EXECUTABLE_PATH || "/usr/bin/chromium",
  args: process.env.CHROMIUM_NO_SANDBOX === "1" ? ["--no-sandbox"] : [],
});
try {
  const context = await browser.newContext({
    viewport: { width: 1440, height: 1000 },
  });
  await context.addCookies([
    { name: "session", value: input.token, url: base },
  ]);
  const page = await context.newPage(),
    errors = [];
  let sockets = 0;
  page.on("pageerror", (e) => errors.push(e.message));
  page.on("websocket", () => sockets++);
  await page.goto(base);
  await page.locator("#sidebar").waitFor({ state: "visible" });
  await page.locator('#sidebar [data-open="chat"]').click();
  const root = page.locator(".chat-root");
  await root.locator(".chat-signal.healthy").waitFor();
  await root.locator(".chat-tab").nth(1).click();
  await root.locator("textarea").fill("Real PostgreSQL browser chat ✨");
  await root.locator('.chat-send, .chat-compose button[type="submit"]').click();
  await root
    .getByText("Real PostgreSQL browser chat ✨", { exact: true })
    .waitFor();
  await root.locator(".chat-tab").nth(0).click();
  await root.locator("textarea").fill("Personal test note");
  await root.locator('.chat-compose button[type="submit"]').click();
  await root.getByText("Personal test note", { exact: true }).waitFor();
  const original = await root.evaluate((el) => {
    el.dataset.instance = "single";
    return true;
  });
  assert.ok(original);
  await root.getByRole("button", { name: "На главную", exact: true }).click();
  await page.locator("#chat-home-slot .chat-root").waitFor();
  assert.equal(await root.getAttribute("data-instance"), "single");
  assert.equal(
    await root.evaluate((el) => Math.round(el.getBoundingClientRect().height)),
    825,
  );
  assert.equal(sockets, 1);
  await root
    .getByRole("button", { name: "В отдельное окно", exact: true })
    .click();
  await page.locator("#window-chat .chat-root").waitFor();
  assert.equal(sockets, 1);
  await root.locator('[aria-label="Настройки чата"]').click();
  await root.locator('input[type="range"]').evaluate((el) => {
    el.value = "40";
    el.dispatchEvent(new Event("input"));
    el.dispatchEvent(new Event("change"));
  });
  assert.equal(await root.evaluate((el) => getComputedStyle(el).opacity), "1");
  await root.locator(".chat-emoji-trigger").click();
  await root.locator(".chat-emoji-grid button").first().click();
  await root.locator(".chat-emoji-grid button").nth(1).click();
  assert.ok(await root.locator(".chat-emoji-panel").isVisible());
  assert.ok(
    Array.from(await root.locator("textarea").inputValue()).length >= 2,
  );
  for (const width of [375, 900, 1280, 1440]) {
    await page.setViewportSize({ width, height: 1000 });
    await root.locator(".chat-emoji-close, .chat-emoji-head button").click();
    const r = await page.locator("#window-chat").boundingBox();
    assert.ok(r.width <= width, JSON.stringify(r));
    const tabs = await root.locator(".chat-tab").count();
    assert.equal(tabs, 6);
    if (width !== 1440) await root.locator(".chat-emoji-trigger").click();
  }
  await page.setViewportSize({ width: 1440, height: 1000 });
  await page
    .locator("#window-chat")
    .screenshot({ path: process.env.CHAT_SCREENSHOT || "/tmp/chat240.png" });
  assert.deepEqual(errors, []);
  console.log(
    JSON.stringify({
      browser: "chromium",
      real_postgres: true,
      websockets: sockets,
      viewports: [375, 900, 1280, 1440],
      errors,
    }),
  );
  await context.close();
} finally {
  await browser.close();
}
