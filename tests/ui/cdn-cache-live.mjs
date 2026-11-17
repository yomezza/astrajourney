// Real Go CDN + Cosmic + disposable PostgreSQL. No request interception: browser
// HTTP caching and Canvas origin checks must exercise actual responses.
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { chromium } from "playwright";
const fixture = JSON.parse(readFileSync(0, "utf8"));
const base = new URL(process.env.CDN_TEST_URL);
assert.ok(["127.0.0.1", "localhost", "[::1]"].includes(base.hostname));
assert.ok(process.env.CDN_TEST_NONCE?.startsWith("cdn-test-"));
assert.equal((await fetch(base)).headers.get("x-cosmos-test-run"), process.env.CDN_TEST_NONCE);
const browser = await chromium.launch({ executablePath: process.env.CHROMIUM_EXECUTABLE_PATH, args: process.env.CHROMIUM_NO_SANDBOX === "1" ? ["--no-sandbox"] : [] });
try {
  const context = await browser.newContext();
  await context.addCookies([{name:fixture.cookie_name,value:fixture.cookie,url:base.href}]);
  const page = await context.newPage();
  const errors = []; page.on("pageerror", e => errors.push(e.message));
  const load = async () => page.evaluate(async () => {
    const { ART_PATHS } = await import(new URL("./features/planet/art-pack.js", document.querySelector('script[type="module"][src$="bootstrap.js"]').src));
    const keys = ["ground_grass", "ground_dry", "ground_sand", "ground_rock", "ground_snow", "ground_volcanic", "atlas"];
    const result = [];
    for (const key of keys) {
      const url = ART_PATHS[key];
      if (!/^\/assets\/sha256\/[a-f0-9]{64}\//.test(url)) throw Error("mutable texture URL");
      const response = await fetch(url);
      if (!response.ok || !response.headers.get("cache-control").includes("immutable")) throw Error("texture response");
      const bitmap = await createImageBitmap(await response.blob());
      const canvas = document.createElement("canvas"); canvas.width = canvas.height = 32;
      const ctx = canvas.getContext("2d"); ctx.drawImage(bitmap, 0, 0, 32, 32);
      ctx.getImageData(0, 0, 1, 1); bitmap.close(); // A tainted Canvas throws here.
      result.push(url);
    }
    return result.map(url => performance.getEntriesByName(new URL(url, location.href).href).at(-1)?.transferSize);
  });
  await page.goto(new URL("/?cache-check=1", base).href);
  await page.locator("#game:not([hidden])").waitFor();
  await page.evaluate(async () => (await import(new URL("./bootstrap.js", document.querySelector('script[type="module"][src$="bootstrap.js"]').src))).application.Modules.open("ship"));
  await page.locator(".hologram-surface svg").first().waitFor();
  await page.waitForFunction(() => document.querySelector(".profile-symbol")?.getBBox().width > 0);
  const first = await load(); assert.ok(first.every(n => n > 0), "initial textures must cross HTTP");
  await page.goto(new URL("/?cache-check=2", base).href);
  await page.locator("#game:not([hidden])").waitFor();
  const second = await load(); assert.deepEqual(second, [0,0,0,0,0,0,0], "reopened page must use local HTTP cache");
  assert.deepEqual(errors, []);
  console.log(JSON.stringify({initial_png_requests:7, repeat_png_requests:0, repeated_transfer_sizes:second, canvas_readable:true, hologram_loaded:true}));
  await context.close();
} finally { await browser.close(); }
