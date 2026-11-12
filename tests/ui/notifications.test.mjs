import assert from "node:assert/strict";
import { test } from "node:test";
import { game } from "./harness.mjs";

const payload = (id = 1, message = "Private note A") => ({
  count: 1,
  unread: 1,
  items: [
    {
      id,
      kind: "message",
      level: "success",
      message,
      data: {},
      seen: false,
      created_at: "2026-11-12T06:00:00Z",
    },
  ],
});
async function start(t) {
  const g = await game(t, { signedIn: true });
  await g.ready();
  g.data.notifications = payload();
  await g.refresh();
  await g.open("notifications");
  await g.page.locator(".notification-compact-row").waitFor();
  return g;
}
const frame = (page) =>
  page.evaluate(
    () =>
      new Promise((resolve) =>
        requestAnimationFrame(() => requestAnimationFrame(resolve)),
      ),
  );
const posts = (g) =>
  g.requests.filter((r) => r.path === "notifications" && r.method === "POST");

test("notification write failure preserves unread state, suppresses double clicks and permits retry", async (t) => {
  const g = await start(t);
  let release;
  const gate = new Promise((resolve) => (release = resolve));
  g.handlers.set("notifications", async ({ req, json }) => {
    if (req.method() === "POST") {
      await gate;
      return json({ error: "Temporary notification failure" }, 503);
    }
    return json(g.data.notifications);
  });
  const read = g.page.locator(".notification-compact-mark-one");
  await read.click();
  await g.page.waitForFunction(
    () => document.querySelector(".notification-compact-mark-one").disabled,
  );
  await g.page.evaluate(() =>
    document.querySelector(".notification-compact-mark-one").click(),
  );
  assert.equal(posts(g).length, 1);
  release();
  await g.page.waitForFunction(
    () => !document.querySelector(".notification-compact-mark-one").disabled,
  );
  assert.equal(
    await g.page
      .locator('.notification-compact-row[data-read="false"]')
      .count(),
    1,
  );
  assert.equal(await g.page.locator("#notification-count").textContent(), "1");
  assert.match(
    await g.page.locator('#toast[data-level="error"]').textContent(),
    /Temporary notification failure/,
  );
  g.handlers.set("notifications", ({ req, json }) => {
    if (req.method() === "POST") {
      g.data.notifications.items[0].seen = true;
      g.data.notifications.unread = 0;
    }
    return json(g.data.notifications);
  });
  await read.click();
  await g.page.waitForSelector('.notification-compact-row[data-read="true"]');
  assert.equal(posts(g).length, 2);
  assert.equal(await g.page.locator("#notification-count").isVisible(), false);
});

test("a snapshot started before seen cannot restore an unread entry after the write", async (t) => {
  const g = await start(t);
  let release;
  const gate = new Promise((resolve) => (release = resolve));
  let old = true;
  const previous = structuredClone(g.data.notifications);
  g.handlers.set("notifications", async ({ req, json }) => {
    if (req.method() === "POST") {
      g.data.notifications.items[0].seen = true;
      g.data.notifications.unread = 0;
      return json(g.data.notifications);
    }
    if (old) {
      old = false;
      await gate;
      return json(previous);
    }
    return json(g.data.notifications);
  });
  const requested = g.page.waitForRequest(
    (r) => new URL(r.url()).pathname === "/api/notifications",
  );
  await g.page.evaluate(() => {
    window.pendingNotificationSnapshot = window.__testApp.refresh(true);
  });
  await requested;
  await g.page.locator(".notification-compact-mark-one").click();
  await g.page.waitForFunction(
    () =>
      document.querySelector(".notification-compact-statusbar>span")
        .textContent === "Сохраняем…",
  );
  release();
  await g.page.waitForSelector('.notification-compact-row[data-read="true"]');
  await frame(g.page);
  assert.equal(
    await g.page
      .locator('.notification-compact-row[data-read="false"]')
      .count(),
    0,
  );
  assert.equal(await g.page.locator("#notification-count").isVisible(), false);
});

for (const operation of ["read", "write"])
  test(`late notification ${operation} cannot restore data into another account`, async (t) => {
    const g = await start(t);
    let release;
    const gate = new Promise((resolve) => (release = resolve));
    const previous = structuredClone(g.data.notifications);
    g.handlers.set("notifications", async ({ req, json }) => {
      if ((operation === "write") === (req.method() === "POST")) {
        await gate;
        return json(previous);
      }
      return json(g.data.notifications);
    });
    const requested = g.page.waitForRequest(
      (r) =>
        new URL(r.url()).pathname === "/api/notifications" &&
        r.method() === (operation === "write" ? "POST" : "GET"),
    );
    if (operation === "write")
      await g.page.locator(".notification-compact-mark-one").click();
    else
      await g.page.evaluate(() =>
        window.dispatchEvent(new Event("notificationchange")),
      );
    await requested;
    await g.page.evaluate(() => window.__testApp.showAuth());
    assert.equal(
      await g.page
        .locator("#window-notifications .module-content")
        .textContent(),
      "",
    );
    g.handlers.delete("notifications");
    g.data.state.user.id = 52;
    g.data.state.user.username = "other_pilot";
    g.data.notifications = payload(22, "Private note B");
    await g.login();
    await g.open("notifications");
    await g.page.getByText("Private note B", { exact: true }).waitFor();
    const delivered = g.page.waitForResponse(
      (r) =>
        new URL(r.url()).pathname === "/api/notifications" &&
        r.request().method() === (operation === "write" ? "POST" : "GET"),
    );
    release();
    await delivered;
    await frame(g.page);
    assert.equal(
      await g.page.getByText("Private note A", { exact: true }).count(),
      0,
    );
    assert.equal(
      await g.page.getByText("Private note B", { exact: true }).count(),
      1,
    );
    assert.equal(
      await g.page.locator(".notification-compact-mark-one").isEnabled(),
      true,
    );
    assert.equal(
      await g.page.locator("#notification-count").textContent(),
      "1",
    );
  });
