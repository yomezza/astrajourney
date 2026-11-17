import assert from "node:assert/strict";
import { createServer } from "node:http";
import { readFile, readdir } from "node:fs/promises";
import { chromium } from "playwright";
import { fixture } from "./fixtures.mjs";

const web = new URL("../../internal/server/web/", import.meta.url);
const mime = {
  html: "text/html",
  js: "text/javascript",
  css: "text/css",
  json: "application/json",
  svg: "image/svg+xml",
  png: "image/png",
};

// Each test owns its server, browser context, fixtures and request journal.
// There is intentionally no configurable target URL, database or remote service.
export async function game(
  t,
  { signedIn = false, role = "player", storage = {}, faults = [] } = {},
) {
  const allowed = new Set(await readdir(web, { recursive: true }));
  const unexpected = [],
    errors = [],
    requests = [],
    assets = [],
    handlers = new Map();
  const data = fixture();
  data.state.user.role = role;
  const session = { authenticated: signedIn };
  const server = createServer(async (req, res) => {
    try {
      const path = new URL(req.url, "http://localhost").pathname;
      const name = path === "/" ? "index.html" : path.slice(1);
      if (!allowed.has(name)) {
        res.writeHead(404);
        res.end();
        return;
      }
      const body = await readFile(new URL(name, web));
      res.writeHead(200, {
        "Content-Type":
          mime[name.split(".").at(-1)] || "application/octet-stream",
      });
      res.end(body);
    } catch {
      res.writeHead(500);
      res.end();
    }
  });
  await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
  const base = `http://127.0.0.1:${server.address().port}`;
  let browser, context;
  t.after(async () => {
    await context?.close();
    await browser?.close();
    await new Promise((resolve) => server.close(resolve));
    assert.deepEqual(
      unexpected,
      [],
      "Unexpected URLs, API routes or mutations",
    );
    assert.deepEqual(errors, [], "Unexpected browser errors");
  });
  const executablePath = process.env.CHROMIUM_EXECUTABLE_PATH;
  browser = await chromium.launch({
    ...(executablePath ? { executablePath } : {}),
    ...(process.env.CHROMIUM_NO_SANDBOX === "1"
      ? { args: ["--no-sandbox"] }
      : {}),
  });
  context = await browser.newContext({
    viewport: { width: 1440, height: 1000 },
    serviceWorkers: "block",
  });
  await context.addInitScript((values) => {
    // Seed only once: reload must exercise the values actually saved by the UI.
    if (sessionStorage.getItem("test-seeded")) return;
    for (const [key, value] of Object.entries(values))
      localStorage.setItem(key, value);
    sessionStorage.setItem("test-seeded", "true");
  }, storage);
  await context.addInitScript(() => {
    window.addEventListener(
      "DOMContentLoaded",
      async () => {
        const { application: app } = await import("/bootstrap.js");
        window.__testApp = app;
        window.__testReady = true;
      },
      { once: true },
    );
  });
  await context.route("**/*", async (route) => {
    const req = route.request(),
      url = new URL(req.url());
    if (url.origin !== base) {
      unexpected.push(`external ${req.method()} ${url.origin}${url.pathname}`);
      return route.abort("blockedbyclient");
    }
    if (!url.pathname.startsWith("/api/")) {
      assets.push(url.pathname);
      return route.continue();
    }
    const key = url.pathname.slice(5),
      method = req.method();
    const entry = {
      method,
      path: key,
      query: url.search,
      body: req.postDataJSON(),
    };
    requests.push(entry);
    const json = (body, status = 200) =>
      route.fulfill({
        status,
        contentType: "application/json",
        body: JSON.stringify(body),
      });
    if (handlers.has(key))
      return handlers.get(key)({ route, req, url, json, data, session });
    if (key === "server-status" && method === "GET")
      return json({ name: "Иггдрасиль", players: 1, bots: 2, version:"0.28.0" });
    if (key === "login" && method === "POST") {
      session.authenticated = true;
      return json({ ok: true });
    }
    if (key === "logout" && method === "POST") {
      session.authenticated = false;
      return json({ ok: true });
    }
    if (!session.authenticated)
      return json({ error: "Войдите в аккаунт" }, 401);
    if (method !== "GET") {
      unexpected.push(`unconfigured mutation ${key}`);
      return json({ error: "Unconfigured test mutation" }, 501);
    }
    if (key === "admin/users") return json(data.users);
    if (Object.hasOwn(data, key)) return json(data[key]);
    unexpected.push(`unconfigured read ${key}${url.search}`);
    return json({ error: "Unconfigured test read" }, 501);
  });
  for (const [key, handler] of faults) handlers.set(key, handler);
  const page = await context.newPage();
  page.setDefaultTimeout(10000);
  page.on("pageerror", (error) => errors.push(error.message));
  await page.goto(base);
  // Test-only inspection bridge. Production imports ES modules and does not
  // publish the old script globals on window.
  await page.waitForFunction(() => window.__testReady);
  await page.evaluate(() => window.__testApp.I18n.ready);
  const ready = () =>
    page.waitForFunction(
      () =>
        window.__testReady && document.getElementById("startup-screen").hidden,
    );
  const login = async () => {
    await page.locator('#auth-form input[name="username"]').fill("test_pilot");
    await page.locator('#auth-form input[name="password"]').fill("a");
    await page.locator("#submit").click();
    await page.waitForFunction(
      () =>
        window.__testApp.session.signedIn &&
        !document.getElementById("game").hidden,
    );
  };
  const open = async (id) => {
    await page.evaluate((id) => window.__testApp.Modules.open(id), id);
    await page.waitForFunction(
      (id) => document.getElementById("window-" + id)?.open,
      id,
    );
  };
  const refresh = () => page.evaluate(() => window.__testApp.refresh(true));
  return {
    page,
    context,
    base,
    data,
    session,
    requests,
    assets,
    handlers,
    errors,
    login,
    ready,
    open,
    refresh,
  };
}
