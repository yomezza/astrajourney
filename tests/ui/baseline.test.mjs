import assert from "node:assert/strict";
import { test } from "node:test";
import { game } from "./harness.mjs";

test("startup keeps auth hidden until the session check finishes", async (t) => {
  let answer;
  const pending = new Promise((resolve) => {
    answer = resolve;
  });
  const g = await game(t, {
    signedIn: true,
    faults: [
      [
        "state",
        async ({ json }) => {
          await pending;
          return json({ error: "Войдите в аккаунт" }, 401);
        },
      ],
    ],
  });
  assert.equal(await g.page.locator("#startup-screen").isVisible(), true);
  assert.equal(await g.page.locator("#auth").isVisible(), false);
  answer();
  await g.ready();
  assert.equal(await g.page.locator("#auth").isVisible(), true);
});

test("offline startup offers retry; a one-character password remains accepted", async (t) => {
  const g = await game(t, {
    faults: [["state", ({ json }) => json({ error: "Temporary outage" }, 503)]],
  });
  await g.page.waitForSelector(".startup-failed");
  assert.equal(await g.page.locator("#auth").isVisible(), false);
  g.handlers.delete("state");
  await g.page.locator("#startup-retry").click();
  await g.ready();
  await g.login();
  assert.equal(g.requests.find((r) => r.path === "login").body.password, "a");
  assert.equal(
    await g.page.locator("#welcome-name").textContent(),
    "test_pilot",
  );
  assert.equal(await g.page.locator("#balance-change").textContent(), "");
});

test("Maps is initially closed and only the player changes its saved state", async (t) => {
  const g = await game(t, { signedIn: true });
  await g.ready();
  assert.equal(
    await g.page.locator("#maps-navigation").evaluate((e) => e.open),
    false,
  );
  await g.page.locator("#maps-navigation > summary").click();
  await g.page.waitForFunction(
    () => localStorage.getItem("maps-navigation-open") === "true",
  );
  await g.refresh();
  await g.page.evaluate(() => window.__testApp.I18n.set("en"));
  assert.equal(
    await g.page.locator("#maps-navigation").evaluate((e) => e.open),
    true,
  );
  await g.page.reload();
  await g.ready();
  assert.equal(
    await g.page.locator("#maps-navigation").evaluate((e) => e.open),
    true,
  );
  await g.page.locator("#maps-navigation > summary").click();
  await g.page.reload();
  await g.ready();
  assert.equal(
    await g.page.locator("#maps-navigation").evaluate((e) => e.open),
    false,
  );
});

test("workspace drag, keyboard move, pin, resize, Escape and geometry restoration", async (t) => {
  const g = await game(t);
  await g.ready();
  await g.open("settings");
  const dialog = g.page.locator("#window-settings"),
    bar = dialog.locator(".module-titlebar");
  const before = await dialog.boundingBox(),
    handle = await bar.boundingBox();
  await g.page.mouse.move(handle.x + 90, handle.y + 12);
  await g.page.mouse.down();
  await g.page.mouse.move(handle.x + 150, handle.y + 42, { steps: 5 });
  await g.page.mouse.up();
  const moved = await dialog.boundingBox();
  assert.ok(moved.x > before.x + 40 && moved.y > before.y + 15);
  await bar.focus();
  await g.page.keyboard.press("ArrowRight");
  const keyboardMoved = await dialog.boundingBox();
  assert.ok(keyboardMoved.x > moved.x);
  await dialog.locator(".window-actions button").nth(0).click();
  const pinned = await dialog.boundingBox();
  await bar.focus();
  await g.page.keyboard.press("ArrowRight");
  assert.equal((await dialog.boundingBox()).x, pinned.x);
  assert.equal(
    await dialog
      .locator(".window-actions button")
      .nth(0)
      .getAttribute("aria-pressed"),
    "true",
  );
  await g.page.keyboard.press("Escape");
  assert.equal(await dialog.evaluate((e) => e.open), false);
  await g.page.reload();
  await g.ready();
  await g.open("settings");
  assert.equal(
    await dialog
      .locator(".window-actions button")
      .nth(0)
      .getAttribute("aria-pressed"),
    "true",
  );
  assert.ok(Math.abs((await dialog.boundingBox()).x - pinned.x) < 2);
  await dialog.locator(".window-actions button").nth(0).click();
  const size = await dialog.boundingBox();
  await g.page.mouse.move(size.x + size.width - 3, size.y + size.height - 3);
  await g.page.mouse.down();
  await g.page.mouse.move(size.x + size.width + 50, size.y + size.height + 30, {
    steps: 5,
  });
  await g.page.mouse.up();
  assert.ok((await dialog.boundingBox()).width > size.width + 30);
});

