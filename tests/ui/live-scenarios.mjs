// Invoked by TestTerminalBrowser against its own nonce-marked httptest server.
import assert from "node:assert/strict";
import { chromium } from "playwright";
const base = new URL(process.env.TERMINAL_TEST_URL);
assert.ok(["127.0.0.1", "localhost", "[::1]"].includes(base.hostname));
const check = await fetch(base);
assert.equal(
  check.headers.get("x-cosmos-test-run"),
  process.env.TERMINAL_TEST_NONCE,
);
assert.ok(process.env.TERMINAL_TEST_NONCE?.startsWith("terminal-test-"));
const browser = await chromium.launch({
  ...(process.env.CHROMIUM_EXECUTABLE_PATH
    ? { executablePath: process.env.CHROMIUM_EXECUTABLE_PATH }
    : {}),
  ...(process.env.CHROMIUM_NO_SANDBOX === "1"
    ? { args: ["--no-sandbox"] }
    : {}),
});
const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } });
page.setDefaultTimeout(15000);
const errors = [],
  badResponses = [],
  pending = new Set();
page.on("pageerror", (e) => errors.push(e.message));
page.on("request", (r) => {
  if (new URL(r.url()).pathname.startsWith("/api/")) pending.add(r);
});
page.on("requestfinished", (r) => pending.delete(r));
page.on("requestfailed", (r) => pending.delete(r));
page.on("response", (r) => {
  if (
    r.status() >= 400 &&
    new URL(r.url()).pathname.startsWith("/api/") &&
    r.status() !== 401
  )
    badResponses.push(`${r.status()} ${new URL(r.url()).pathname}`);
});
async function settle() {
  const end = Date.now() + 15000;
  while (pending.size && Date.now() < end) await page.waitForTimeout(30);
  assert.equal(pending.size, 0, "requests did not settle");
  await page.evaluate(
    () =>
      new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r))),
  );
}
async function open(id) {
  await page.evaluate(
    async (id) => (await import(new URL("./bootstrap.js", document.querySelector('script[type="module"][src$="bootstrap.js"]').src))).application.Modules.open(id),
    id,
  );
  await page.waitForFunction(
    (id) => document.getElementById("window-" + id)?.open,
    id,
  );
  await settle();
  const root = page.locator("#window-" + id + " .module-content");
  assert.ok((await root.textContent()).trim(), id + " is empty");
  assert.deepEqual(errors, [], id);
  assert.deepEqual(badResponses, [], id);
  assert.equal(
    await page.locator('#toast[data-level="error"]:visible').count(),
    0,
    id + " error toast",
  );
  return root;
}
async function close(id) {
  await page.evaluate(
    async (id) => (await import(new URL("./bootstrap.js", document.querySelector('script[type="module"][src$="bootstrap.js"]').src))).application.Modules.close(id),
    id,
  );
}
try {
  await page.goto(base.href);
  await page.waitForSelector("#auth:not([hidden])");
  await page
    .locator('#auth-form input[name="username"]')
    .fill("terminal_audit");
  await page.locator('#auth-form input[name="password"]').fill("1");
  await page.locator("#submit").click();
  await page.waitForSelector("#game:not([hidden])");
  await settle();
  assert.equal(await page.evaluate(() => typeof window.refresh), "undefined");
  for (const language of ["ru", "en"]) {
    await page.evaluate(
      async (lang) =>
        (await import(new URL("./bootstrap.js", document.querySelector('script[type="module"][src$="bootstrap.js"]').src))).application.I18n.set(lang),
      language,
    );
    await settle();
    for (const id of [
      "ship",
      "cargo",
      "market",
      "corporations",
      "profile",
      "research",
      "studies",
      "guide",
      "finance",
      "notifications",
      "tasks",
      "logistics",
      "ideas",
      "users",
      "gameplay",
      "galaxies",
      "adminchanges",
      "benchmark",
      "adminideas",
      "settings",
      "galaxy",
    ]) {
      await open(id);
      await close(id);
      console.log(`OPEN ${language} ${id}`);
    }
  }
  await page.evaluate(async () =>
    (await import(new URL("./bootstrap.js", document.querySelector('script[type="module"][src$="bootstrap.js"]').src))).application.I18n.set("ru"),
  );
  await settle();
  await page.locator("#nav-colonies").click();
  await page.waitForSelector("#window-colonies[open]");
  await settle();
  await close("colonies");
  await open("ship");
  await page.locator("#ship-rename").fill("Audit Pilgrim");
  const renamed = page.waitForResponse(response =>
    new URL(response.url()).pathname === "/api/workbench" &&
    response.request().method() === "POST" &&
    response.request().postDataJSON()?.action === "rename_ship",
  );
  await page
    .locator("#window-ship")
    .getByRole("button", { name: "Переименовать корабль", exact: true })
    .click();
  assert.ok((await renamed).ok(), "ship rename failed");
  await page.waitForFunction(() =>
    document.getElementById("title-ship").textContent.includes("Audit Pilgrim"));
  await settle();
  await close("ship");
  await open("logistics");
  await page
    .locator('.storage-station[data-station="1|station"] summary')
    .click();
  await page
    .locator('[data-inventory="personal"] [data-resource="iron"]')
    .click();
  await page
    .getByRole("menuitem", { name: "Переместить ресурс", exact: true })
    .click();
  await settle();
  await page
    .locator("#window-cargo-destination")
    .getByRole("button", { name: "Продолжить", exact: true })
    .click();
  await page.locator("#cargo-transfer-quantity").fill("3");
  await page
    .locator("#window-cargo-transfer")
    .getByRole("button", { name: "Переместить", exact: true })
    .click();
  await settle();
  const state = await page.request.get(new URL("/api/expedition", base).href);
  assert.equal((await state.json()).ship.cargo.iron, 9);
  assert.equal(
    await page.locator('#toast[data-level="error"]:visible').count(),
    0,
  );
  console.log("ACTION warehouse -> cargo, ship rename, colonization manager");

  async function snapshot() {
    return (
      await page.request.get(new URL("/api/expedition", base).href)
    ).json();
  }
  await close("logistics");
  await open("ship");
  const beforeFuel = await snapshot();
  await page
    .locator("#window-ship")
    .getByRole("button", { name: "Заправить", exact: true })
    .click();
  await settle();
  assert.equal((await snapshot()).ship.fuel, beforeFuel.ship.fuel + 5);
  assert.equal(
    (await snapshot()).ship.cargo.agni,
    beforeFuel.ship.cargo.agni - 1,
  );
  await close("ship");
  await open("market");
  await page
    .locator("#window-market")
    .getByRole("tab", { name: "Локальный рынок", exact: true })
    .click();
  await settle();
  await page
    .locator("#window-market")
    .getByRole("button", { name: "Железо", exact: true })
    .click();
  await settle();
  const beforeTrade = await snapshot();
  await page.locator("#trade-quantity").fill("1");
  await page
    .locator("#window-market")
    .getByRole("button", { name: "Купить в трюм", exact: true })
    .click();
  await settle();
  assert.equal(
    (await snapshot()).ship.cargo.iron,
    beforeTrade.ship.cargo.iron + 1,
  );
  await page
    .locator("#window-market")
    .getByRole("button", { name: "Продать из трюма", exact: true })
    .click();
  await settle();
  assert.equal((await snapshot()).ship.cargo.iron, beforeTrade.ship.cargo.iron);
  await close("market");
  await page.locator('#sidebar [data-open="market"]').click();
  await page.locator('#window-market [data-tab="shipyards"]').click();
  await settle();
  await page
    .locator('.shipyard-card[data-hull="pilgrim"]')
    .getByRole("button", { name: "Купить корабль", exact: true })
    .click();
  await page
    .locator("#window-confirm")
    .getByRole("button", { name: "Подтвердить", exact: true })
    .click();
  await settle();
  assert.equal(
    (await (await page.request.get(new URL("/api/fleet", base).href)).json())
      .ships.length,
    2,
  );
  await close("market");
  // Planner was already opened in the matrix: the new vessel must now be selectable.
  await open("tasks");
  const shipsPicker = page.locator('#window-tasks [data-field="Корабль"]');
  await shipsPicker.click();
  assert.equal(await page.locator('.picker-list [role="option"]').count(), 2);
  await page.keyboard.press("Escape");
  await page.locator("#planner-quantity").fill("1");
  await page
    .locator("#window-tasks")
    .getByRole("button", { name: "Добавить задачу", exact: true })
    .click();
  await settle();
  await page.locator(".planner-ship summary").first().click();
  await page
    .locator(".planner-item")
    .first()
    .getByRole("button", { name: "Выше в очереди", exact: true })
    .click();
  await settle();
  await page
    .locator(".planner-item")
    .first()
    .getByRole("button", { name: "Приостановить", exact: true })
    .click();
  await settle();
  await page
    .locator(".planner-item")
    .first()
    .getByRole("button", { name: "Продолжить", exact: true })
    .click();
  await settle();
  await page
    .locator(".planner-item")
    .first()
    .getByRole("button", { name: "Отменить задачу", exact: true })
    .click();
  await settle();
  await page.waitForFunction(() => !document.querySelector(".planner-item"));
  await close("tasks");
  await open("ideas");
  await page.locator("#idea-title").fill("Проверка реального терминала");
  await page
    .locator("#idea-description")
    .fill(
      "Предложение создано браузерной проверкой на отдельной тестовой базе.",
    );
  await page
    .locator("#window-ideas")
    .getByRole("button", { name: "Отправить идею", exact: true })
    .click();
  await settle();
  assert.ok(
    (await page.locator("#window-ideas").textContent()).includes(
      "Идея отправлена",
    ),
  );
  await close("ideas");
  await open("adminideas");
  assert.ok(
    (await page.locator("#window-adminideas").textContent()).includes(
      "Проверка реального терминала",
    ),
  );
  await close("adminideas");
  await open("corporations");
  const leave = page
    .locator("#window-corporations")
    .getByRole("button", { name: "Выйти из корпорации", exact: true });
  if ((await leave.count()) && (await leave.isEnabled())) {
    await leave.click();
    await page
      .locator("#window-confirm")
      .getByRole("button", { name: "Подтвердить", exact: true })
      .click();
    await settle();
  }
  await page
    .locator("#window-corporations")
    .getByRole("button", { name: "Создать корпорацию", exact: true })
    .first()
    .click();
  await page.locator("#company-full-name").fill("Audit Corporation");
  await page.locator("#company-tag").fill("AUD");
  await page.locator("#company-color").fill("#41a575");
  await page
    .locator("#window-corporations form")
    .getByRole("button", { name: "Создать корпорацию", exact: true })
    .click();
  await settle();
  await page
    .locator("#window-corporations")
    .getByRole("tab", { name: "Финансы", exact: true })
    .click();
  await settle();
  await page.locator("#company-amount").fill("100");
  await page
    .locator("#window-corporations")
    .getByRole("button", { name: "Пополнить", exact: true })
    .click();
  await settle();
  await page.locator("#company-amount").fill("20");
  await page
    .locator("#window-corporations")
    .getByRole("button", { name: "Снять", exact: true })
    .click();
  await settle();
  await page
    .locator("#window-corporations")
    .getByRole("tab", { name: "Управление", exact: true })
    .click();
  await settle();
  await page.locator("#role-name").fill("Auditor");
  await page
    .locator("#window-corporations")
    .getByRole("button", { name: "Добавить роль", exact: true })
    .click();
  await settle();
  await close("corporations");
  await open("resourcegrant");
  await page.locator("#grant-quantity").fill("1");
  await page
    .locator("#window-resourcegrant")
    .getByRole("button", { name: "Выдать ресурсы", exact: true })
    .click();
  await settle();
  await close("resourcegrant");
  await open("gameplay");
  await page
    .locator("#window-gameplay")
    .getByRole("button", { name: "Настройки колонизации", exact: true })
    .click();
  await page.waitForSelector("#window-colony-settings[open]");
  await settle();
  await close("colony-settings");
  await close("gameplay");
  await page.locator("#admin-menu").click();
  await page.locator('#window-administration [data-category="world"]').click();
  await page.locator("#window-administration").getByRole("button", {name:"Пропустить тик",exact:true}).click();
  await settle();
  assert.equal(await page.locator("#window-administration").getAttribute("open"), "");
  await page.locator("#window-administration .window-actions button").last().click();
  await open("notifications");
  await page
    .locator("#window-notifications")
    .getByRole("button", { name: "Действия с уведомлениями", exact: true })
    .click();
  await page.getByRole("menuitem", { name: "Очистить все", exact: true }).click();
  await settle();
  await close("notifications");
  console.log(
    "ACTION refuel, buy/sell, shipyard, new-ship planner, priority/pause/resume/cancel, ideas, corporation create/finance/role, resource grant, colony settings, tick, notifications",
  );
  assert.deepEqual(errors, []);
  assert.deepEqual(badResponses, []);
  assert.equal(
    await page.locator('#toast[data-level="error"]:visible').count(),
    0,
  );
  console.log("REAL TERMINAL OK");
} finally {
  await browser.close();
}
