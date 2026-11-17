import assert from "node:assert/strict";
import { test } from "node:test";
import { game } from "./harness.mjs";

test("cancel admin gameplay changes restores server values without sending a command", async (t) => {
  const g = await game(t, { signedIn: true, role: "admin" });
  g.data["admin/gameplay"] = g.data.gameplay;
  await g.ready();
  await g.open("gameplay");
  await g.page.locator("#setting-tick_seconds").fill("99");
  await g.page
    .locator("#window-gameplay .save-bar")
    .getByRole("button", { name: "Отменить", exact: true })
    .click();
  await g.page.waitForFunction(
    () => document.getElementById("setting-tick_seconds").value === "1000",
  );
  assert.equal(g.requests.filter((r) => r.method === "POST").length, 0);
});

test("cancel user edits discards the local draft", async (t) => {
  const g = await game(t, { signedIn: true, role: "admin" });
  await g.ready();
  await g.open("users");
  await g.page
    .locator("#window-users")
    .getByRole("button", { name: "Редактировать", exact: true })
    .click();
  await g.page.locator("#edit-name-71").fill("discard_this");
  await g.page
    .locator("#window-user-71 .save-bar")
    .getByRole("button", { name: "Отменить", exact: true })
    .click();
  assert.equal(
    await g.page.locator("#edit-name-71").inputValue(),
    "test_other",
  );
  assert.equal(g.requests.filter((r) => r.method === "POST").length, 0);
});

test("delivery composer uses a station from its actual station list", async (t) => {
  const g = await game(t, { signedIn: true });
  g.handlers.set("workbench", ({ req, json }) =>
    req.method() === "GET" ? json(g.data.workbench) : json({ ok: true }),
  );
  await g.ready();
  await g.open("market");
  await g.page
    .locator("#window-market")
    .getByRole("tab", { name: "Перевозки", exact: true })
    .click();
  await g.page
    .locator("#window-market summary")
    .filter({ hasText: "Создать доставку" })
    .click();
  await g.page
    .locator("#window-market")
    .getByRole("button", { name: "Создать заказ", exact: true })
    .click();
  await g.page.waitForFunction(() =>
    document
      .querySelector("#toast")
      ?.textContent.includes("Действие выполнено"),
  );
  const command = g.requests.find(
    (r) => r.method === "POST" && r.path === "workbench",
  );
  assert.equal(command.body.to, 1);
  assert.equal(command.body.station, "port-1");
});

test("planner refresh includes purchased and renamed ships without issuing commands", async (t) => {
  const g = await game(t, { signedIn: true });
  await g.ready();
  await g.open("tasks");
  await g.page.waitForSelector("#planner-quantity");
  g.data.fleet.ships.push({
    ...g.data.fleet.ships[0],
    id: 99,
    name: "New Audit Ship",
  });
  await g.refresh();
  const picker = g.page.locator('#window-tasks [data-field="Корабль"]');
  await picker.click();
  await g.page
    .getByRole("option", { name: "New Audit Ship", exact: true })
    .waitFor();
  assert.equal(await g.page.locator('.picker-list [role="option"]').count(), 3);
  assert.equal(g.requests.filter((r) => r.method === "POST").length, 0);
});

test("updating picker options preserves its value without calling the action callback", async (t) => {
  const g = await game(t);
  await g.ready();
  await g.page.evaluate(() => {
    const picker = window.__testApp.UI.picker(
      [
        [1, "one"],
        [2, "two"],
      ],
      2,
      () => {
        throw Error("selection must not execute during refresh");
      },
    );
    picker.id = "audit-options";
    Object.assign(picker.style, {
      position: "fixed",
      top: "100px",
      left: "300px",
      zIndex: "999",
    });
    document.body.append(picker);
  });
  await g.page.locator("#audit-options").click();
  await g.page.evaluate(() =>
    document.getElementById("audit-options").setOptions([
      [2, "renamed"],
      [3, "three"],
    ]),
  );
  assert.equal(await g.page.locator(".picker-popover").count(), 0);
  assert.equal(
    await g.page.locator("#audit-options").evaluate((e) => e.value),
    "2",
  );
  assert.match(await g.page.locator("#audit-options").textContent(), /renamed/);
});

