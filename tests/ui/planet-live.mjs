// Runs only against TestPlanetBrowser180's nonce-marked disposable server.
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { chromium } from "playwright";
const base = new URL(process.env.TERMINAL_TEST_URL);
assert.ok(["127.0.0.1", "localhost"].includes(base.hostname));
const check = await fetch(base);
assert.ok(process.env.TERMINAL_TEST_NONCE?.startsWith("terminal-test-"));
assert.equal(
  check.headers.get("x-cosmos-test-run"),
  process.env.TERMINAL_TEST_NONCE,
);
const input = JSON.parse(readFileSync(0, "utf8"));
const browser = await chromium.launch({
  ...(process.env.CHROMIUM_EXECUTABLE_PATH
    ? { executablePath: process.env.CHROMIUM_EXECUTABLE_PATH }
    : {}),
  ...(process.env.CHROMIUM_NO_SANDBOX === "1"
    ? { args: ["--no-sandbox"] }
    : {}),
});
try {
  const context = await browser.newContext({
    viewport: { width: 1440, height: 1000 },
  });
  await context.addCookies([
    { name: "session", value: input.cookie, url: base.href },
  ]);
  let liveRoute, liveServer;
  await context.routeWebSocket("**/planet/ws/v1", (route) => {
    liveRoute = route;
    liveServer = route.connectToServer();
  });
  const page = await context.newPage();
  page.setDefaultTimeout(15000);
  const errors = [],
    artRequests = [];
  page.on("request", (r) => {
    if (new URL(r.url()).pathname.startsWith("/planet-art/"))
      artRequests.push(r.url());
  });
  page.on("pageerror", (e) => errors.push(e.message));
  page.on("console", (m) => {
    if (
      m.type() === "error" &&
      m.text().includes("Planet WS frame handling failed")
    )
      errors.push(m.text());
    if (m.type() !== "log") console.log("Browser:", m.type(), m.text());
  });
  page.on("response", async (r) => {
    if (r.status() >= 400)
      console.log(
        "HTTP failure:",
        new URL(r.url()).pathname,
        r.status(),
        (await r.text()).slice(0, 300),
      );
  });
  await page.goto(base.href);
  await page.locator("#game").waitFor({ state: "visible" });
  await page.locator("#startup-screen").waitFor({ state: "hidden" });
  assert.equal(artRequests.length, 0);
  await page.evaluate(async (site) => {
    const { openVisit } = await import("/features/planet/visit.js");
    await openVisit(site);
  }, input.site);
  const view = page.locator("#planet-host");
  try {
    await view
      .locator('canvas[data-ready="true"]')
      .waitFor({ state: "visible" });
  } catch (e) {
    console.log("Preview status:", await view.textContent());
    console.log("Page errors:", errors);
    console.log(
      "Alerts:",
      await page.locator("[role=alert],.toast").allTextContents(),
    );
    throw e;
  }
  assert.equal(
    await view
      .locator("canvas")
      .evaluate(
        (c) =>
          c
            .getContext("2d")
            .getImageData(
              Math.floor(c.width / 2),
              Math.floor(c.height / 2),
              1,
              1,
            ).data[3],
      ),
    255,
  );
  const peerContext = await browser.newContext({
    viewport: { width: 1440, height: 1000 },
  });
  await peerContext.addCookies([
    { name: "session", value: input.peer_cookie, url: base.href },
  ]);
  const peerPage = await peerContext.newPage();
  peerPage.on("pageerror", (e) => errors.push(e.message));
  await peerPage.goto(base.href);
  await peerPage.locator("#game").waitFor({ state: "visible" });
  await peerPage.locator("#startup-screen").waitFor({ state: "hidden" });
  await peerPage.evaluate(async (site) => {
    const { openVisit } = await import("/features/planet/visit.js");
    await openVisit(site);
  }, input.site);
  await peerPage
    .locator('.visit-canvas[data-ready="true"]')
    .waitFor({ state: "visible" });
  await page.waitForFunction(
    () => document.querySelector(".visit-canvas")?.dataset.actors === "2",
  );
  await peerPage.waitForFunction(
    () => document.querySelector(".visit-canvas")?.dataset.actors === "2",
  );
  const canvas = view.locator("canvas");
  assert.equal(await canvas.getAttribute("data-art"), "ready");
  assert.equal(await canvas.getAttribute("data-ground"), "textured");
  assert.equal(await canvas.getAttribute("data-landing"), "true");
  assert.equal(
    artRequests.filter((p) => p.endsWith("colonist_8dir_atlas.png")).length,
    1,
  );
  const start = await canvas.evaluate((c) => ({
    x: Number(c.dataset.xq),
    y: Number(c.dataset.yq),
  }));
  const box = await canvas.boundingBox();
  await canvas.click({
    position: { x: box.width / 2 + 32, y: box.height / 2 },
  });
  await page.waitForFunction(
    (x) =>
      Number(document.querySelector(".visit-canvas")?.dataset.xq) > x + 1024,
    start.x,
  );
  const goalX = (Math.floor(start.x / 1024) + 2) * 1024 + 512;
  await page.waitForFunction(
    (x) =>
      Math.abs(
        Number(document.querySelector(".visit-canvas")?.dataset.xq) - x,
      ) < 3,
    goalX,
  );
  assert.equal(await canvas.getAttribute("data-facing"), "6");
  await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
  await page.mouse.wheel(0, -250);
  await page.waitForFunction(
    () => Number(document.querySelector(".visit-canvas")?.dataset.zoom) > 16,
  );
  assert.ok(
    Math.abs((await canvas.evaluate((c) => Number(c.dataset.xq))) - goalX) < 3,
    "zoom changed the character position",
  );
  await peerPage.waitForFunction(
    (x) =>
      JSON.parse(
        document.querySelector(".visit-canvas")?.dataset.peers || "[]",
      ).some((p) => p.x_q > x + 1024),
    start.x,
  );
  const beforeReconnect = await page.evaluate(async () => {
    const { api } = await import("/core/api.js");
    return api("planet/visit");
  });
  // Cut a real proxied WS, then let the production reconnect code recover it.
  await liveRoute.close({ code: 1012, reason: "disposable test reconnect" });
  await liveServer.close();
  await page.waitForFunction(
    () => document.querySelector(".visit-canvas")?.dataset.ready === "false",
  );
  await view.locator('canvas[data-ready="true"]').waitFor({ state: "visible" });
  const recovered = await page.evaluate(async () => {
    const { api } = await import("/core/api.js");
    return api("planet/visit");
  });
  assert.equal(recovered.visit, beforeReconnect.visit);
  await page.waitForFunction(
    () => document.querySelector(".visit-canvas")?.dataset.actors === "2",
  );
  // Another tab takes over the same account. The old page must stop, rather
  // than automatically fighting the new controller through reconnect loops.
  const replacementPage = await context.newPage();
  await replacementPage.goto(base.href);
  // The authoritative visit is restored automatically in the replacement tab.
  await replacementPage
    .locator('.visit-canvas[data-ready="true"]')
    .waitFor({ state: "visible" });
  await view
    .getByRole("button", { name: "Продолжить высадку", exact: true })
    .waitFor({ state: "visible" });
  await page.locator("#startup-retry").click();
  await view.locator('canvas[data-ready="true"]').waitFor({ state: "visible" });
  await replacementPage
    .locator("#planet-host")
    .getByRole("button", { name: "Продолжить высадку", exact: true })
    .waitFor({ state: "visible" });
  await replacementPage.close();
  await page.waitForFunction(
    () => document.querySelector(".visit-canvas")?.dataset.actors === "2",
  );
  if (process.env.PLANET_TEST_SCREENSHOT)
    await view.screenshot({ path: process.env.PLANET_TEST_SCREENSHOT });
  assert.ok(artRequests.length >= 2, "scene art was not requested lazily");
  const first = await page.evaluate(async () => {
    const { api } = await import("/core/api.js");
    return api("planet/visit");
  });
  assert.equal(first.state, "active");
  await page.reload();
  await view.locator('canvas[data-ready="true"]').waitFor({ state: "visible" });
  const second = await page.evaluate(async () => {
    const { api } = await import("/core/api.js");
    return api("planet/visit");
  });
  assert.equal(second.visit, first.visit);
  await page.waitForFunction(
    (x) =>
      Math.abs(
        Number(document.querySelector(".visit-canvas")?.dataset.xq) - x,
      ) < 3,
    goalX,
  );
  const landing = await page.evaluate(async () => {
    const { api } = await import("/core/api.js");
    const visit = await api("planet/visit");
    const epoch =
      document.querySelector(".visit-canvas").dataset.connectionEpoch;
    const response = await fetch(
      `/planet/api/v1/scene?visit=${visit.visit}&epoch=${epoch}`,
    );
    if (!response.ok) throw Error("scene " + response.status);
    return (await response.json()).landing;
  });
  const pos = await canvas.evaluate((c) => ({
    x: Number(c.dataset.xq),
    y: Number(c.dataset.yq),
    zoom: Number(c.dataset.zoom),
  }));
  const returnedBox = await canvas.boundingBox();
  await canvas.click({
    position: {
      x: returnedBox.width / 2 + ((landing.X - pos.x) / 1024) * pos.zoom,
      y: returnedBox.height / 2 + ((landing.Y - pos.y) / 1024) * pos.zoom,
    },
  });
  await page
    .getByRole("menuitem", { name: "Вернуться на корабль", exact: true })
    .click();
  await page.waitForFunction(
    () => !document.body.classList.contains("planet-mode"),
  );
  await peerPage.waitForFunction(
    () => document.querySelector(".visit-canvas")?.dataset.actors === "1",
  );
  await peerContext.close();
  assert.deepEqual(errors, []);
  console.log(
    "PASS: two real browser accounts see each other and movement; forced WS disconnect automatically resumes the same visit; replaced tabs stop and require explicit takeover; click/zoom/reload/return work; peer leave removes avatar; lazy art; no page errors",
  );
  await context.close();
} finally {
  await browser.close();
}
