import assert from "node:assert/strict";
import { test } from "node:test";
import { game } from "./harness.mjs";

test("all market tabs use their own view and keep exactly one tab panel", async (t) => {
  const g = await game(t, { signedIn: true });
  await g.ready();
  await g.open("market");
  await g.page.waitForSelector("#trade-quantity");
  for (const id of [
    "global",
    "orders",
    "data",
    "delivery",
    "shipyards",
    "local",
  ]) {
    await g.page.locator(`#window-market [data-tab="${id}"]`).click();
    await g.page.waitForFunction(
      (id) =>
        document
          .querySelector(`#window-market [data-tab="${id}"]`)
          ?.getAttribute("aria-selected") === "true",
      id,
    );
    assert.equal(
      await g.page.locator("#window-market .market-tab-panel").count(),
      1,
    );
    assert.equal(
      await g.page.locator("#window-market [role=tabpanel]").count(),
      1,
    );
  }
  assert.equal(await g.page.locator("#toast").textContent(), "");
});

test("reservation refresh does not duplicate the global market table", async (t) => {
  const g = await game(t, { signedIn: true });
  await g.ready();
  await g.open("market");
  await g.page.waitForSelector("#trade-quantity");
  await g.page.locator('#window-market [data-tab="global"]').click();
  await g.page.waitForFunction(
    () =>
      document
        .querySelector('#window-market [data-tab="global"]')
        .getAttribute("aria-selected") === "true",
  );
  g.handlers.set("market/global", ({ req, json }) => {
    if (req.method() === "POST") return json({ ok: true });
    return json(g.data["market/global"]);
  });
  const purchase = g.page.waitForRequest(
    (r) =>
      r.method() === "POST" &&
      new URL(r.url()).pathname === "/api/market/global",
  );
  await g.page
    .locator("#window-market")
    .getByRole("button", { name: "Забронировать на станции", exact: true })
    .click();
  await purchase;
  await g.page.waitForFunction(
    () =>
      document
        .querySelector('#window-market [data-tab="global"]')
        .getAttribute("aria-selected") === "true",
  );
  assert.equal(await g.page.locator("#window-market .table-wrap").count(), 2);
  const posted = g.requests.find(
    (r) => r.path === "market/global" && r.method === "POST",
  );
  assert.deepEqual(posted.body, {
    action: "reserve",
    star: 1,
    station: "port-1",
    resource: "iron",
    quantity: 1,
    expected_price: 5,
  });
});

test("changing tabs ignores an obsolete global market response", async (t) => {
  const g = await game(t, { signedIn: true });
  await g.ready();
  await g.open("market");
  await g.page.waitForSelector("#trade-quantity");
  let reply;
  g.handlers.set("market/global", async ({ json }) => {
    await new Promise(
      (r) =>
        (reply = () => {
          r();
          return json(g.data["market/global"]);
        }),
    );
  });
  const requested = g.page.waitForRequest(
    (r) => new URL(r.url()).pathname === "/api/market/global",
  );
  await g.page.locator('#window-market [data-tab="global"]').click();
  await requested;
  assert.equal(await g.page.locator("#window-market [role=tabpanel]").getAttribute("aria-busy"),"true");
  await g.page.locator('#window-market [data-tab="data"]').click();
  await g.page.waitForFunction(
    () =>
      document
        .querySelector('#window-market [data-tab="data"]')
        .getAttribute("aria-selected") === "true",
  );
  await reply();
  await g.page.evaluate(
    () =>
      new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r))),
  );
  assert.equal(
    await g.page
      .locator('#window-market [data-tab="data"]')
      .getAttribute("aria-selected"),
    "true",
  );
  assert.equal(await g.page.locator("#trade-quantity").count(), 0);
  assert.equal(await g.page.locator("#window-market [role=tabpanel]").getAttribute("aria-busy"),null);
  assert.ok(await g.page.locator('#window-market [data-tab="data"]').evaluate(el => el === document.activeElement));

  g.handlers.set("market/global", ({json}) => json({error:"Market unavailable"},503));
  const global = g.page.locator('#window-market [data-tab="global"]');
  await global.click();
  await g.page.locator("#toast").getByText("Market unavailable",{exact:true}).waitFor();
  assert.equal(await g.page.locator('#window-market [data-tab="data"]').getAttribute("aria-selected"),"true");
  assert.equal(await g.page.locator("#window-market [role=tabpanel]").getAttribute("aria-busy"),null);
  assert.ok(await global.evaluate(el => el === document.activeElement));
});

test("station personalization is a view projection and keeps the raw snapshot intact", async (t) => {
  const g = await game(t, { signedIn: true });
  g.data.workbench.notes = [
    {
      star: 1,
      object_id: "port-1",
      label: "My personal port",
      note: "My note",
    },
  ];
  await g.ready();
  await g.refresh();
  const values = await g.page.evaluate(async () => {
    const { application } = await import(new URL("./bootstrap.js", document.querySelector('script[type="module"][src$="bootstrap.js"]').src));
    return {
      raw: application.session.snapshot.workbench.stations[0].name,
      view: application.session.data.workbench.stations[0].name,
    };
  });
  assert.deepEqual(values, { raw: "Test Port", view: "My personal port" });
  await g.open("market");
  await g.page.waitForSelector("#trade-quantity");
  assert.match(
    await g.page.locator("#window-market .location-strip").textContent(),
    /My personal port/,
  );
});

test("corporations keep founders first and the academy before player companies", async (t) => {
  const g = await game(t, { signedIn: true });
  await g.ready();
  g.data.companies.unshift({
    id: 3,
    name: "Player Corp",
    tag: "PLC",
    flag: "◈",
    color: "#aaccee",
    members: 2,
  });
  await g.open("corporations");
  await g.page.waitForSelector(".company-list-row");
  const rows = await g.page.locator(".company-list-row").allTextContents();
  assert.match(rows[0], /Корпорация-основатель/);
  assert.match(rows[1], /Академия/);
  assert.match(rows[2], /Player Corp/);
});

test("market tabs support keyboard navigation and panel relationships", async (t) => {
  const g = await game(t, { signedIn: true });
  await g.ready();
  await g.open("market");
  await g.page.waitForSelector("#trade-quantity");
  await g.page.locator('#window-market [data-tab="local"]').focus();
  await g.page.keyboard.press("ArrowRight");
  await g.page.waitForFunction(
    () =>
      document
        .querySelector('#window-market [data-tab="global"]')
        .getAttribute("aria-selected") === "true",
  );
  const id = await g.page
    .locator('#window-market [data-tab="global"]')
    .getAttribute("aria-controls");
  assert.equal(await g.page.locator(`#${id}`).getAttribute("role"), "tabpanel");
  assert.equal(
    await g.page.locator('#window-market [role="tab"][tabindex="0"]').count(),
    1,
  );
  assert.ok(await g.page.locator('#window-market [data-tab="global"]').evaluate(el => el === document.activeElement));
});
