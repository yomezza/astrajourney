import assert from "node:assert/strict";
import { test } from "node:test";
import { game } from "./harness.mjs";

test("The catalog can switch an open system map back to the galaxy", async t => {
  const g = await game(t,{signedIn:true});
  const {page} = g;
  await g.ready();
  await page.locator("#maps-navigation summary").click();
  await page.locator("#current-system-map").click();
  const map = page.locator("#window-galaxy .galaxy-content");
  await map.locator(".atlas-modes button[aria-pressed=true]").getByText("Карта текущей системы").waitFor();
  await page.keyboard.press("Control+k");
  const catalog = page.locator(".matrix-launcher");
  await catalog.getByRole("searchbox").fill("Галактическая карта");
  await catalog.getByRole("button",{name:"Галактическая карта",exact:true}).click();
  await map.locator(".atlas-modes button[aria-pressed=true]").getByText("Галактика",{exact:true}).waitFor();
});

test("Window shortcuts preserve the live view and closing restores the previous window", async t => {
  const g = await game(t,{signedIn:true});
  const {page} = g;
  await g.ready();
  const shortcut = page.locator('#sidebar [data-open="market"]');
  await shortcut.click();
  const quantity = page.locator("#trade-quantity");
  await quantity.fill("12");
  await quantity.evaluate(el => { el.dataset.testView = "original"; });
  const switcher = page.locator("#window-switcher");
  const market = switcher.getByRole("button",{name:"Рынок",exact:true});
  await market.evaluate(el => { el.dataset.testSwitcher = "original"; });
  await shortcut.click();
  assert.equal(await quantity.getAttribute("data-test-view"),"original");
  assert.equal(await quantity.inputValue(),"12");

  await page.locator('#sidebar [data-open="ship"]').click();
  await page.locator("#window-ship").getByRole("button",{name:"Закрыть",exact:true}).click();
  assert.equal(await market.getAttribute("data-test-switcher"),"original");
  assert.equal(await market.getAttribute("aria-pressed"),"true");
  assert.ok(await page.locator("#window-market .module-titlebar").evaluate(el => el === document.activeElement));
});

