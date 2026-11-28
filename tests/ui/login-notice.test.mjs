import assert from "node:assert/strict";
import { test } from "node:test";
import { game } from "./harness.mjs";

async function logout(page) {
  await page.evaluate(() => document.getElementById("logout").click());
  await page.locator("#auth:not([hidden])").waitFor();
}

test("the notice appears on each successful login until dismissed permanently", async (t) => {
  const g = await game(t);
  const { page } = g;
  const notice = page.getByRole("dialog", { name: "Немного о том, куда вы попали" });
  const hide = notice.getByRole("checkbox", { name: "Больше не показывать" });
  const proceed = notice.getByRole("button", { name: "Продолжить" });
  await g.ready();
  assert.equal(await notice.isVisible(), false);

  await g.login({ keepNotice: true });
  await notice.waitFor();
  assert.equal(await notice.locator(".login-notice-body p").count(), 7);
  assert.equal(await notice.locator("strong").textContent(), "«Предложить идею»");
  assert.equal(await hide.isChecked(), false);
  assert.equal(await notice.locator("h2").evaluate((e) => document.activeElement === e), true);
  await page.keyboard.press("Escape");
  await notice.waitFor({ state: "hidden" });
  await g.refresh();
  assert.equal(await notice.isVisible(), false);
  await page.reload();
  await g.ready();
  assert.equal(await notice.isVisible(), false);

  await logout(page);
  await page.setViewportSize({ width: 320, height: 640 });
  await g.login({ keepNotice: true });
  await notice.waitFor();
  assert.equal(await hide.isChecked(), false);
  const body = notice.locator(".login-notice-body");
  assert.ok(await body.evaluate((el) => el.scrollHeight > el.clientHeight));
  const box = await proceed.boundingBox();
  assert.ok(box && box.x >= 0 && box.x + box.width <= 320 && box.y + box.height <= 640);
  await hide.check();
  await proceed.click();

  await logout(page);
  await g.login({ keepNotice: true });
  assert.equal(await notice.isVisible(), false);
  await logout(page);
  await page.reload();
  await g.ready();
  await g.login({ keepNotice: true });
  assert.equal(await notice.isVisible(), false);

  await logout(page);
  g.data.state.user = { ...g.data.state.user, id: 52, username: "another_pilot" };
  await g.login({ username: "another_pilot", keepNotice: true });
  await notice.waitFor();
  assert.equal(await hide.isChecked(), false);
  await proceed.click();
});

test("a rejected login does not open the notice", async (t) => {
  const g = await game(t, {
    faults: [["login", ({ json }) => json({ error: "Неверный пароль" }, 401)]],
  });
  await g.ready();
  await g.page.locator('#auth-form input[name="username"]').fill("test_pilot");
  await g.page.locator('#auth-form input[name="password"]').fill("wrong");
  await g.page.locator("#submit").click();
  await g.page.getByText("Неверный пароль", { exact: true }).waitFor();
  assert.equal(await g.page.locator("#login-notice").isVisible(), false);
});