test("planner counts active jobs per ship, keeps completed jobs in history and persists branches", async (t) => {
  const g = await game(t, { signedIn: true });
  await g.ready();
  await g.open("tasks");
  await g.page.waitForSelector(".planner-ship-count");
  const groups = g.page.locator(".planner-ship");
  assert.equal(await groups.count(), 2);
  assert.deepEqual(await groups.evaluateAll((es) => es.map((e) => e.open)), [
    false,
    false,
  ]);
  assert.deepEqual(
    await g.page.locator(".planner-ship-count").allTextContents(),
    ["Задания: 2", "Задания: 1"],
  );
  assert.equal(
    await g.page.locator('.planner-list [data-task="4"]').count(),
    0,
  );
  await groups.nth(0).locator(":scope > summary").click();
  await g.page.waitForFunction(
    () => localStorage.getItem("fold:51:planner:ship:v2:101") === "true",
  );
  assert.deepEqual(
    await groups
      .nth(0)
      .locator(".planner-item")
      .evaluateAll((es) => es.map((e) => e.open)),
    [true, true],
  );
  await g.refresh();
  await g.page.reload();
  await g.ready();
  await g.open("tasks");
  await g.page.waitForSelector(".planner-ship-count");
  assert.deepEqual(await groups.evaluateAll((es) => es.map((e) => e.open)), [
    true,
    false,
  ]);
  // A different account must not inherit the first account's ship branch.
  g.data.state.user.id = 52;
  await g.page.reload();
  await g.ready();
  await g.open("tasks");
  await g.page.waitForSelector(".planner-ship-count");
  assert.deepEqual(await groups.evaluateAll((es) => es.map((e) => e.open)), [
    false,
    false,
  ]);
});

test("map filters default other ships off in galaxy and on in system; modes and accounts are independent", async (t) => {
  const g = await game(t, { signedIn: true });
  await g.ready();
  await g.open("galaxy");
  await g.page.waitForFunction(() => document.getElementById("route-to"));
  const others = g.page.locator('label[data-filter="others"] input');
  await g.page.locator(".map-display-menu > summary").click();
  assert.equal(await others.isChecked(), false);
  await others.check();
  await g.page.waitForFunction(
    () =>
      JSON.parse(localStorage.getItem("map-filters:51:galaxy")).others === true,
  );
  await g.page.evaluate(() =>
    window.dispatchEvent(new Event("showcurrentsystem")),
  );
  await g.page.waitForFunction(() =>
    document
      .querySelector("#map-breadcrumb")
      .textContent.includes("Test Region"),
  );
  assert.equal(await others.isChecked(), true);
  await others.uncheck();
  assert.equal(
    await g.page.evaluate(
      () => JSON.parse(localStorage.getItem("map-filters:51:galaxy")).others,
    ),
    true,
  );
  await g.page.reload();
  await g.ready();
  await g.open("galaxy");
  await g.page.waitForFunction(() => document.getElementById("route-to"));
  assert.equal(await others.isChecked(), true);
  await g.page.evaluate(() =>
    window.dispatchEvent(new Event("showcurrentsystem")),
  );
  await g.page.waitForFunction(() =>
    document
      .querySelector("#map-breadcrumb")
      .textContent.includes("Test Region"),
  );
  assert.equal(await others.isChecked(), false);
  g.data.state.user.id = 52;
  await g.page.reload();
  await g.ready();
  await g.open("galaxy");
  await g.page.waitForFunction(() => document.getElementById("route-to"));
  assert.equal(await others.isChecked(), false);
});