test("Command summaries distinguish station prices, player offers and real fleet activity", async t => {
  const g = await game(t,{signedIn:true,faults:[["workspaces",({req,json}) => {
    const id = "42000000-0000-4000-8000-000000000001";
    return req.method() === "GET"
      ? json({revision:1,layout:{version:1,active:id,default:id,workspaces:[{id,name:"Астра",ship_id:0,modules:[]}]}})
      : json({revision:2,layout:req.postDataJSON().layout});
  }]]});
  const {page,data} = g;
  const [first,second] = data.fleet.ships;
  second.destination = 3;
  data.workbench.research = [{ship:second.id,star:1,scanned:2,total:5,paused:true}];
  data.workbench.mining = [{ship_id:first.id,ship_name:first.name,star:1,quantity:12,resource:"iron"}];
  data.workbench.orders = [{id:7,star:1,station:"port-1",resource:"agni",quantity:45,price:9}];
  data.expedition.resources.push({id:"food",name:"Пищевой концентрат"});
  data.expedition.offers.push({resource:"food",stock:205,buy:195,sell:175});
  await g.ready();
  await g.refresh();

  const card = type => page.locator(`.matrix-card[data-module="${type}"]`);
  const market = card("market"), operations = card("operations");
  await market.getByRole("cell",{name:"5 CR",exact:true}).waitFor();
  await market.getByRole("cell",{name:"3 CR",exact:true}).waitFor();
  assert.equal(await market.locator('[data-resource="agni"]').count(),0);
  for (const width of [768,320]) {
    await page.setViewportSize({width,height:800});
    const table = market.locator(".matrix-table");
    assert.ok(await table.evaluate(el => el.scrollWidth <= el.clientWidth + 1),`market at ${width}px`);
    assert.ok(await table.locator("th").evaluateAll(headers =>
      headers.every(el => getComputedStyle(el).overflowWrap === "normal")));
  }
  await page.setViewportSize({width:1440,height:1000});

  const offerList = market.getByRole("button",{name:"Предложения игроков",exact:true});
  await offerList.press("Enter");
  await market.getByRole("cell",{name:"9 CR",exact:true}).waitFor();
  assert.ok(await offerList.evaluate(el => el === document.activeElement));
  await market.getByRole("button",{name:"Открыть рынок",exact:true}).click();
  await page.locator("#window-market").getByRole("tab",{name:"Предложения игроков",selected:true}).waitFor();
  await page.locator("#window-market").getByRole("button",{name:"Закрыть",exact:true}).click();
  await market.getByRole("button",{name:"Станция",exact:true}).click();

  await card("overview").getByRole("button",{name:second.name,exact:true}).click();
  await market.getByText("Локальные цены доступны для активного корабля у станции").waitFor();
  const scan = operations.getByRole("row").filter({hasText:"Сканирование"});
  assert.match(await scan.textContent(),/Test vessel 2.*2 \/ 5.*На паузе/);
  assert.equal(await scan.locator("[data-tone]").getAttribute("data-tone"),"paused");
  await operations.getByRole("row").filter({hasText:"Перелёт"}).getByText("Test Altair",{exact:true}).waitFor();
  assert.equal(await operations.locator("tbody tr").first().getByText(second.name,{exact:true}).count(),1);
  await page.locator("#studio").getByText("Раскладка сохранена").waitFor();
  assert.deepEqual(g.requests.filter(req => req.method === "POST").map(req => req.path),["workspaces"]);
  assert.equal(data.expedition.ship.id,first.id);
});

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
    const close = dialog.getByRole("button",{name:"Закрыть",exact:true});
    const icon = await close.locator(".line-icon").boundingBox();
    const button = await close.boundingBox();
    assert.ok(button.width >= 40 && icon.width === icon.height);
    assert.ok(Math.abs(icon.x + icon.width / 2 - button.x - button.width / 2) <= 1);
    assert.ok(Math.abs(icon.y + icon.height / 2 - button.y - button.height / 2) <= 1);
    const layout = await dialog.locator(".module-content").evaluate(el => ({
      width:el.clientWidth, scroll:el.scrollWidth,
    }));
    assert.ok(layout.scroll <= layout.width + 1, id + " overflows its sheet");
    if (id === "research" || id === "tasks") {
      const grid = dialog.locator(id === "research" ? ".research-layout > aside" : ".planner form").first();
      const columns = await grid.evaluate(el => getComputedStyle(el).gridTemplateColumns.split(" "));
      assert.equal(columns.length, 1, id + " compresses controls side by side");
    }
    if (id === "research") {
      const controls = await dialog.locator(".research-layout .table-wrap").evaluate(el =>
        [el.clientWidth, el.scrollWidth]);
      assert.ok(controls[1] <= controls[0] + 1, "mining action buttons require sideways scrolling");
    }
    await close.click();
  }
  assert.equal(g.requests.filter(r => r.method === "POST").length,0);
});

test("Mobile Matrix opens a usable full-width market and returns to its workspace", async t => {
  const g = await game(t,{signedIn:true});
  const {page} = g;
  g.data.workbench.deliveries = [{
    id:1,owner:g.data.state.user.id,from_star:101,to_star:202,
    resource:"iron",quantity:500,reward:100000,status:"open",
  }];
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
  const table = market.getByRole("region",{name:"Перевозки",exact:true});
  assert.equal(await table.getByRole("columnheader",{name:"Статус"}).getAttribute("scope"),"col");
  assert.equal(await table.getByRole("columnheader",{name:"Статус"}).evaluate(el => getComputedStyle(el).textAlign),"start");
  assert.equal(await table.getByRole("columnheader",{name:"Награда"}).evaluate(el => getComputedStyle(el).textAlign),"end");
  assert.ok(await table.evaluate(el => el.scrollWidth > el.clientWidth));
  await table.focus();
  await table.press("ArrowRight");
  await page.waitForFunction(() => document.querySelector("#window-market .table-wrap").scrollLeft > 0);
  await market.getByRole("button",{name:"Закрыть",exact:true}).click();
  await nav.getByRole("button",{name:"Пространства"}).click();
  assert.ok(await page.locator("#studio select").first().evaluate(el => el === document.activeElement));
  assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth));
});
