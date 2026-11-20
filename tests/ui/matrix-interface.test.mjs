import assert from "node:assert/strict";
import { test } from "node:test";
import { game } from "./harness.mjs";

test("Matrix catalog searches localized modules, restores windows and keeps administration private", async t => {
  const g = await game(t, {signedIn:true});
  const {page} = g;
  await g.ready();
  const catalog = page.locator(".matrix-launcher");
  await page.keyboard.press("Control+k");
  const search = catalog.getByRole("searchbox");
  await search.fill("Панель администратора");
  assert.equal(await catalog.getByText("Нет совпадений").isVisible(),true);
  await search.fill("Рынок");
  await search.press("Enter");
  await page.locator("#window-market[open]").waitFor();
  await page.keyboard.press("Control+k");
  assert.equal(await catalog.getByRole("heading",{name:"Открытые окна"}).isVisible(),true);
  await search.fill("Рынок");
  assert.equal(await catalog.getByRole("button",{name:"Рынок",exact:true}).count(),1);
  await search.press("ArrowDown");
  const result = catalog.getByRole("button",{name:"Рынок",exact:true});
  assert.ok(await result.evaluate(el => el === document.activeElement));
  await result.press("ArrowUp");
  assert.ok(await search.evaluate(el => el === document.activeElement));
  await page.keyboard.press("Escape");
  assert.equal(await catalog.isVisible(),false);
  await page.evaluate(() => window.__testApp.I18n.set("en"));
  await page.keyboard.press("Control+k");
  await catalog.getByRole("searchbox",{name:"Search modules"}).fill("Market");
  assert.ok(await catalog.getByRole("button",{name:"Market",exact:true}).count() > 0);
  await page.keyboard.press("Escape");
});

test("Narrow layouts keep full game controls and the profile reachable for administrators", async t => {
  const g = await game(t,{signedIn:true,role:"admin"});
  g.data.logistics = {personal:[],corporate:[],built:[],history:[]};
  const {page} = g;
  await g.ready();
  await page.setViewportSize({width:320,height:780});
  const profile = page.locator("#profile-menu").getByLabel("Профиль игрока",{exact:true});
  await profile.click();
  await page.locator("#profile-menu[open] #logout").waitFor();
  assert.ok((await profile.boundingBox()).x + (await profile.boundingBox()).width <= 320);
  await profile.click();
  const nav = page.locator("#matrix-mobile");
  const launcher = nav.getByRole("button",{name:"Модули",exact:true});
  const catalog = page.locator(".matrix-launcher");
  for (const [id, label, content] of [
    ["ship","Корабль",".ship-inline-model"],
    ["research","Добыча",".module-content h3"],
    ["logistics","Логистика",".logistics-toolbar"],
    ["tasks","Планировщик задач",".planner-workspace"],
    ["galaxy","Галактическая карта","canvas"],
    ["administration","Панель администратора",".administration-categories"],
  ]) {
    await launcher.click();
    await catalog.getByRole("searchbox").fill(label);
    await catalog.getByRole("button",{name:label,exact:true}).click();
    const dialog = page.locator("#window-"+id);
    await dialog.locator(content).first().waitFor();
    const layout = await dialog.locator(".module-content").evaluate(el => ({
      width:el.clientWidth, scroll:el.scrollWidth,
    }));
    assert.ok(layout.scroll <= layout.width + 1, id + " overflows its sheet");
    await dialog.getByRole("button",{name:"Закрыть",exact:true}).click();
  }
  assert.equal(g.requests.filter(r => r.method === "POST").length,0);
});

test("Mobile Matrix opens a usable full-width market and returns to its workspace", async t => {
  const g = await game(t,{signedIn:true});
  const {page} = g;
  await g.ready();
  await page.setViewportSize({width:320,height:740});
  const nav = page.locator("#matrix-mobile");
  await nav.getByRole("button",{name:"Модули",exact:true}).click();
  const catalog = page.locator(".matrix-launcher");
  await catalog.getByRole("searchbox").fill("Рынок");
  await catalog.getByRole("button",{name:"Рынок",exact:true}).click();
  const market = page.locator("#window-market");
  const tabs = market.getByRole("tab");
  await tabs.first().waitFor();
  assert.ok((await market.boundingBox()).width <= 320);
  assert.ok((await market.boundingBox()).y + (await market.boundingBox()).height <= (await nav.boundingBox()).y);
  await market.getByRole("tab",{name:"Перевозки",exact:true}).click();
  await market.getByText("Создать доставку",{exact:true}).waitFor();
  await market.getByRole("button",{name:"Закрыть",exact:true}).click();
  await nav.getByRole("button",{name:"Пространства"}).click();
  assert.ok(await page.locator("#studio select").first().evaluate(el => el === document.activeElement));
  assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth));
});
