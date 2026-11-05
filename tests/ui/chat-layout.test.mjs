import assert from "node:assert/strict";
import { test } from "node:test";
import { readFile } from "node:fs/promises";
import { game } from "./harness.mjs";

const message = {
  id: "00000000-0000-4000-8000-000000000001",
  seq: "1",
  author_id: "72",
  author: "TestPilot",
  body: "Проверка оформления чата",
  sent_at: "2026-11-05T10:00:00Z",
  entities: [],
};
async function chat(t) {
  const g = await game(t, { signedIn: true });
  await g.page.route("**/chat/api/v1/**", (route) => {
    const path = new URL(route.request().url()).pathname.split("/").at(-1);
    const body =
      path === "bootstrap"
        ? {
            user: { id: String(g.data.state.user.id), role: "player" },
            conversations: [],
            invitations: [],
            folders: [
              { id: "folder-1", name: "Маршруты" },
              { id: "folder-2", name: "Полезное" },
            ],
            settings: {},
            corporation: [],
            presence: [],
            public_new: [],
          }
        : path === "roster"
          ? [{ id: "72", name: "TestPilot", tag: "TEST", online: true }]
          : path === "history"
            ? [message]
            : path === "favorites"
              ? [
                  {
                    ...message,
                    kind: "copy",
                    saved_at: message.sent_at,
                    folder_id: "folder-1",
                  },
                ]
              : [];
    return route.fulfill({ json: body });
  });
  await g.page.routeWebSocket("**/chat/ws/v1", (ws) => {
    ws.send(
      JSON.stringify({
        type: "hello",
        health: { ready: true, cosmic: true, planet: true },
      }),
    );
    ws.onMessage((raw) => {
      const c = JSON.parse(raw);
      ws.send(
        JSON.stringify({ type: "ack", request_id: c.request_id, result: {} }),
      );
    });
  });
  await g.page.evaluate(() => {
    window.__testApp.session.data.state.chat_enabled = true;
    window.dispatchEvent(new Event("gamedata"));
  });
  await g.open("chat");
  await g.page.locator(".chat-msg-text").first().waitFor();
  return g;
}
test("chat follows v13 spacing, shared header and compact menus for click and right-click", async (t) => {
  const g = await chat(t),
    root = g.page.locator(".chat-root"),
    dialog = g.page.locator("#window-chat");
  assert.equal(await dialog.locator(".module-titlebar").count(), 1);
  const geometry = await dialog.evaluate((d) => {
    const r = d.getBoundingClientRect(),
      c = d.querySelector(".chat-root").getBoundingClientRect();
    return { right: r.right - c.right, left: c.left - r.left };
  });
  assert.ok(geometry.right <= 2, JSON.stringify(geometry));
  assert.ok(geometry.left <= 2);
  assert.equal(
    await g.page.locator('#sidebar [data-open="chat"] svg').count(),
    1,
  );

  assert.equal(await dialog.locator(":scope > .module-titlebar").count(), 0);
  assert.equal(await root.locator(".chat-reply-preview").isVisible(), false);
  assert.equal(await root.locator(".chat-search-panel").isVisible(), false);
  assert.ok(
    (await root
      .locator(".chat-compose-main")
      .evaluate(
        (e) =>
          e.getBoundingClientRect().top -
          e.parentElement.getBoundingClientRect().top,
      )) <= 9,
  );
  await root.locator(".chat-signal").hover();
  await g.page.locator("#tooltip .chat-status-tooltip-row").first().waitFor();
  assert.equal(
    await g.page.locator("#tooltip .chat-service-square").count(),
    2,
  );
  await root.getByRole("button", { name: "Игроки", exact: true }).click();
  const roster = g.page.locator("#window-chat-roster");
  await roster.locator(".chat-player-name").waitFor();
  assert.equal(
    await roster.locator(".chat-player-name strong").textContent(),
    "TestPilot",
  );
  assert.equal(
    await roster.locator(".chat-player-name span").textContent(),
    "[TEST]",
  );
  await roster.getByRole("searchbox").fill("missing");
  assert.equal(await roster.locator(".chat-players-total").textContent(), "0");
  await g.page.evaluate(() => window.__testApp.Modules.close("chat-roster"));
  const header = root.locator(".chat-titlebar");
  const old = await dialog.boundingBox(),
    hr = await header.boundingBox();
  await g.page.mouse.move(hr.x + 60, hr.y + 15);
  await g.page.mouse.down();
  await g.page.mouse.move(hr.x + 100, hr.y + 45);
  await g.page.mouse.up();
  const moved = await dialog.boundingBox();
  assert.ok(Math.abs(moved.x - old.x - 40) < 2);
  assert.ok(Math.abs(moved.y - old.y - 30) < 2);
  const prototype = await readFile(
    new URL(
      "../../docs/ideas/chat-service/chat-service-ui-prototype-v13.html",
      import.meta.url,
    ),
    "utf8",
  );
  const referenceContext = await g.context.browser().newContext();
  t.after(() => referenceContext.close());
  const ref = await referenceContext.newPage();
  await ref.setContent(
    `<style>${prototype.match(/<style>([\s\S]*?)<\/style>/)[1]}</style><div class="chat-context-menu"><button>Ответить</button><button>Сохранить в избранное</button></div>`,
  );
  const appearance = (e) => {
    const s = getComputedStyle(e),
      b = e.querySelector("button"),
      bs = getComputedStyle(b);
    return {
      background: s.backgroundColor,
      border: s.borderTopColor,
      padding: s.padding,
      minWidth: s.minWidth,
      rowHeight: b.getBoundingClientRect().height,
      font: bs.fontSize,
      lineHeight: bs.lineHeight,
      buttonPadding: bs.padding,
    };
  };
  const expected = await ref.locator(".chat-context-menu").evaluate(appearance);
  await root.locator(".chat-more").click();
  let menu = g.page.locator(".context-menu:not([hidden])");
  assert.deepEqual(await menu.evaluate(appearance), expected);
  await menu.getByRole("menuitem", { name: "Ответить", exact: true }).click();
  await root.locator(".chat-reply-preview").waitFor({ state: "visible" });
  await root.locator(".chat-reply-preview button").click();
  assert.equal(await root.locator(".chat-reply-preview").isVisible(), false);
  await g.page.evaluate(() =>
    document.querySelector("#overview-upgrade").focus(),
  );
  await root.locator(".chat-msg-text").click({ button: "right" });
  assert.deepEqual(await menu.evaluate(appearance), expected);
  await g.page.keyboard.press("Escape");
  await root.locator(".chat-tab").first().click();
  await root.locator(".chat-favorite-card").waitFor();
  assert.equal(await root.locator(".chat-compose").isVisible(), false);
  assert.equal(await root.locator(".chat-folder").count(), 3);
  await root.locator(".chat-favorite-card").click({ button: "right" });
  await menu.getByRole("menuitem", { name: "Переместить в папку" }).hover();
  const sub = g.page.locator(".context-submenu");
  await sub.waitFor();
  assert.equal(
    await sub
      .getByRole("menuitemradio", { name: "Маршруты" })
      .getAttribute("aria-checked"),
    "true",
  );
  assert.equal(
    await sub
      .getByRole("menuitemradio", { name: "Без папки (Все)" })
      .getAttribute("aria-checked"),
    "false",
  );
  assert.deepEqual(await sub.evaluate(appearance), expected);
  await g.page.keyboard.press("Escape");
  await root.getByRole("button", { name: "На главную", exact: true }).click();
  assert.equal(await dialog.isVisible(), false);
  assert.equal(
    await g.page
      .locator('#sidebar [data-open="chat"]')
      .evaluate((e) => e.classList.contains("module-open")),
    true,
  );
  assert.equal(
    await g.page.locator("#chat-home-slot .chat-titlebar").count(),
    1,
  );
  const homeHeader = await header.boundingBox();
  await g.page.mouse.move(homeHeader.x + 60, homeHeader.y + 15);
  await g.page.mouse.down();
  await g.page.mouse.move(homeHeader.x + 100, homeHeader.y + 30);
  await g.page.mouse.up();
  await root
    .getByRole("button", { name: "В отдельное окно", exact: true })
    .click();
  const returned = await dialog.boundingBox();
  assert.ok(Math.abs(returned.x - moved.x) < 2);
  assert.ok(Math.abs(returned.y - moved.y) < 2);
  await root.locator(".chat-tab").nth(1).click();
  await root.locator(".chat-msg-text").waitFor();
  await root.locator(".chat-more").click();
  await dialog.screenshot({ path: "/tmp/chat-layout-fixed.png" });
  await g.page.keyboard.press("Escape");
  await g.page.evaluate(() =>
    window.__testApp.UI.menu([{ label: "Обычное меню" }], 100, 100, {
      theme: "default",
      owner: document.body,
    }),
  );
  assert.equal(
    await menu.evaluate((e) => e.classList.contains("chat-compact")),
    false,
  );
});
