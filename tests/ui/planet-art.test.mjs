import assert from "node:assert/strict";
import { test } from "node:test";
import { game } from "./harness.mjs";
import { localArtScene } from "./planet-art-fixture.mjs";
async function open(g, scene) {
  await g.page.evaluate(async (scene) => {
    const { Modules } = await import("/ui/runtime.js");
    const { createController } = await import("/features/planet/controller.js");
    const root = Modules.create("art-test", "Планетарная локация", 1000, 760);
    root.classList.add("visit-content");
    const viewport = document.createElement("div");
    viewport.className = "visit-viewport";
    const canvas = document.createElement("canvas");
    canvas.className = "visit-canvas";
    viewport.append(canvas);
    root.replaceChildren(viewport);
    Modules.open("art-test");
    const model = window.__testApp;
    model.artAbort = new AbortController();
    model.artController = await createController(
      canvas,
      scene,
      {
        actor: "self",
        x_q: 98 * 1024,
        y_q: 130 * 1024,
        server_time: Date.now(),
      },
      {
        current: () => !model.artAbort.signal.aborted,
        target: () => 1,
        signal: model.artAbort.signal,
      },
    );
  }, scene);
  await g.page.locator('.visit-canvas[data-ready="true"]').waitFor();
}
test("local art uses saved masks, global texture UV, one atlas and server-facing walking", async (t) => {
  const g = await game(t, { signedIn: true });
  await g.ready();
  assert.equal(
    g.assets.some((p) => p.includes("/planet/pixelart-v1/")),
    false,
  );
  const scene = localArtScene();
  const before = JSON.stringify(scene);
  await open(g, scene);
  const canvas = g.page.locator(".visit-canvas");
  assert.equal(await canvas.getAttribute("data-art"), "ready");
  assert.equal(await canvas.getAttribute("data-landing"), "true");
  assert.equal(
    g.assets.filter((p) => p.endsWith("player_suit_7x8_896x1024.png")).length,
    1,
  );
  assert.equal(
    g.assets.some((p) => p.includes("/frames/") || p.includes("habitat")),
    false,
  );
  await g.page.evaluate(async () => {
    const c = window.__testApp.artController;
    for (let i = 1; i <= 6; i++) {
      c.update({
        x_q: (98 + i * 0.4) * 1024,
        y_q: 130 * 1024,
        moving: true,
        facing: 6,
        server_time: Date.now(),
        remaining: 1,
      });
      await new Promise((r) => setTimeout(r, 100));
    }
  });
  assert.equal(await canvas.getAttribute("data-facing"), "6");
  assert.ok(Number(await canvas.getAttribute("data-animation")) > 0);
  await g.page.evaluate(() =>
    window.__testApp.artController.update({
      x_q: 100.4 * 1024,
      y_q: 130 * 1024,
      moving: false,
      facing: 6,
      server_time: Date.now(),
      remaining: 0,
    }),
  );
  await g.page.waitForFunction(
    () => document.querySelector(".visit-canvas")?.dataset.animation === "0",
  );
  await g.page.evaluate(() => window.__testApp.artController.zoom(0.7));
  if (process.env.PLANET_ART_TEST_SCREENSHOT)
    await g.page
      .locator("#window-art-test")
      .screenshot({ path: process.env.PLANET_ART_TEST_SCREENSHOT });
  assert.equal(JSON.stringify(scene), before);
  const uv = await g.page.evaluate(
    async (scene) => {
      const { acquireArt } = await import("/features/planet/scene-art.js");
      const { makeGround, groundKeys } =
        await import("/features/planet/scene-ground.js");
      const a = await acquireArt(groundKeys(scene));
      const first = makeGround(scene, a),
        other = makeGround({ ...scene, origin: { x: 1.25, y: 1 } }, a);
      const read = (g, x) => [
        ...g.canvas.getContext("2d").getImageData(x * 8, 130 * 8, 1, 1).data,
      ];
      const result = [read(first, 160), read(other, 128)];
      first.stop();
      other.stop();
      a.release();
      return result;
    },
    localArtScene({ wet: false }),
  );
  assert.deepEqual(uv[0], uv[1]);
  await g.page.setViewportSize({ width: 720, height: 800 });
  await g.page.evaluate(async () => {
    const { I18n } = await import("/core/i18n.js");
    I18n.set("en");
  });
  assert.match(await canvas.getAttribute("aria-label"), /sector|surface/i);
  const dimensions = await canvas.evaluate((c) => ({
    width: c.getBoundingClientRect().width,
    height: c.getBoundingClientRect().height,
    x: Number(c.dataset.xq),
  }));
  assert.ok(dimensions.width > 200 && dimensions.width <= 720);
  assert.ok(dimensions.height >= 240);
  assert.ok(Math.abs(dimensions.x - 100.4 * 1024) < 2);
  await g.page.evaluate(async () => {
    const { I18n } = await import("/core/i18n.js");
    I18n.set("ru");
  });
  assert.match(await canvas.getAttribute("aria-label"), /Поверхность/);
  await g.page.evaluate(() => {
    window.__testApp.artController.stop();
    window.__testApp.artAbort.abort();
  });
  const stats = await g.page.evaluate(async () => {
    const { artCacheStats } = await import("/features/planet/scene-art.js");
    return artCacheStats();
  });
  assert.equal(stats.pinned, 0);
  assert.ok(stats.bytes <= 24 * 1024 * 1024);
  assert.ok(stats.entries <= 20);
});
test("missing atlas degrades explicitly, releases leases and retries without stale renderer", async (t) => {
  const g = await game(t, { signedIn: true });
  await g.ready();
  await g.context.route("**/player_*_7x8_896x1024.png*", (r) => r.abort());
  await open(g, localArtScene());
  assert.equal(
    await g.page.locator(".visit-canvas").getAttribute("data-art"),
    "partial",
  );
  await g.page.evaluate(() => {
    window.__testApp.artAbort.abort();
    window.__testApp.artController.stop();
  });
  await g.context.unroute("**/player_*_7x8_896x1024.png*");
  await open(g, localArtScene());
  assert.equal(
    await g.page.locator(".visit-canvas").getAttribute("data-art"),
    "ready",
  );
  await g.page.evaluate(() => {
    window.__testApp.artController.stop();
    window.__testApp.artAbort.abort();
  });
  const stats = await g.page.evaluate(async () => {
    const { artCacheStats } = await import("/features/planet/scene-art.js");
    return artCacheStats();
  });
  assert.equal(stats.pinned, 0);
});