test("colony settings close on role loss and a late read cannot reopen them", async (t) => {
  const g = await game(t, { signedIn: true, role: "admin" });
  g.data["admin/gameplay"] = g.data.gameplay;
  g.data["admin/planet/settings"] = {
    min_level: 6,
    personal_limit: 5,
    min_land: 50,
    funding_days: 7,
    build_ticks: 10,
    refund_percent: 80,
    counts: [100, 200, 300, 400],
    cost: { credits: 1000 },
  };
  await g.ready();
  await g.open("gameplay");
  const settings = g.page
    .locator("#window-gameplay")
    .getByRole("button", { name: "Настройки колонизации", exact: true });
  await settings.click();
  await g.page.waitForSelector("#window-colony-settings[open]");
  g.data.state.user.role = "player";
  await g.refresh();
  assert.equal(await g.page.locator("#window-colony-settings").count(), 0);
  g.data.state.user.role = "admin";
  await g.refresh();
  await g.open("gameplay");
  let reply;
  const blocked = new Promise((resolve) =>
    g.handlers.set("admin/planet/settings", async ({ json }) => {
      await new Promise((r) => {
        reply = () => {
          r();
          return json(g.data["admin/planet/settings"]);
        };
        resolve();
      });
    }),
  );
  // The button action awaits the blocked read, so start it in the browser without awaiting it.
  await settings.evaluate((e) => e.click());
  await blocked;
  g.data.state.user.role = "player";
  await g.refresh();
  await reply();
  await g.page.evaluate(
    () =>
      new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r))),
  );
  assert.equal(await g.page.locator("#window-colony-settings").count(), 0);
});

test("object quota countdown does not insert a fictitious zero stock", async (t) => {
  const g = await game(t, { signedIn: true });
  await g.ready();
  await g.page.evaluate(async () => {
    const { showObject } = await import("/features/objects/cards.js");
    const { world, loadWorld } = await import("/core/world-store.js");
    await loadWorld();
    showObject(
      {
        id: "belt-audit",
        name: "Audit Belt",
        kind: "Пояс астероидов",
        resources: [],
        shared_quota: {
          remaining: 123,
          capacity: 5000,
          reset_at: new Date(Date.now() + 3600000).toISOString(),
        },
      },
      world.data.stars[0],
    );
  });
  await g.page.waitForFunction(
    () =>
      document.querySelector("#window-object-1-belt-audit [data-reset-at]")
        ?.textContent.length > 0,
  );
  assert.match(
    await g.page
      .locator("#window-object-1-belt-audit [data-reset-at]")
      .textContent(),
    /^\d{2}:\d{2}:\d{2}$/,
  );
  assert.match(
    await g.page.locator("#window-object-1-belt-audit").textContent(),
    /123 \/ 5000/,
  );
});

test("logistics errors arriving after logout remain silent", async (t) => {
  const g = await game(t, { signedIn: true });
  await g.ready();
  let reply;
  const blocked = new Promise((resolve) =>
    g.handlers.set("logistics", async ({ json }) => {
      await new Promise((r) => {
        reply = () => {
          r();
          return json({ error: "OLD_LOGISTICS_FAILURE" }, 500);
        };
        resolve();
      });
    }),
  );
  await g.open("logistics");
  await blocked;
  await g.page.evaluate(() => window.__testApp.showAuth());
  await reply();
  await g.page.evaluate(
    () =>
      new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r))),
  );
  assert.equal(await g.page.locator("#toast").textContent(), "");
});

