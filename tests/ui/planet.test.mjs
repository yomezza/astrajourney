import assert from "node:assert/strict";
import { test } from "node:test";
import { game } from "./harness.mjs";
import { planetFixture, planetState } from "./planet-fixture.mjs";

test("planet scenes load public art lazily, reuse six textures and keep geography intact", async (t) => {
  const g = await game(t, { signedIn: true });
  g.data["planet/surface"] = planetFixture();
  g.data["planet/state"] = planetState();
  await g.ready();
  assert.equal(
    g.assets.some(
      (p) => p.endsWith("/planet/index.js") || p.includes("surface-ground-"),
    ),
    false,
  );
  const before = JSON.stringify(g.data["planet/surface"].geography);
  for (const kind of [
    "Садовый мир",
    "Ледяная",
    "Пустынная",
    "Вулканическая",
    "Каменистая",
  ]) {
    g.data["planet/surface"].kind = kind;
    await g.page.evaluate(async () => {
      const { openPlanet } = await import("/features/planet/loader.js");
      await openPlanet(1, 1);
    });
    await g.page.waitForSelector('#planet-map[data-textures="ready"]');
  }
  const png = g.assets.filter((p) => p.includes("surface-ground-"));
  assert.equal(png.length, 6);
  assert.equal(new Set(png).size, 6);
  assert.equal(JSON.stringify(g.data["planet/surface"].geography), before);
  assert.equal(
    g.requests.some((r) => r.method === "POST"),
    false,
  );
  await g.page.evaluate(() => window.__testApp.I18n.set("en"));
  await g.page.waitForSelector('#planet-map[data-textures="ready"]');
  assert.equal(g.assets.filter((p) => p.includes("surface-ground-")).length, 6);
});

test("texture failure falls back and retries without replacing geography", async (t) => {
  const g = await game(t, { signedIn: true });
  g.data["planet/surface"] = planetFixture();
  g.data["planet/state"] = planetState();
  await g.ready();
  await g.context.route("**/surface-ground-*.png", (r) => r.abort());
  await g.page.evaluate(async () => {
    const { openPlanet } = await import("/features/planet/loader.js");
    await openPlanet(1, 1);
  });
  await g.page.waitForSelector('#planet-map[data-textures="partial"]');
  await g.context.unroute("**/surface-ground-*.png");
  await g.page
    .getByRole("button", { name: "Повторить загрузку текстур", exact: true })
    .click();
  await g.page.waitForSelector('#planet-map[data-textures="ready"]');
  await g.page.evaluate(() => window.__testApp.showAuth());
  await g.page.waitForFunction(() => !document.querySelector(".planet-ui"));
  assert.equal(await g.page.locator("#planet-map").count(), 0);
});

test("late images cannot resurrect a scene after logout", async (t) => {
  const g = await game(t, { signedIn: true });
  g.data["planet/surface"] = planetFixture();
  g.data["planet/state"] = planetState();
  await g.ready();
  let release, received;
  const gate = new Promise((r) => (release = r)),
    allRequested = new Promise((r) => (received = r)),
    requests = [];
  await g.context.route("**/surface-ground-*.png", async (route) => {
    requests.push(route.request());
    if (requests.length === 5) received();
    await gate;
    await route.continue();
  });
  try {
    await g.page.evaluate(async () => {
      const { openPlanet } = await import("/features/planet/loader.js");
      await openPlanet(1, 1);
    });
    await allRequested;
    await g.page.evaluate(() => window.__testApp.showAuth());
    await g.page.waitForFunction(() => !document.querySelector(".planet-ui"));
    release();
    await Promise.all(
      requests.map(async (req) => (await req.response())?.finished()),
    );
    await g.page.evaluate(
      () =>
        new Promise((r) =>
          requestAnimationFrame(() => requestAnimationFrame(r)),
        ),
    );
    assert.equal(await g.page.locator(".planet-ui,#planet-map").count(), 0);
  } finally {
    release();
  }
});

test("surface cards locate shared landing infrastructure and reflect current access", async (t) => {
  const g = await game(t, { signedIn: true });
  g.data["planet/surface"] = planetFixture();
  g.data.notifications = {
    count: 1,
    items: [
      {
        id: 77,
        kind: "landing_ready",
        level: "success",
        message: "Посадочная площадка готова",
        created_at: new Date().toISOString(),
        data: { planet: "Test surface", sector: 2, star: 1, planet_index: 1 },
      },
    ],
  };
  g.data["planet/state"] = {
    ...planetState(),
    claims: [1, 2].map((sector) => ({
      id: sector,
      sector,
      status: "complete",
      owner: "Test pilot",
      owner_user: 51,
    })),
    landing: {
      preparing: false,
      sites: [{ id: "27", sector: 2, owner: "Test pilot", accessible: true }],
      territories: [{ sectors: [1, 2], sites: ["27"] }],
    },
  };
  await g.ready();
  assert.equal(
    g.assets.some((p) => p.endsWith("planet-landing-core.png")),
    false,
  );
  await g.page.evaluate(async () => {
    const { openPlanet } = await import("/features/planet/loader.js");
    await openPlanet(1, 1);
  });
  await g.page.waitForSelector('#planet-map[data-textures="ready"]');
  await g.page.getByRole("button", { name: "Сектор 2", exact: true }).click();
  await g.page.locator(".landing-site-preview").waitFor();
  await g.page
    .getByText("Посадочная площадка подготовлена", { exact: true })
    .waitFor();
  g.data["planet/state"].landing.sites[0].accessible = false;
  g.data["planet/state"].landing.sites[0].reason =
    "Нет права посещения этой территории";
  await g.refresh();
  await g.page
    .getByText("Нет права посещения этой территории", { exact: true })
    .waitFor();
  assert.equal(
    g.requests.some((r) => r.method === "POST"),
    false,
  );
  await g.page.evaluate(() => window.__testApp.I18n.set("en"));
  const target = g.page.getByRole("button", { name: "Sector 2", exact: true });
  await target.waitFor();
  await target.click();
  await g.page
    .getByText("You do not have permission to visit this territory", {
      exact: true,
    })
    .waitFor();
  await g.page.evaluate(() => window.__testApp.Modules.close("galaxy"));
  await g.open("notifications");
  await g.page.locator(".notification-compact-row").first().waitFor();
  assert.match(
    await g.page.locator(".notification-compact-row").first().textContent(),
    /Landing site ready.*Test surface.*Sector 2/,
  );
});
