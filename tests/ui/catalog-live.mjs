// Production pages + real Go handlers/PostgreSQL, no intercepted API responses.
import assert from "node:assert/strict";
import { readFileSync, mkdirSync } from "node:fs";
import { chromium } from "playwright";
const fixture = JSON.parse(readFileSync(0, "utf8"));
const base = new URL(process.env.TERMINAL_TEST_URL);
assert.ok(["127.0.0.1", "localhost", "[::1]"].includes(base.hostname));
assert.equal(
  (await fetch(base)).headers.get("x-cosmos-test-run"),
  process.env.TERMINAL_TEST_NONCE,
);
assert.ok(process.env.TERMINAL_TEST_NONCE?.startsWith("terminal-test-"));
const browser = await chromium.launch({
  executablePath: process.env.CHROMIUM_EXECUTABLE_PATH || undefined,
  args: process.env.CHROMIUM_NO_SANDBOX === "1" ? ["--no-sandbox"] : [],
});
const context = await browser.newContext({
  viewport: { width: 1440, height: 1000 },
});
await context.addCookies([
  { name: fixture.cookie_name, value: fixture.cookie, url: base.href },
]);
const page = await context.newPage();
page.setDefaultTimeout(12000);
const errors = [],
  broken = [];
page.on("pageerror", (e) => errors.push(e.message));
page.on("response", (r) => {
  if (r.status() >= 400 && !r.url().endsWith("/favicon.ico"))
    broken.push(`${r.status()} ${new URL(r.url()).pathname}`);
});
const open = (id) =>
  page.evaluate(
    async (id) => (await import("/bootstrap.js")).application.Modules.open(id),
    id,
  );
const close = (id) =>
  page.evaluate(
    async (id) => (await import("/bootstrap.js")).application.Modules.close(id),
    id,
  );
const refresh = () =>
  page.evaluate(async () =>
    (await import("/core/snapshot-store.js")).refresh(),
  );
const api = async (path, body) => {
  const r = body
    ? await page.request.post(new URL("/api/" + path, base).href, {
        data: body,
      })
    : await page.request.get(new URL("/api/" + path, base).href);
  assert.equal(r.status(), 200, await r.text());
  return r.json();
};
const click = async (root, name) =>
  root.getByRole("button", { name, exact: true }).click();
