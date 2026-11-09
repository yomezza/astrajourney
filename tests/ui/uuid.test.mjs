import assert from "node:assert/strict";
import { test } from "node:test";
import { readFile } from "node:fs/promises";
import { chromium } from "playwright";
import { uuid } from "../../internal/server/web/core/uuid.js";
test("UUID v4 preserves cryptographic randomness and canonical version bits", () => {
  const ids = new Set(Array.from({ length: 100 }, () => uuid()));
  assert.equal(ids.size, 100);
  for (const id of ids)
    assert.match(
      id,
      /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/,
    );
});
test("UUID works on an insecure HTTP IP origin with strict CSP and no randomUUID", async () => {
  const browser = await chromium.launch({
    ...(process.env.CHROMIUM_EXECUTABLE_PATH
      ? { executablePath: process.env.CHROMIUM_EXECUTABLE_PATH }
      : {}),
    args: process.env.CHROMIUM_NO_SANDBOX === "1" ? ["--no-sandbox"] : [],
  });
  try {
    const page = await browser.newPage();
    const code = await readFile(
      new URL("../../internal/server/web/core/uuid.js", import.meta.url),
      "utf8",
    );
    await page.route("**/*", (route) => {
      const path = new URL(route.request().url()).pathname;
      return route.fulfill({
        contentType: path === "/" ? "text/html" : "application/javascript",
        headers: { "Content-Security-Policy": "script-src 'self'" },
        body:
          path === "/"
            ? '<script type="module" src="/test.js"></script>'
            : path === "/uuid.js"
              ? code
              : 'import {uuid} from "/uuid.js";document.body.textContent=JSON.stringify({secure:isSecureContext,native:typeof crypto.randomUUID,id:uuid()});',
      });
    });
    await page.goto("http://192.0.2.1/");
    await page.waitForFunction(() => document.body.textContent.startsWith("{"));
    const result = JSON.parse(await page.locator("body").textContent());
    assert.equal(result.secure, false);
    assert.equal(result.native, "undefined");
    assert.match(result.id, /^[a-f0-9-]{36}$/);
  } finally {
    await browser.close();
  }
});