test("ship uses current mining stats, inline hologram and a separate cargo window", async (t) => {
  const g = await game(t, { signedIn: true });
  await g.ready();
  await g.open("ship");
  await g.page.waitForSelector(".ship-mining");
  assert.equal(await g.page.locator(".ship-mining").textContent(), "5");
  await g.page.waitForSelector(".ship-projection .ship-inline-model svg");
  assert.equal(
    await g.page.locator(".ship-projection #ship-rename").count(),
    1,
  );
  const model = await g.page.locator(".ship-inline-model").elementHandle();
  await g.refresh();
  assert.equal(
    await model.evaluate(
      (e) => e === document.querySelector(".ship-inline-model"),
    ),
    true,
  );
  await g.page
    .locator(".ship-controls")
    .getByRole("button", { name: "Трюм", exact: true })
    .click();
  await g.page.waitForFunction(
    () => document.getElementById("window-cargo").open,
  );
  assert.equal(
    await g.page
      .locator("#window-cargo")
      .evaluate((e) => e.parentElement === document.body),
    true,
  );
  const ship = await g.page.locator("#window-ship").boundingBox(),
    cargo = await g.page.locator("#window-cargo").boundingBox();
  assert.ok(cargo.x >= ship.x + ship.width - 2);
});

test("guide searches the current resource catalogue and keeps the article reader", async (t) => {
  const g = await game(t, { signedIn: true });
  await g.ready(); await g.open("guide");
  await g.page.locator('.result-row[data-article="game:resource:iron"]').waitFor();
  await g.page.getByRole("searchbox", {name:"Поиск по базе знаний"}).fill("Железо");
  assert.equal(await g.page.locator(".result-row").count(),1);
  await g.page.locator('.result-row[data-article="game:resource:iron"]').click();
  assert.match(await g.page.locator(".c-reading").textContent(),/Железо/);
});

test("picker searches, paginates, selects with keyboard and closes on Escape/outside", async (t) => {
  const g = await game(t);
  await g.ready();
  await g.page.evaluate(() => {
    const options = Array.from({ length: 24 }, (_, i) => [
      i + 1,
      `System ${String(i + 1).padStart(2, "0")}`,
    ]);
    const picker = window.__testApp.UI.picker(options, 1, (value) => {
      window.testPick = value;
    });
    picker.id = "test-picker";
    picker.style.cssText =
      "position:fixed;left:20px;top:100px;width:220px;z-index:1000";
    document.body.append(picker);
  });
  await g.page.locator("#test-picker").click();
  assert.equal(await g.page.getByRole("option").count(), 10);
  await g.page
    .locator(".picker-popover")
    .getByRole("button", { name: "›", exact: true })
    .click();
  assert.match(
    await g.page.getByRole("option").first().textContent(),
    /System 11/,
  );
  await g.page.locator(".picker-popover input").fill("System 24");
  assert.equal(await g.page.getByRole("option").count(), 1);
  await g.page.locator(".picker-popover input").press("ArrowDown");
  await g.page.keyboard.press("Enter");
  assert.equal(await g.page.evaluate(() => window.testPick), "24");
  assert.equal(await g.page.locator(".picker-popover").count(), 0);
  await g.page.locator("#test-picker").click();
  await g.page.keyboard.press("Escape");
  assert.equal(await g.page.locator(".picker-popover").count(), 0);
  await g.page.locator("#test-picker").click();
  await g.page.evaluate(() => new Promise(requestAnimationFrame));
  await g.page.mouse.click(1300, 900);
  assert.equal(await g.page.locator(".picker-popover").count(), 0);
});

test("context menu clamps to viewport and supports nested keyboard navigation", async (t) => {
  const g = await game(t);
  await g.ready();
  await g.page.evaluate(() =>
    window.__testApp.UI.menu(
      [
        {
          label: "Parent",
          children: [
            {
              label: "Child",
              action: () => {
                window.testMenu = true;
              },
            },
          ],
        },
        { label: "Disabled", disabled: true },
      ],
      innerWidth - 1,
      innerHeight - 1,
    ),
  );
  const menu = g.page.locator(".context-menu").first(),
    box = await menu.boundingBox();
  assert.ok(
    box.x >= 0 &&
      box.y >= 0 &&
      box.x + box.width <= 1440 &&
      box.y + box.height <= 1000,
  );
  await menu.getByRole("menuitem", { name: "Parent" }).focus();
  await g.page.keyboard.press("ArrowRight");
  await g.page.getByRole("menuitem", { name: "Child" }).press("Enter");
  assert.equal(await g.page.evaluate(() => window.testMenu), true);
  assert.equal(await menu.isVisible(), false);
});

