// The real production page, Go API and disposable PostgreSQL fixture; no route mocks.
import assert from "node:assert/strict";
import { readFileSync, mkdirSync } from "node:fs";
import { chromium } from "playwright";
const f = JSON.parse(readFileSync(0, "utf8")),
  base = new URL(process.env.TERMINAL_TEST_URL);
assert.ok(["127.0.0.1", "localhost", "[::1]"].includes(base.hostname));
assert.equal(
  (await fetch(base)).headers.get("x-cosmos-test-run"),
  process.env.TERMINAL_TEST_NONCE,
);
const browser = await chromium.launch({
  executablePath: process.env.CHROMIUM_EXECUTABLE_PATH || undefined,
  args: process.env.CHROMIUM_NO_SANDBOX === "1" ? ["--no-sandbox"] : [],
});
try {
  const context = await browser.newContext({
    viewport: { width: 1440, height: 1000 },
  });
  await context.addCookies([
    { name: f.cookie_name, value: f.cookie, url: base.href },
  ]);
  const page = await context.newPage();
  page.setDefaultTimeout(12000);
  const errors = [],
    bad = [],
    writes = [],
    sockets = [];
  page.on("pageerror", (e) => errors.push(e.message));
  page.on("websocket", (s) => sockets.push(s.url()));
  page.on("request", (r) => {
    if (r.method() === "POST" && r.url().includes("/api/"))
      writes.push({ path: new URL(r.url()).pathname, body: r.postDataJSON() });
  });
  page.on("response", (r) => {
    if (r.status() >= 400 && !r.url().endsWith("/favicon.ico"))
      bad.push(`${r.status()} ${new URL(r.url()).pathname}`);
  });
  const api = async (path) => {
    const r = await page.request.get(new URL("/api/" + path, base).href);
    assert.equal(r.status(), 200);
    return r.json();
  };
  const module = async (id, open = true) =>
    page.evaluate(
      async ({ id, open }) => {
        const a = (await import("/bootstrap.js")).application;
        a.Modules[open ? "open" : "close"](id);
      },
      { id, open },
    );
  const refresh = () =>
    page.evaluate(async () =>
      (await import("/core/snapshot-store.js")).refresh(true),
    );
  mkdirSync(new URL("./artifacts/notifications/", import.meta.url), {
    recursive: true,
  });
  const root = page.locator("#window-notifications"),
    rows = root.locator(".notification-compact-row");
  const choose = async (id) => {
    await root
      .locator(
        `.notification-compact-row[data-id="${id}"] .notification-compact-open`,
      )
      .click();
    await root.locator(".notification-compact-detail-panel").waitFor();
  };
  const done = () =>
    root
      .getByRole("button", { name: "Закрыть подробности", exact: true })
      .click();
  const menu = async (name) => {
    await root
      .getByRole("button", { name: "Действия с уведомлениями", exact: true })
      .click();
    await page.getByRole("menuitem", { name, exact: true }).click();
  };
  const idle = () =>
    page.waitForFunction(
      () =>
        document.querySelector(".notification-compact-statusbar>span")
          ?.textContent !== "Сохраняем…",
    );
  await page.goto(base.href);
  await page.locator("#game:not([hidden])").waitFor();
  await module("notifications");
  await rows.first().waitFor();
  assert.equal(await rows.count(), 500);
  assert.equal((await api("notifications")).count, 516);
  assert.equal(await page.locator("#notification-count").textContent(), "99+");
  assert.equal(
    await root.locator(".notification-compact-count").textContent(),
    "99+",
  );
  for (const kind of [
    "system",
    "economy",
    "research",
    "corporation",
    "gameplay",
  ])
    assert.ok((await root.locator(`[data-kind="${kind}"]`).count()) > 0, kind);
  // Unknown kinds are neutral and external strings are rendered as text.
  assert.equal(
    await root
      .locator(`[data-id="${f.ids.constructor}"]`)
      .getAttribute("data-kind"),
    "system",
  );
  await choose(f.ids.message);
  assert.match(
    await root.locator(".notification-compact-detail-text").textContent(),
    /<img src=x onerror=alert\(1\)>/,
  );
  assert.equal(
    await root.locator(".notification-compact-detail-text img").count(),
    0,
  );
  await root
    .getByRole("button", { name: "Закрыть подробности", exact: true })
    .focus();
  await refresh();
  assert.equal(
    await root
      .getByRole("button", { name: "Закрыть подробности", exact: true })
      .evaluate((el) => el === document.activeElement),
    true,
    "polling must preserve detail focus",
  );
  await root.screenshot({
    path: new URL("./artifacts/notifications/detail.png", import.meta.url)
      .pathname,
  });
  await page.keyboard.press("Escape");
  assert.equal(await root.evaluate((el) => el.open), true);
  assert.equal(
    await root.locator(".notification-compact-detail-backdrop").isVisible(),
    false,
  );
  assert.equal((await api("expedition")).ship.id, f.ship);
  await choose(f.ids.mining_complete);
  assert.equal(
    (await api("expedition")).ship.id,
    f.ship,
    "opening details must not switch a ship",
  );
  await root.getByRole("button", { name: "Корабль", exact: true }).click();
  await page.waitForFunction(
    async (ship) =>
      (await (await fetch("/api/expedition")).json()).ship.id === ship,
    f.second,
  );
  await page.locator("#window-ship[open]").waitFor();
  await module("ship", false);
  await module("notifications");
  await done();
  await choose(f.ids.mining_complete);
  await root.getByRole("button", { name: "Трюм", exact: true }).click();
  await page.locator("#window-cargo[open]").waitFor();
  await module("cargo", false);
  await module("notifications");
  await done();
  await choose(f.ids.arrival);
  await root.getByRole("button", { name: "На карте", exact: true }).click();
  await page.locator("#window-galaxy[open]").waitFor();
  await module("galaxy", false);
  await module("notifications");
  await done();
  await choose(f.ids.landing_ready);
  await root
    .getByRole("button", { name: "Открыть поверхность", exact: true })
    .click();
  await page.locator("#window-galaxy[open] #planet-map").waitFor();
  await module("galaxy", false);
  await module("notifications");
  await done();
  await choose(f.ids.colony);
  await root
    .getByRole("button", { name: "Открыть проект", exact: true })
    .click();
  await page
    .locator("#window-colony-project-" + f.project + "[open]")
    .waitFor();
  await module("colony-project-" + f.project, false);
  await module("notifications");
  await done();
  assert.equal(
    writes.filter((r) => r.body?.action === "select_ship").length,
    1,
    "only the explicit ship action may select a ship",
  );
  // Read one, grouped rendering and server data must agree.
  await root
    .locator(
      `[data-id="${f.ids.research_complete}"] .notification-compact-mark-one`,
    )
    .click();
  await idle();
  await page.waitForFunction(
    (id) =>
      document.querySelector(`[data-id="${id}"]`)?.dataset.read === "true",
    f.ids.research_complete,
  );
  assert.equal((await api("notifications")).unread, 515);
  await root.locator('[data-filter="unread"]').click();
  assert.equal(
    await root.locator(`[data-id="${f.ids.research_complete}"]`).count(),
    0,
  );
  await root.locator('[data-filter="all"]').click();
  // Genuine Modules geometry and scrolling at the three requested sizes.
  mkdirSync(new URL("./artifacts/notifications/", import.meta.url), {
    recursive: true,
  });
  for (const [width, height] of [
    [420, 540],
    [360, 420],
    [320, 360],
  ]) {
    await root.evaluate(
      (el, { width, height }) => {
        el.style.width = width + "px";
        el.style.height = height + "px";
        el.style.left = "820px";
        el.style.top = "150px";
      },
      { width, height },
    );
    await page.waitForTimeout(50);
    assert.equal(await root.locator(".module-content").isVisible(), true);
    const scroll = root.locator(".notification-compact-scroll");
    await scroll.evaluate((el) => (el.scrollTop = 0));
    const before = await page.evaluate(() => ({ x: scrollX, y: scrollY }));
    await scroll.hover();
    await page.mouse.wheel(0, 500);
    await page.waitForFunction(
      () =>
        document.querySelector(".notification-compact-scroll").scrollTop > 0,
    );
    assert.deepEqual(
      await page.evaluate(() => ({ x: scrollX, y: scrollY })),
      before,
    );
    await scroll.evaluate((el) => (el.scrollTop = el.scrollHeight));
    await page.mouse.wheel(0, 900);
    assert.deepEqual(
      await page.evaluate(() => ({ x: scrollX, y: scrollY })),
      before,
    );
    await scroll.evaluate((el) => (el.scrollTop = 0));
    await root.screenshot({
      path: new URL(
        `./artifacts/notifications/${width}x${height}.png`,
        import.meta.url,
      ).pathname,
    });
  }
  await root.evaluate((el) => {
    el.style.width = "420px";
    el.style.height = "540px";
    el.style.left = "750px";
    el.style.top = "130px";
  });
  const bar = root.locator(".module-titlebar"),
    box = await bar.boundingBox();
  await page.mouse.move(box.x + 110, box.y + 17);
  await page.mouse.down();
  await page.mouse.move(box.x + 145, box.y + 42, { steps: 4 });
  await page.mouse.up();
  const moved = await root.boundingBox();
  assert.ok(moved.x > 750 && moved.y > 130);
  await root.locator(".window-actions button").first().click();
  assert.equal(
    await root
      .locator(".window-actions button")
      .first()
      .getAttribute("aria-pressed"),
    "true",
  );
  await root.locator(".window-actions button").first().click();
  await page.waitForTimeout(250);
  await module("notifications", false);
  await module("notifications");
  const reopened = await root.boundingBox();
  assert.ok(
    Math.abs(reopened.x - moved.x) < 2 && Math.abs(reopened.y - moved.y) < 2,
    "Modules must restore the same position",
  );
  // Select all 500 loaded records, not all 516. Old unread records must remain visible as a limit state.
  await menu("Выбрать уведомления");
  await root
    .getByRole("checkbox", { name: "Выбрать все показанные", exact: true })
    .check();
  await menu("Отметить выбранные прочитанными");
  await idle();
  await root.locator('[data-filter="unread"]').click();
  await root
    .getByText("Есть более ранние непрочитанные уведомления", { exact: true })
    .waitFor();
  assert.equal((await api("notifications")).unread, 16);
  assert.equal(await page.locator("#notification-count").textContent(), "16");
  await root
    .getByRole("button", { name: "Прочитать всё", exact: true })
    .click();
  await idle();
  await page.waitForFunction(
    () => document.getElementById("notification-count").hidden,
  );
  assert.equal((await api("notifications")).unread, 0);
  assert.equal((await api("notifications")).count, 516);
  // Single and selected deletion retain the production ownership and bulk contracts.
  await root.locator('[data-filter="all"]').click();
  await root.getByRole("button", { name: "Готово", exact: true }).click();
  await choose(f.ids.constructor);
  await root
    .getByRole("button", { name: "Удалить уведомление", exact: true })
    .click();
  await idle();
  await page.waitForFunction(
    () =>
      document.querySelector(".notification-compact-detail-backdrop").hidden,
  );
  assert.equal((await api("notifications")).count, 515);
  await menu("Выбрать уведомления");
  await root.locator(".notification-compact-row input").nth(0).check();
  await root.locator(".notification-compact-row input").nth(1).check();
  await menu("Удалить выбранные");
  await idle();
  assert.equal((await api("notifications")).count, 513);
  await page.evaluate(async () =>
    (await import("/bootstrap.js")).application.I18n.set("en"),
  );
  await root.getByRole("button", { name: "Read all", exact: true }).waitFor();
  assert.equal(
    await root
      .getByRole("button", { name: "Read all", exact: true })
      .isDisabled(),
    true,
  );
  await root
    .getByRole("button", { name: "Notification actions", exact: true })
    .click();
  await page.getByRole("menuitem", { name: "Clear all", exact: true }).click();
  await idle();
  await root.getByText("All quiet", { exact: true }).waitFor();
  assert.equal((await api("notifications")).count, 0);
  // A new account never inherits the previous selection or text.
  await page.evaluate(async () =>
    (await import("/bootstrap.js")).application.showAuth(),
  );
  await context.clearCookies();
  await context.addCookies([
    { name: f.cookie_name, value: f.peer_cookie, url: base.href },
  ]);
  await page.reload();
  await page.locator("#game:not([hidden])").waitFor();
  await module("notifications");
  await root.getByText("Peer private notification", { exact: true }).waitFor();
  assert.equal(await rows.count(), 1);
  assert.equal(
    await root.locator(".notification-compact-selection").isVisible(),
    false,
  );
  assert.deepEqual(errors, []);
  assert.deepEqual(bad, []);
  assert.equal(
    sockets.length,
    0,
    "notifications must not open a chat/WebSocket connection",
  );
  console.log(
    "PASS real API: A UI, unread vs count and 500 limit, all categories, safe full text, read/selected/all/delete/clear, explicit ship and all navigation, 420/360/320 scrolling, window geometry, RU/EN and account isolation; no extra sockets or page errors",
  );
} finally {
  await browser.close();
}
