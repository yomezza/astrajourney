import assert from "node:assert/strict";
import { test } from "node:test";
import { game } from "./harness.mjs";

test("admin service cards show connectivity, centered names and outlined status tags", async (t) => {
  const g = await game(t, { signedIn: true, role: "admin" });
  g.data["admin/services"] = {
    checked_at: new Date().toISOString(),
    services: [
      {
        id: "cosmic",
        name: "Космическая служба",
        status: "ready",
        info: [{ label: "Версия", value: "0.20.0" }],
      },
      {
        id: "planet",
        name: "Планетарная служба",
        status: "disabled",
        reason: "planetary_disabled",
        info: [{ label: "Ожидаемая схема", value: "6" }],
      },
    ],
  };
  await g.ready();
  await g.page.locator("#admin-menu").click();
  const root = g.page.locator("#window-services");
  await root.locator(".service-card").nth(1).waitFor();
  assert.equal(await root.locator(".service-card").count(), 2);
  assert.equal(
    await root.locator('[data-status="ready"]').textContent(),
    "Готова",
  );
  assert.equal(
    await root.locator('[data-status="disabled"]').textContent(),
    "Отключена",
  );
  const style = await root
    .locator(".service-status-tag")
    .first()
    .evaluate((el) => ({
      background: getComputedStyle(el).backgroundColor,
      align: getComputedStyle(el).justifyContent,
    }));
  assert.equal(style.background, "rgba(0, 0, 0, 0)");
  assert.equal(style.align, "center");
  assert.equal(await root.locator("hr").count(), 2);
  assert.equal(
    await root
      .locator("h3")
      .first()
      .evaluate((el) => getComputedStyle(el).textAlign),
    "center",
  );
  assert.equal(await root.locator("xpath=..").getAttribute("class"), "administration-outlet");
  assert.deepEqual(await g.page.evaluate(() => window.__testApp.Modules.openWindows().map(window => window.id)), ["administration"]);
  if (process.env.SERVICES_TEST_SCREENSHOT)
    await root.screenshot({ path: process.env.SERVICES_TEST_SCREENSHOT });
  const admin = g.page.locator("#window-administration");
  await admin.getByRole("button", {name:"Пользователи", exact:true}).click();
  await admin.locator("#window-users #user-search").waitFor();
  assert.equal(await root.isVisible(), false);
  await g.page.evaluate(() => window.__testApp.Modules.open("services"));
  await admin.locator("#window-services .service-card").first().waitFor();
  await admin.locator(":scope > .module-titlebar").getByRole("button", {name:"Закрыть", exact:true}).click();
  assert.equal(await root.isVisible(), false);
  assert.equal(
    g.requests.some((r) => r.method === "POST"),
    false,
  );
});
