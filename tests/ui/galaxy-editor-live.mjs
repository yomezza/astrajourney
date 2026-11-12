// Real Cosmic handlers and a disposable PostgreSQL schema; no mock API routes.
import assert from "node:assert/strict";
import { readFileSync, mkdirSync } from "node:fs";
import { chromium } from "playwright";
const fixture = JSON.parse(readFileSync(0, "utf8")),
  base = new URL(process.env.TERMINAL_TEST_URL);
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
try {
  const context = await browser.newContext({
    viewport: { width: 1440, height: 1000 },
  });
  await context.addCookies([
    { name: fixture.cookie_name, value: fixture.cookie, url: base.href },
  ]);
  const page = await context.newPage();
  page.setDefaultTimeout(12000);
  const errors = [],
    failed = [],
    requests = [];
  page.on("pageerror", (e) => errors.push(e.message));
  page.on("response", (r) => {
    if (r.status() >= 400 && !r.url().endsWith("/favicon.ico"))
      failed.push(`${r.status()} ${new URL(r.url()).pathname}`);
  });
  page.on("request", (r) => {
    if (r.method() === "POST" && r.url().includes("/api/admin/galaxies/"))
      requests.push({ url: r.url(), body: r.postDataJSON() });
  });
  const api = async (path) => {
    const r = await page.request.get(new URL("/api/" + path, base).href);
    assert.equal(r.status(), 200);
    return r.json();
  };
  await page.goto(base.href);
  await page.locator("#game:not([hidden])").waitFor();
  await page.evaluate(async () =>
    (await import("/bootstrap.js")).application.Modules.open("galaxies"),
  );
  const root = page.locator("#window-galaxies");
  await root.locator(".galaxy-create-button").waitFor();
  await root.locator(".galaxy-create-button").click();
  await root.locator("#galaxy-name").fill("Browser Helix");
  await root.locator("#galaxy-count").fill("50");
  assert.equal(await root.locator("#galaxy-count").getAttribute("min"), "5");
  assert.equal(
    await root.locator("#galaxy-count").getAttribute("max"),
    "50000",
  );
  await root.getByRole("radio", { name: "Кольцевая", exact: true }).click();
  const resources = root.locator("details").filter({
    has: page.locator("summary", { hasText: "Распределение ресурсов" }),
  });
  await resources.locator("summary").click();
  assert.equal(
    await root.locator("#galaxy-resource-colony_core").count(),
    0,
    "a technology must not become a deposit weight",
  );
  const fields = resources.locator('input[type="number"]');
  assert.ok((await fields.count()) > 5);
  for (const field of await fields.all()) await field.fill("100");
  await root.locator("#galaxy-resource-iron").fill("0");
  // Switching the catalogue must preserve the complete unsaved generation draft.
  await root.locator(".galaxy-admin-card").filter({ hasText: "Edem" }).click();
  await root
    .locator(".galaxy-admin-card")
    .filter({ hasText: "Browser Helix" })
    .click();
  assert.equal(
    await root.locator("#galaxy-name").inputValue(),
    "Browser Helix",
  );
  assert.equal(await root.locator("#galaxy-count").inputValue(), "50");
  assert.equal(await root.locator("#galaxy-resource-iron").inputValue(), "0");
  assert.equal(
    await root
      .getByRole("radio", { name: "Кольцевая", exact: true })
      .getAttribute("aria-checked"),
    "true",
  );
  const preview = root.getByRole("button", {
      name: "Предпросмотр",
      exact: true,
    }),
    apply = root
      .locator(".galaxy-editor-actions")
      .getByRole("button", { name: "Создать галактику", exact: true });
  const before = (await api("admin/galaxies")).galaxies;
  await preview.click();
  await root.locator(".galaxy-preview-ready canvas").waitFor();
  assert.equal(await apply.isEnabled(), true);
  const body = requests.find((r) => r.url.endsWith("/preview")).body;
  assert.deepEqual(
    Object.keys(body.settings.resource_weights).sort(),
    Object.keys(before[0].definition.settings.resource_weights).sort(),
  );
  assert.ok(!Object.hasOwn(body.settings.resource_weights, "colony_core"));
  assert.equal(body.settings.resource_weights.iron, 0);
  assert.ok(
    !(await api("admin/galaxies")).galaxies.some(
      (g) => g.definition.name === "Browser Helix",
    ),
    "preview must not create anything",
  );
  await root.getByRole("tab", { name: "Параметры", exact: true }).click();
  const resourceSection = root
    .locator("details")
    .filter({
      has: page.locator("summary", { hasText: "Распределение ресурсов" }),
    });
  if (!(await resourceSection.evaluate((el) => el.open)))
    await resourceSection.locator("summary").click();
  await root.locator("#galaxy-resource-iron").fill("");
  await resourceSection.locator("summary").click();
  const posts = requests.length;
  await preview.click();
  assert.equal(
    requests.length,
    posts,
    "invalid input must be caught before the API request",
  );
  assert.equal(
    await resourceSection.evaluate((el) => el.open),
    true,
    "invalid collapsed resource must be revealed",
  );
  await root.locator("#galaxy-resource-iron").fill("0");
  await root.locator("#galaxy-name").fill("Browser Helix Final");
  assert.equal(
    await apply.isDisabled(),
    true,
    "editing invalidates the preview token",
  );
  await preview.click();
  await page.waitForFunction(
    () =>
      document.querySelector(".galaxy-editor-status")?.dataset.state ===
      "ready",
  );
  mkdirSync(new URL("./artifacts/galaxy-editor/", import.meta.url), {
    recursive: true,
  });
  await root.evaluate((el) => {
    el.style.left = "180px";
    el.style.top = "80px";
    el.style.width = "1150px";
    el.style.height = "840px";
  });
  await page.screenshot({
    path: new URL("./artifacts/galaxy-editor/settings.png", import.meta.url)
      .pathname,
  });
  await apply.click();
  await page.waitForFunction(
    () =>
      document.querySelector(".galaxy-editor-heading h2")?.textContent ===
        "Browser Helix Final" && !document.querySelector(".galaxy-shapes"),
  );
  const galaxies = (await api("admin/galaxies")).galaxies,
    created = galaxies.find((g) => g.definition.name === "Browser Helix Final");
  assert.equal(created.stats.systems, 50);
  assert.equal(created.definition.shape, "rings");
  assert.equal(created.definition.settings.resource_weights.iron, 0);
  assert.equal(requests.filter((r) => r.url.endsWith("/apply")).length, 1);
  await root
    .locator(".galaxy-admin-editor")
    .evaluate((el) => (el.scrollTop = el.scrollHeight));
  await page.screenshot({
    path: new URL("./artifacts/galaxy-editor/created.png", import.meta.url)
      .pathname,
  });
  await page.setViewportSize({ width: 600, height: 820 });
  await root.evaluate((el) => {
    el.style.left = "5px";
    el.style.top = "65px";
    el.style.width = "580px";
    el.style.height = "700px";
  });
  await root
    .locator(".galaxy-admin-editor")
    .evaluate((el) => (el.scrollTop = 0));
  assert.ok(
    await root
      .locator(".galaxy-admin-layout")
      .evaluate((el) => getComputedStyle(el).flexDirection === "column"),
  );
  await page.screenshot({
    path: new URL("./artifacts/galaxy-editor/narrow.png", import.meta.url)
      .pathname,
  });
  assert.deepEqual(errors, []);
  assert.deepEqual(failed, []);
  console.log(
    "PASS: real UI preview/apply creates one ring galaxy; valid resource keys, zero weights, preserved draft, preview invalidation, desktop/narrow, no JS or HTTP errors",
  );
} finally {
  await browser.close();
}
