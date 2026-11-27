// Real Cosmic + Planet + PostgreSQL, nonce-guarded disposable integration host.
import assert from "node:assert/strict";
import { readFileSync, mkdirSync } from "node:fs";
import { chromium } from "playwright";
const base = new URL(process.env.TERMINAL_TEST_URL);
assert.ok(["127.0.0.1", "localhost"].includes(base.hostname));
assert.equal(
  (await fetch(base)).headers.get("x-cosmos-test-run"),
  process.env.TERMINAL_TEST_NONCE,
);
assert.ok(process.env.TERMINAL_TEST_NONCE?.startsWith("terminal-test-"));
const input = JSON.parse(readFileSync(0, "utf8"));
const browser = await chromium.launch({
  executablePath: process.env.CHROMIUM_EXECUTABLE_PATH || undefined,
  args: process.env.CHROMIUM_NO_SANDBOX === "1" ? ["--no-sandbox"] : [],
});
const screenshots = new URL(
  "./artifacts/discovery-guide-planet/",
  import.meta.url,
);
mkdirSync(screenshots, { recursive: true });
try {
  const context = await browser.newContext({
    viewport: { width: 1440, height: 1000 },
  });
  await context.addCookies([
    { name: "session", value: input.cookie, url: base.href },
  ]);
  const page = await context.newPage();
  page.setDefaultTimeout(18000);
  const errors = [];
  page.on("pageerror", (e) => errors.push(e.message));
  await page.goto(base.href);
  await page.locator("#game").waitFor({ state: "visible" });
  await page.locator("#startup-screen").waitFor({ state: "hidden" });
  const api = async (path, body) =>
    page.evaluate(
      async ({ path, body }) => {
        const { api } = await import(new URL("./core/api.js", document.querySelector('script[type="module"][src$="bootstrap.js"]').src));
        return api(path, body);
      },
      { path, body },
    );
  const op = () => crypto.randomUUID();
  const cat = await api("admin/guide/categories", {
    operation: op(),
    slug: "pilot-test",
    ru: { title: "Сведения пилота" },
    en: { title: "Pilot notes" },
  });
  const article = await api("admin/guide/articles", {
    operation: op(),
    slug: "first-planet",
    category: cat.id,
    locale: "ru",
    title: "Планетарная экспедиция",
    summary: "Проверка настоящей публикации",
    markdown:
      "## Посадка\nСерверноеусловиепоиска. **Посадочный модуль** — место возврата на корабль.\n- [Железо](game:resource:iron)",
    media: [
      { builtin: "object/garden", role: "hologram", caption: "Садовый мир" },
    ],
  });
  await api("admin/guide/publish", {
    operation: op(),
    id: article.id,
    expected_revision: article.version,
    locale: "ru",
    revision: article.revision,
    action: "publish",
  });
  await page.evaluate(async () => {
    const { Modules } = await import(new URL("./ui/runtime.js", document.querySelector('script[type="module"][src$="bootstrap.js"]').src));
    Modules.open("ship");
    Modules.open("guide");
  });
  const guide = page.locator("#window-guide");
  await guide.locator(".mode-c").waitFor();
  await guide.getByRole("button", { name: /Планетарная экспедиция/ }).waitFor();
  // Scroll a real dynamic game catalogue, then choose an already visible row.
  await guide.locator(".c-index").evaluate((e) => {
    e.scrollTop = e.scrollHeight - 220;
  });
  const before = await guide.locator(".c-index").evaluate((e) => e.scrollTop);
  await guide.getByRole("button", { name: /Планетарная экспедиция/ }).click();
  await guide
    .locator(".c-reading")
    .getByText("Серверноеусловиепоиска.", { exact: false })
    .waitFor();
  assert.ok(
    Math.abs(
      (await guide.locator(".c-index").evaluate((e) => e.scrollTop)) - before,
    ) < 3,
    "selecting a topic reset index scroll",
  );
  await guide
    .locator(".c-reading")
    .getByRole("button", { name: "В избранное", exact: true })
    .click();
  await guide.locator('.c-tabs [data-tab="favorites"]').click();
  assert.equal(await guide.locator(".result-row").count(), 1);
  await guide.locator('.c-tabs [data-tab="all"]').click();
  await guide.locator(".c-big-search input").fill("Серверноеусловиепоиска");
  await page.waitForFunction(
    () => document.querySelectorAll("#window-guide .result-row").length === 1,
  );
  await guide.locator(".c-big-search input").fill("");
  await page.waitForFunction(
    () => document.querySelectorAll("#window-guide .result-row").length > 30,
  );
  for (const [width, height] of [
    [810, 550],
    [420, 580],
  ]) {
    await guide.evaluate(
      (e, { width, height }) => {
        Object.assign(e.style, {
          width: width + "px",
          height: height + "px",
          left: "210px",
          top: "90px",
        });
      },
      { width, height },
    );
    await page.waitForTimeout(150);
    assert.equal(await guide.locator(".c-reading").isVisible(), true);
    await guide.screenshot({
      path: new URL(`guide-${width}x${height}.png`, screenshots).pathname,
    });
  }
  await page.evaluate(async () => {
    const { I18n } = await import(new URL("./core/i18n.js", document.querySelector('script[type="module"][src$="bootstrap.js"]').src));
    I18n.set("ru");
  });
  await guide.getByRole("button", { name: /Планетарная экспедиция/ }).waitFor();
  await guide.getByRole("button", { name: /Планетарная экспедиция/ }).click();
  await guide.locator(".c-reading").getByText("Серверноеусловиепоиска.", { exact: false }).waitFor();
  assert.equal(await guide.locator(".guide-fallback").count(), 0);
  assert.match(
    await guide.locator(".guide-title").innerText(),
    /Планетарная экспедиция/,
  );
  // Administrator edits the existing published article without changing its live version.
  await page.evaluate(async () => {
    const { Modules } = await import(new URL("./ui/runtime.js", document.querySelector('script[type="module"][src$="bootstrap.js"]').src));
    Modules.open("guide-admin");
  });
  const admin = page.locator("#window-guide-admin");
  await admin.getByRole("button", { name: /Планетарная экспедиция/ }).click();
  await admin.locator(".knowledge-markdown").waitFor();
  await admin
    .getByRole("button", { name: "Предпросмотр", exact: true })
    .click();
  await admin
    .locator(".knowledge-preview")
    .getByRole("heading", { name: "Посадка", exact: true })
    .waitFor();
  await admin.screenshot({
    path: new URL("guide-admin.png", screenshots).pathname,
  });
  await page.evaluate(async () => {
    const { Modules } = await import(new URL("./ui/runtime.js", document.querySelector('script[type="module"][src$="bootstrap.js"]').src));
    Modules.close("guide-admin");
  });
  const geometry = await guide.evaluate((e) => ({
    left: e.style.left,
    top: e.style.top,
    width: e.style.width,
    height: e.style.height,
    open: e.open,
  }));
  await page.evaluate(async (site) => {
    const { openVisit } = await import(new URL("./features/planet/visit.js", document.querySelector('script[type="module"][src$="bootstrap.js"]').src));
    await openVisit(site);
  }, input.site);
  const host = page.locator("#planet-host"),
    canvas = host.locator('canvas[data-ready="true"]');
  await canvas.waitFor({ state: "visible" });
  await page.locator("#startup-screen").waitFor({ state: "hidden" });
  assert.equal(await page.locator("#startup-screen").count(), 1);
  assert.equal(await page.locator("#window-planet-visit").count(), 0);
  assert.equal(await guide.isVisible(), false);
  assert.equal(
    await guide.evaluate((e) => e.open),
    true,
    "space windows were closed rather than suspended",
  );
  assert.deepEqual(
    await guide.evaluate((e) => ({
      left: e.style.left,
      top: e.style.top,
      width: e.style.width,
      height: e.style.height,
      open: e.open,
    })),
    geometry,
  );
  const first = await api("planet/visit");
  assert.equal(first.state, "active");
  await host.screenshot({
    path: new URL("planet-mode.png", screenshots).pathname,
  });
  await page.reload();
  await host.locator('canvas[data-ready="true"]').waitFor({ state: "visible" });
  await page.locator("#startup-screen").waitFor({ state: "hidden" });
  const resumed = await api("planet/visit");
  assert.equal(resumed.visit, first.visit, "F5 created another visit");
  // Click the real server landing object's projected ground contact.
  // Epoch comes from the server; the fixture exposes no browser position setter.
  const landing = await page.evaluate(async () => {
    const { api } = await import(new URL("./core/api.js", document.querySelector('script[type="module"][src$="bootstrap.js"]').src));
    const v = await api("planet/visit");
    const epoch = Number(
      document.querySelector(".visit-canvas").dataset.connectionEpoch,
    );
    const r = await fetch(
      `/planet/api/v1/scene?visit=${v.visit}&epoch=${epoch}`,
    );
    if (!r.ok) throw Error("scene " + r.status);
    return (await r.json()).landing;
  });
  // The sprite ground anchor can land on a transparent pixel. Verify the
  // hit using the real hover UI before clicking and sending a return request.
  const landingHint = host.getByText(/Площадка высадки и возврата к кораблю/);
  let hitLanding = false;
  for (const [dx, dy] of [[0, -4], [0, 0], [-4, -5], [4, -5], [0, -8], [-5, -9], [5, -9]]) {
    const point = await canvas.evaluate((el, { landing, dx, dy }) => {
      const bounds = el.getBoundingClientRect();
      const zoom = Number(el.dataset.zoom);
      return {
        x: bounds.left + bounds.width / 2 + ((landing.X - Number(el.dataset.xq)) / 1024 + dx) * zoom,
        y: bounds.top + bounds.height / 2 + ((landing.Y - Number(el.dataset.yq)) / 1024 + dy) * zoom,
      };
    }, { landing, dx, dy });
    await page.mouse.move(point.x, point.y);
    if (!await landingHint.isVisible()) continue;
    await page.mouse.click(point.x, point.y);
    hitLanding = true;
    break;
  }
  assert.ok(hitLanding, "landing sprite should be reachable through its visible pixels");
  await page
    .getByRole("menuitem", { name: "Вернуться на корабль", exact: true })
    .click();
  await page.waitForFunction(
    () => !document.body.classList.contains("planet-mode"),
  );
  await page.locator("#startup-screen").waitFor({ state: "hidden" });
  assert.equal((await api("planet/visit")).state, "none");
  assert.deepEqual(errors, []);
  console.log(
    "PASS real API/PG: Original C 810/420, scroll/favorites/body search, published Russian article, admin preview, full Planet Mode, preserved space geometry, one startup, F5 same visit, landing-only authorized return; no JS errors",
  );
  await context.close();
} finally {
  await browser.close();
}