const studies = page.locator("#window-studies");
const colonies = page.locator("#window-colonies");
try {
  await page.goto(base.href);
  await page.locator("#game:not([hidden])").waitFor();
  await open("studies");
  await studies.locator('.study-entry[data-star="2"]').waitFor();
  assert.equal(
    await studies
      .locator(".studies-current-pin .study-entry")
      .getAttribute("data-star"),
    "1",
  );
  await studies.locator('.study-entry[data-star="2"]').click();
  await studies
    .locator(".studies-current h2")
    .filter({ hasText: "Catalog 02" })
    .waitFor();
  assert.equal(
    await studies
      .locator(".study-current-cover [data-model]")
      .getAttribute("data-model"),
    "object-star-b.svg",
  );
  assert.equal(
    (await api("expedition")).ship.star,
    1,
    "selection must not move a ship",
  );
  const search = studies.locator("#studies-search");
  await search.fill("Catalog");
  await studies.locator(".studies-source-picker").click();
  await page
    .getByRole("menuitemradio", { name: "Полученные сведения", exact: true })
    .click();
  assert.equal(
    await studies.locator(".study-entry:not(.study-entry-current)").count(),
    9,
  );
  await click(studies.locator(".studies-current"), "Подробнее");
  const known = await api("system?id=2");
  await page.waitForFunction(
    (n) => document.querySelectorAll(".research-system-object").length === n,
    known.sites.length + 1,
  );
  assert.deepEqual(
    await studies
      .locator(".research-system-object")
      .evaluateAll((ns) => ns.map((n) => n.dataset.objectId)),
    [
      String(known.star.id),
      ...known.sites.toSorted((a, b) => a.orbit - b.orbit).map((s) => s.id),
    ],
  );
  const first = studies.locator(".research-system-object-trigger").first();
  await first.focus();
  await page.locator(".research-system-tooltip:not([hidden])").waitFor();
  assert.match(
    await page.locator(".research-system-tooltip").textContent(),
    /Спектральный класс/,
  );
  await page.keyboard.press("Escape");
  assert.equal(
    await page.locator(".research-system-tooltip:visible").count(),
    0,
  );
  const scroller = studies.locator(".research-system-scroll");
  await scroller.hover();
  await page.mouse.wheel(700, 0);
  await page.waitForFunction(
    () => document.querySelector(".research-system-scroll").scrollLeft > 0,
  );
  await click(studies, "← Назад");
  assert.equal(await search.inputValue(), "Catalog");
  assert.equal(
    await studies.locator(".studies-source-picker").textContent(),
    "Полученные сведения",
  );
  assert.equal(
    await studies.locator(".study-entry-selected").getAttribute("data-star"),
    "2",
  );
  await refresh();
  assert.equal(
    await studies.locator(".studies-current h2").textContent(),
    "Catalog 02",
  );
  // Pagination and pinned current system survive filters, search and detail navigation.
  await studies.locator(".studies-source-picker").click();
  await page
    .getByRole("menuitemradio", { name: "Все исследования", exact: true })
    .click();
  await studies.locator(".pagination button").last().click();
  const beforePage = await studies.locator(".pagination").textContent();
  await studies
    .locator(".study-entry:not(.study-entry-current)")
    .first()
    .getByRole("button", { name: "Подробнее", exact: true })
    .click();
  await studies.locator(".research-system-object").first().waitFor();
  await click(studies, "← Назад");
  assert.equal(await studies.locator(".pagination").textContent(), beforePage);
  await studies.locator(".study-entry-current").click();
  await click(studies.locator(".studies-current"), "Подробнее");
  await page.waitForFunction(
    () => document.querySelectorAll(".research-system-object").length > 6,
  );
  const expectedModels = [
    "garden",
    "rocky",
    "desert",
    "icy",
    "lava",
    "gas",
    "asteroids",
    "station",
  ];
  for (const model of expectedModels) {
    const img = studies
      .locator(`.research-system-art img[src="object-${model}.svg"]`)
      .first();
    await img.scrollIntoViewIfNeeded();
    await page.waitForFunction(
      (src) =>
        [...document.images].some(
          (i) =>
            i.getAttribute("src") === src && i.complete && i.naturalWidth > 0,
        ),
      `object-${model}.svg`,
    );
  }
  await click(studies, "← Назад");
  // Every spectral class is resolved from real metadata, never the active ship's class.
  for (let id = 2; id <= 8; id++) {
    const system = await api("system?id=" + id);
    const src = await page.evaluate(
      async (system) =>
        (await import("/features/research/system-view.js")).assetFor(
          { kind: "Звезда" },
          system.star,
        ),
      system,
    );
    assert.equal(src, `object-star-${system.star.class.toLowerCase()}.svg`);
    const res = await page.request.get(new URL(src, base).href);
    assert.equal(res.status(), 200);
  }
  // Existing map integration opens the selected system without changing position.
  await search.fill("");
  await studies.locator('.study-entry[data-star="2"]').click();
  await click(studies.locator(".studies-current"), "На карте");
  await page.locator("#window-galaxy[open]").waitFor();
  await close("galaxy");
  await close("studies");
  await page.locator("#nav-colonies").click();
  await colonies.locator(".colony-catalog-entry").first().waitFor();
  const data = await api("planet/projects");
  assert.equal(data.projects.length, 4);
  assert.ok(!data.projects.some((p) => p.id === fixture.private));
  for (const project of data.projects) {
    await colonies.locator(`[data-project-id="${project.id}"]`).click();
    await page.waitForFunction(
      () =>
        document
          .querySelector(".colony-catalog-planet")
          ?.getAttribute("src") === "object-garden.svg",
    );
    const system = await api("system?id=" + project.star),
      planet = system.sites.find(
        (s) => s.planet === project.planet && s.kind === "Садовый мир",
      );
    assert.equal(
      await colonies.locator(".colony-catalog-planet-name").textContent(),
      planet.name,
    );
  }
  await colonies.locator(`[data-project-id="${fixture.funding}"]`).click();
  await click(colonies, "Открыть проект");
  const projectWindow = page.locator(
    "#window-colony-project-" + fixture.funding,
  );
  await projectWindow.locator("select").waitFor();
  await projectWindow.locator("select").selectOption("credits");
  await projectWindow.locator('input[type="number"]').fill("1");
  await click(projectWindow, "Внести вклад");
  await click(page.locator("#window-confirm"), "Подтвердить");
  await page.waitForFunction(async (id) => {
    const r = await fetch("/api/planet/project?id=" + id);
    return (await r.json()).project.received.credits === 1;
  }, fixture.funding);
  await page.locator("#window-confirm").waitFor({ state: "hidden" });
  await close("colony-project-" + fixture.funding);
  await click(colonies, "Поверхность планеты");
  await page.locator("#window-galaxy[open] canvas").waitFor();
  await close("galaxy");
  await colonies.locator(".colony-catalog-refunds button").first().click();
  await page.waitForFunction(
    async () =>
      !(await (await fetch("/api/planet/projects")).json()).refunds.length,
  );
  await refresh();
  await colonies.locator(".colony-catalog-search").fill("Catalog 01");
  await refresh();
  assert.equal(
    await colonies.locator(".colony-catalog-search").inputValue(),
    "Catalog 01",
  );
  assert.equal(
    await colonies
      .locator('[aria-pressed="true"]')
      .getAttribute("data-project-id"),
    String(fixture.funding),
  );
  // Resize production Modules windows; no CSS/asset requests are replaced by mock responses.
  await page.setViewportSize({ width: 600, height: 820 });
  await colonies.evaluate((el) => {
    el.style.width = "570px";
    el.style.height = "700px";
    el.style.left = "5px";
    el.style.top = "65px";
  });
  assert.ok(
    await colonies
      .locator(".colony-catalog-layout")
      .evaluate(
        (e) => getComputedStyle(e).gridTemplateColumns.split(" ").length === 1,
      ),
  );
  await close("colonies");
  await open("studies");
  await studies.evaluate((el) => {
    el.style.width = "570px";
    el.style.height = "700px";
    el.style.left = "5px";
    el.style.top = "65px";
  });
  assert.ok(
    await studies
      .locator(".studies-layout")
      .evaluate(
        (e) => getComputedStyle(e).gridTemplateColumns.split(" ").length === 1,
      ),
  );
  mkdirSync(new URL("./artifacts/catalogs/", import.meta.url), {
    recursive: true,
  });
  await page.screenshot({
    path: new URL("./artifacts/catalogs/research-narrow.png", import.meta.url)
      .pathname,
  });
  await close("studies");
  await page.setViewportSize({ width: 1440, height: 1000 });
  await open("studies");
  await studies.evaluate((el) => {
    el.style.width = "1100px";
    el.style.height = "730px";
    el.style.left = "200px";
    el.style.top = "90px";
  });
  await page.screenshot({
    path: new URL("./artifacts/catalogs/research.png", import.meta.url)
      .pathname,
  });
  await close("studies");
  await page.locator("#nav-colonies").click();
  await colonies.locator(".colony-catalog-layout").waitFor();
  await colonies.evaluate((el) => {
    el.style.width = "950px";
    el.style.height = "750px";
    el.style.left = "230px";
    el.style.top = "90px";
  });
  await page.screenshot({
    path: new URL("./artifacts/catalogs/colonies.png", import.meta.url)
      .pathname,
  });
  await page.evaluate(async () =>
    (await import("/bootstrap.js")).application.I18n.set("en"),
  );
  await colonies
    .locator(".colony-catalog-heading h3")
    .filter({ hasText: "Colonies and projects" })
    .waitFor();
  await close("colonies");
  await open("studies");
  await click(studies.locator(".studies-current"), "Details");
  await studies.locator(".research-system-object").first().waitFor();
  await page.evaluate(async () =>
    (await import("/bootstrap.js")).application.I18n.set("ru"),
  );
  await studies
    .locator(".research-system-back")
    .filter({ hasText: "Назад" })
    .waitFor();
  await click(studies, "← Назад");
  assert.equal(
    await studies.locator(".study-entry-selected").getAttribute("data-star"),
    "2",
  );
  assert.deepEqual(errors, []);
  assert.deepEqual(broken, []);
  console.log(
    "PASS real API: research selection/filter/page/back/objects/SVG/tooltips/scroll/map; colonies IDs/project/contribution/surface/refund/selection/search; desktop/narrow",
  );
  // A second real account cannot see another pilot's projects or hidden objects.
  const peer = await browser.newContext({
    viewport: { width: 900, height: 800 },
  });
  await peer.addCookies([
    { name: fixture.cookie_name, value: fixture.peer_cookie, url: base.href },
  ]);
  const peerPage = await peer.newPage();
  await peerPage.goto(base.href);
  await peerPage.locator("#game:not([hidden])").waitFor();
  const hidden = await (
    await peerPage.request.get(new URL("/api/system?id=1", base).href)
  ).json();
  assert.equal(hidden.sites.length, 0);
  await peerPage.evaluate(async () => {
    const a = (await import("/bootstrap.js")).application;
    a.Modules.open("studies");
  });
  await peerPage.locator(".study-entry-current").waitFor();
  await click(peerPage.locator(".studies-current"), "Подробнее");
  await peerPage.waitForFunction(
    () => document.querySelectorAll(".research-system-object").length === 1,
  );
  assert.equal(
    await peerPage.locator(".research-system-object").getAttribute("data-kind"),
    "Звезда",
  );
  await peerPage.evaluate(async () =>
    (await import("/bootstrap.js")).application.Modules.close("studies"),
  );
  assert.equal(await peerPage.locator(".research-system-tooltip").count(), 0);
  // Use an existing player-owned project with no disclosed planet metadata.
  await peerPage.locator("#nav-colonies").click();
  await peerPage.waitForFunction(
    () =>
      document.querySelector(".colony-catalog-planet")?.getAttribute("src") ===
      "object-unknown.svg",
  );
  await peerPage
    .locator(".colony-catalog-detail")
    .filter({ hasText: "Сведения о планете недоступны" })
    .waitFor();
  const denied = await peerPage.request.post(
    new URL("/api/planet/action", base).href,
    {
      data: {
        action: "cancel",
        id: fixture.funding,
        request: "catalog-denied",
      },
    },
  );
  assert.equal(denied.status(), 409);
  await peer.close();
  const empty = await browser.newContext();
  const created = await empty.request.post(
    new URL("/api/register", base).href,
    { data: { username: "catalog_empty", password: "1" } },
  );
  assert.equal(created.status(), 200);
  const emptyPage = await empty.newPage();
  await emptyPage.goto(base.href);
  await emptyPage.locator("#game:not([hidden])").waitFor();
  await emptyPage.locator("#nav-colonies").click();
  await emptyPage
    .locator(".colony-catalog-detail")
    .filter({ hasText: "Нет доступных колоний или проектов колонизации" })
    .waitFor();
  assert.equal(await emptyPage.locator(".colony-catalog-entry").count(), 0);
  await empty.close();
  console.log(
    "PASS real API: undisclosed objects masked, neutral planet fallback, empty catalogue, unauthorized operation denied, RU/EN",
  );
} finally {
  await context.close();
  await browser.close();
}