test("RU/EN number presentation preserves unknown values and source precision", async (t) => {
  const g = await game(t);
  await g.ready();
  const values = await g.page.evaluate(() => {
    const raw = 0.0078;
    window.__testApp.I18n.set("ru");
    const ru = window.__testApp.UI.percent(50.2),
      unknown = window.__testApp.UI.formatNumber(null);
    const science = window.__testApp.UI.formatNumber(raw, {
      maximumFractionDigits: 4,
    });
    window.__testApp.I18n.set("en");
    return {
      ru,
      en: window.__testApp.UI.percent(50.2),
      unknown,
      science,
      raw,
      lang: document.documentElement.lang,
    };
  });
  assert.deepEqual(values, {
    ru: "50,2%",
    en: "50.2%",
    unknown: "—",
    science: "0,0078",
    raw: 0.0078,
    lang: "en",
  });
});

test("technical toast never creates a saved notification", async (t) => {
  const g = await game(t, { signedIn: true });
  await g.ready();
  await g.page.evaluate(() => window.__testApp.notify("Действие выполнено"));
  assert.match(
    await g.page.locator("#toast").textContent(),
    /Действие выполнено/,
  );
  assert.equal(
    g.requests.filter((r) => r.path === "notifications" && r.method === "POST")
      .length,
    0,
  );
});

test("player cannot open admin modules; logout clears private UI", async (t) => {
  const g = await game(t, { signedIn: true });
  await g.ready();
  assert.equal(await g.page.locator("#admin-menu").isVisible(), false);
  await g.open("ship");
  await g.page.waitForSelector(".ship-mining");
  await g.page.evaluate(() => window.__testApp.Modules.open("users"));
  await g.page.waitForFunction(() => !window.__testApp.Modules.isOpen("users"));
  assert.equal(g.requests.filter((r) => r.path === "admin/users").length, 0);
  await g.page.evaluate(() => document.getElementById("logout").click());
  await g.page.waitForSelector("#auth:not([hidden])");
  assert.equal(
    await g.page.locator("#window-ship .module-content").textContent(),
    "",
  );
  assert.equal(await g.page.locator(".module-window[open]").count(), 0);
});

test("space terminal and system map do not eagerly load planetary code or textures", async (t) => {
  const g = await game(t, { signedIn: true });
  await g.ready();
  await g.open("galaxy");
  await g.page.waitForFunction(() => document.getElementById("route-to"));
  await g.page.evaluate(() =>
    window.dispatchEvent(new Event("showcurrentsystem")),
  );
  await g.page.waitForFunction(() =>
    document
      .querySelector("#map-breadcrumb")
      .textContent.includes("Test Region"),
  );
  assert.equal(
    g.assets.some(
      (path) =>
        /\/features\/planet\/(index|terrain)\.js$/.test(path) ||
        path.startsWith("/planet-pixelart-v1/terrain/"),
    ),
    false,
  );
});

test("visual contracts preserve thin actions and a square system ID", async (t) => {
  const g = await game(t, { signedIn: true });
  await g.ready();
  await g.open("galaxy");
  await g.page.waitForFunction(() =>
    document.querySelector("#route-to .system-id"),
  );
  const shape = await g.page.locator("#route-to .system-id").evaluate((e) => {
    const s = getComputedStyle(e);
    return {
      radius: s.borderTopLeftRadius,
      background: s.backgroundImage,
      width: e.offsetWidth,
      height: e.offsetHeight,
    };
  });
  assert.deepEqual(shape, {
    radius: "0px",
    background: "none",
    width: 26,
    height: 26,
  });
  await g.open("ship");
  await g.page.waitForSelector(".ship-mining");
  const action = await g.page
    .locator(".ship-controls .terminal-action")
    .first()
    .evaluate((e) => {
      const s = getComputedStyle(e);
      return { radius: s.borderTopLeftRadius, height: e.offsetHeight };
    });
  assert.equal(action.radius, "1px");
  assert.ok(action.height >= 26 && action.height <= 30);
});

test("newly opened windows fit a narrow viewport", async (t) => {
  const g = await game(t);
  await g.ready();
  await g.page.setViewportSize({ width: 750, height: 900 });
  await g.open("settings");
  const box = await g.page.locator("#window-settings").boundingBox();
  assert.ok(box.x >= 0 && box.x + box.width <= 750);
  assert.ok(box.y >= 58 && box.y + box.height <= 900);
});