test("notification opens its system on the map before any prior map visit", async (t) => {
  const g = await game(t, { signedIn: true });
  g.data.notifications = {
    count: 1,
    items: [
      {
        id: 10,
        kind: "arrival",
        level: "success",
        created_at: new Date().toISOString(),
        data: { star: 2 },
      },
    ],
  };
  await g.ready();
  await g.open("notifications");
  await g.page.locator(".notification-compact-open").click();
  await g.page.getByRole("button", { name: "На карте", exact: true }).click();
  await g.page.waitForSelector("#window-galaxy[open]");
  await g.page.waitForFunction(
    () => document.querySelector("#map-system-id")?.value === "2",
  );
  assert.match(
    await g.page.locator("#map-system-id").textContent(),
    /Test Vega/,
  );
});

test("galactic navigation restores galaxy mode after visiting and closing the system map", async (t) => {
  const g = await game(t, { signedIn: true });
  await g.ready();
  await g.page.locator("#maps-navigation > summary").click();
  await g.page.locator("#current-system-map").click();
  await g.page.waitForFunction(() =>
    document
      .querySelector("#map-breadcrumb")
      ?.textContent.includes("Test Region"),
  );
  assert.equal(
    await g.page
      .locator("#current-system-map")
      .evaluate((e) => e.classList.contains("module-open")),
    true,
  );
  assert.equal(
    await g.page
      .locator('#sidebar [data-open="galaxy"]')
      .evaluate((e) => e.classList.contains("module-open")),
    false,
  );
  await g.page.locator('#sidebar [data-open="galaxy"]').click();
  await g.page.waitForSelector("#route-to");
  assert.equal(
    await g.page
      .locator('#sidebar [data-open="galaxy"]')
      .evaluate((e) => e.classList.contains("module-open")),
    true,
  );
  assert.equal(
    await g.page
      .locator("#current-system-map")
      .evaluate((e) => e.classList.contains("module-open")),
    false,
  );
  assert.ok(
    !(await g.page.locator("#map-breadcrumb").textContent()).includes(
      "Test Region",
    ),
  );
  await g.page.locator("#current-system-map").click();
  await g.page.waitForFunction(() =>
    document
      .querySelector("#map-breadcrumb")
      ?.textContent.includes("Test Region"),
  );
  await g.page.evaluate(() => window.__testApp.Modules.close("galaxy"));
  assert.equal(await g.page.locator("#sidebar .module-open").count(), 0);
  await g.page.locator('#sidebar [data-open="galaxy"]').click();
  await g.page.waitForSelector("#route-to");
  assert.equal(
    await g.page
      .locator('#sidebar [data-open="galaxy"]')
      .evaluate((e) => e.classList.contains("module-open")),
    true,
  );
  assert.equal(
    await g.page
      .locator("#current-system-map")
      .evaluate((e) => e.classList.contains("module-open")),
    false,
  );
  assert.ok(
    !(await g.page.locator("#map-breadcrumb").textContent()).includes(
      "Test Region",
    ),
  );
});

test("late system response cannot override explicit galactic navigation", async (t) => {
  const g = await game(t, { signedIn: true });
  await g.ready();
  await g.open("galaxy");
  await g.page.waitForSelector("#route-to");
  let reply;
  const blocked = new Promise((resolve) =>
    g.handlers.set("system", async ({ json }) => {
      await new Promise((r) => {
        reply = () => {
          r();
          return json(g.data.system);
        };
        resolve();
      });
    }),
  );
  await g.page.locator("#maps-navigation > summary").click();
  await g.page.locator("#current-system-map").click();
  await blocked;
  await g.page.locator('#sidebar [data-open="galaxy"]').click();
  await reply();
  await g.page.evaluate(
    () =>
      new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r))),
  );
  await g.page.waitForSelector("#route-to");
  assert.ok(
    !(await g.page.locator("#map-breadcrumb").textContent()).includes(
      "Test Region",
    ),
  );
});
