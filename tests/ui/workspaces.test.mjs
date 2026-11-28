import assert from "node:assert/strict";
import { test } from "node:test";
import { game } from "./harness.mjs";

const ids = ["42000000-0000-4000-8000-000000000001","42000000-0000-4000-8000-000000000002"];
const moduleID = "72000000-0000-4000-8000-000000000001";

test("Studio keeps two ship contexts and the same communication windows across switches", async (t) => {
  const stored = {
    revision:1,
    layout:{version:1,active:ids[0],default:ids[0],workspaces:[
      {id:ids[0],name:"Alpha",ship_id:41,modules:[
        {id:moduleID,type:"cargo",view:"window",binding:"workspace",ship_id:0,filter:"quantity",
          geometry:{x:235,y:240,w:400,h:300,pinned:false,minimized:false}},
      ]},
      {id:ids[1],name:"Beta",ship_id:82,modules:[
        {id:"72000000-0000-4000-8000-000000000002",type:"cargo",view:"window",binding:"workspace",ship_id:0},
      ]},
    ]},
  };
  const g = await game(t,{signedIn:true,faults:[["workspaces",({req,json,data}) => {
    if (req.method() === "GET") {
      const ship = data.fleet.ships[0];
      data.fleet.ships = [{...ship,id:41,name:"Same name",cargo:{ore:7}},
        {...ship,id:82,name:"Same name",cargo:{ore:19}}];
      return json(stored);
    }
    const next = req.postDataJSON();
    if (next.revision !== stored.revision) return json({error:"Conflict"},409);
    Object.assign(stored,{revision:stored.revision+1,layout:next.layout});
    return json(stored);
  }]]});
  await g.ready();
  await g.refresh();
  const {page} = g;
  const studio = page.locator("#studio");
  const first = page.locator("#window-studio-" + moduleID);
  await first.getByText("Same name · #41",{exact:true}).waitFor();
  assert.equal(await first.locator("dd").textContent(),"7");
  await page.route("**/chat/api/v1/**", route => {
    const path = new URL(route.request().url()).pathname.split("/").at(-1);
    return route.fulfill({json:path === "bootstrap" ? {
      user:{id:String(g.data.state.user.id),role:"player"},
      conversations:[],invitations:[],folders:[],settings:{},corporation:[],presence:[],public_new:[],
    } : []});
  });
  // Keep the production ChatClient and DOM. Substitute only its socket
  // transport here so this contract can run with system Chromium as well.
  await page.evaluate(() => {
    window.studioChatConnections = 0;
    window.WebSocket = class {
      static OPEN = 1;
      static CONNECTING = 0;
      readyState = 1;
      constructor() {
        window.studioChatConnections++;
        queueMicrotask(() => this.onmessage?.({data:JSON.stringify({
          type:"hello",health:{ready:true,cosmic:true,planet:true},
        })}));
      }
      send(raw) {
        const {request_id} = JSON.parse(raw);
        queueMicrotask(() => this.onmessage?.({data:JSON.stringify({type:"ack",request_id,result:{}})}));
      }
      close() { this.readyState = 3; }
    };
    window.__testApp.session.data.state.chat_enabled = true;
    window.dispatchEvent(new Event("gamedata"));
  });
  await g.open("chat");
  await g.open("notifications");
  await page.locator("#window-chat .chat-root").waitFor();
  await page.waitForFunction(() => document.querySelector("#window-notifications").open);
  await page.evaluate(() => {
    window.communicationDialogs = [document.querySelector("#window-chat"),document.querySelector("#window-notifications")];
  });
  // Switch via the actual navigation control while global windows remain alive.
  const before = await page.evaluate(() => window.studioChatConnections);
  await studio.getByRole("button",{name:"Beta",exact:true}).evaluate((b) => b.click());
  const second = page.locator("#window-studio-72000000-0000-4000-8000-000000000002");
  await second.getByText("Same name · #82",{exact:true}).waitFor();
  assert.equal(await second.locator("dd").textContent(),"19");
  assert.equal(await first.count(),0);
  assert.equal(await page.evaluate(() => window.communicationDialogs.every((el) => el.isConnected && el.open)),true);
  await studio.getByText("Раскладка сохранена").waitFor();
  assert.equal(stored.layout.active,ids[1]);
  assert.ok(before > 0);
  assert.equal(await page.evaluate(() => window.studioChatConnections),before);
  assert.deepEqual(g.requests.filter((r) => r.method === "POST").map((r) => r.path),["workspaces"]);
  await page.reload();
  await g.ready();
  await second.getByText("Same name · #82",{exact:true}).waitFor();
  await page.setViewportSize({width:320,height:700});
  await studio.getByLabel("Рабочее пространство",{exact:true}).evaluate((el) => {
    el.value = "42000000-0000-4000-8000-000000000001";
    el.dispatchEvent(new Event("change"));
  });
  await first.getByText("Same name · #41",{exact:true}).waitFor();
  assert.equal(await first.getByLabel("Сортировка трюма").inputValue(),"quantity");
  assert.ok((await first.boundingBox()).width <= 320);
  await studio.getByText("Раскладка сохранена").waitFor();
  assert.equal(stored.layout.workspaces[0].modules[0].geometry.w,400);
});

test("Studio saves CRUD in order and requires a fresh read after conflict", async (t) => {
  const stored = {
    revision:1,
    layout:{version:1,active:ids[0],default:ids[0],workspaces:[
      {id:ids[0],name:"Alpha",ship_id:0,modules:[]},
    ]},
  };
  let deny = false;
  const g = await game(t,{signedIn:true,faults:[["workspaces",({req,json}) => {
    if (req.method() === "GET") return json(stored);
    const next = req.postDataJSON();
    if (deny || next.revision !== stored.revision)
      return json({error:"Рабочие пространства изменились в другой вкладке"},409);
    Object.assign(stored,{revision:stored.revision+1,layout:next.layout});
    return json(stored);
  }]]});
  const studio = g.page.locator("#studio");
  const named = (name) => studio.getByRole("button",{name,exact:true});
  const saved = () => studio.getByText("Раскладка сохранена").waitFor();
  await studio.getByRole("button",{name:"Настроить пространство"}).click();
  await named("Создать пространство").click();
  await saved();
  const name = studio.getByLabel("Название пространства");
  await name.fill("Beta");
  await named("Переименовать").click();
  await saved();
  await named("Дублировать").click();
  await saved();
  assert.equal(stored.layout.workspaces.length,3);
  assert.equal(new Set(stored.layout.workspaces.map(w => w.id)).size,3);
  await named("Переместить влево").click();
  await saved();
  assert.equal(stored.layout.workspaces[1].id,stored.layout.active);
  await named("Назначить стартовым").click();
  await saved();
  const removed = stored.layout.active;
  await named("Удалить пространство").click();
  await g.page.locator("#window-confirm").getByRole("button",{name:"Подтвердить"}).click();
  await saved();
  assert.ok(!stored.layout.workspaces.some(w => w.id === removed));

  deny = true;
  await name.fill("Conflicting draft");
  await named("Переименовать").click();
  await studio.getByText("Рабочие пространства изменились в другой вкладке").waitFor();
  assert.equal(await name.inputValue(),"Conflicting draft");
  await named("Загрузить сохранённую раскладку").click();
  await studio.getByRole("button",{name:"Alpha",exact:true}).waitFor();
  assert.equal(stored.layout.workspaces[0].name,"Alpha");
});

test("Matrix command deck loads presets and persists card order and width", async (t) => {
  const id = "42000000-0000-4000-8000-000000000001";
  let saved;
  const g = await game(t,{signedIn:true,faults:[["workspaces",({req,json}) => {
    if (req.method() === "GET") return json(saved || {revision:1,layout:{
      version:1,active:id,default:id,workspaces:[{id,name:"Астра",ship_id:0,modules:[]}],
    }});
    const {revision,layout} = req.postDataJSON();
    saved = {revision:revision+1,layout};
    return json(saved);
  }]]});
  await g.ready();
  const {page} = g;
  await page.locator('.matrix-card[data-module="map"]').getByText("Текущая система").waitFor();
  const grid = page.locator(".matrix-grid");
  const overview = grid.locator('[data-module="overview"]');
  assert.equal(await grid.locator(".matrix-card").count(),5);
  assert.deepEqual(await page.locator(".studio-tabs button").allTextContents(),["Командование","Добыча","Торговля"]);
  assert.equal(await overview.locator("tbody tr").count(),2);
  const ships = overview.locator("[data-ship-context]");
  const otherShip = ships.last();
  const otherID = await otherShip.getAttribute("data-ship-context");
  const originalPilotShip = g.data.expedition.ship.id;
  assert.notEqual(Number(otherID), originalPilotShip);
  await otherShip.press("Enter");
  assert.equal(await otherShip.getAttribute("aria-pressed"),"true");
  assert.ok(await otherShip.evaluate(el => el === document.activeElement));
  assert.equal(await page.getByLabel("Корабль пространства",{exact:true}).inputValue(),otherID);
  assert.equal(await page.locator(".studio-window[open]").count(),0);
  const wide = overview.getByRole("button",{name:"Широкая карточка",includeHidden:true});
  assert.equal(await wide.isVisible(),false);
  assert.equal(await overview.locator("h2").getAttribute("draggable"),"false");
  await page.locator(".matrix-edit-toggle").click();
  await wide.click();
  await overview.getByRole("button",{name:"Переместить карточку вправо"}).click();
  assert.equal(await overview.locator("h2").getAttribute("draggable"),"true");
  await page.locator("#studio").getByText("Раскладка сохранена").waitFor();
  assert.equal(saved.layout.workspaces[0].modules[0].type,"operations");
  assert.equal(saved.layout.workspaces[0].ship_id,Number(otherID));
  assert.equal(saved.layout.workspaces[1].ship_id,0);
  assert.equal(g.data.expedition.ship.id,originalPilotShip);
  assert.equal(saved.layout.workspaces[0].modules[1].type,"overview");
  assert.ok(!saved.layout.workspaces[0].modules[1].wide);
  await page.reload();
  await g.ready();
  assert.equal(await grid.locator(".matrix-card").first().getAttribute("data-module"),"operations");
  const map = grid.locator('[data-module="map"]');
  await map.getByRole("button",{name:"Открыть карту"}).click();
  await page.locator("#window-galaxy[open]").waitFor();
  await page.evaluate(() => window.__testApp.Modules.close("galaxy"));
  for (const width of [1440, 1024, 768, 390, 320]) {
    await page.setViewportSize({width,height:800});
    await grid.waitFor();
    assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), `horizontal overflow at ${width}`);
    const bounds = await grid.boundingBox();
    const sidebar = await page.locator("#sidebar").boundingBox();
    if (sidebar) assert.ok(bounds.x >= sidebar.x + sidebar.width - 1, `sidebar overlays cards at ${width}px`);
    else assert.equal(await page.locator("#matrix-mobile").isVisible(),true);
    const cards = await grid.locator(".matrix-card").evaluateAll(items => items.map(item => {
      const {left,right} = item.getBoundingClientRect();
      return {left,right};
    }));
    assert.ok(cards.every(card => card.left >= bounds.x - 1 && card.right <= bounds.x + bounds.width + 1), `card outside grid at ${width}`);
  }
  assert.equal(g.requests.filter(r => r.method === "POST" && r.path !== "workspaces").length,0);
});

test("Studio asks before opening gameplay for a different ship", async (t) => {
  const workspace = "42000000-0000-4000-8000-000000000001";
  const g = await game(t, { signedIn: true, faults: [
    ["workspaces", ({json, data}) => json({
      revision: 1,
      layout: { version: 1, active: workspace, default: workspace, workspaces: [{
        id: workspace, name: "Добыча", customized: true, ship_id: data.fleet.ships[0].id,
        modules: [{ id: moduleID, type: "fleet", view: "card", wide: true,
          binding: "fixed", ship_id: data.fleet.ships[1].id }],
      }] },
    })],
    ["workbench", ({req, json, data}) => {
      if (req.method() === "GET") return json(data.workbench);
      const {ship} = req.postDataJSON();
      data.expedition.ship = data.fleet.ships.find(item => item.id === ship);
      return json({ ok: true });
    }],
  ] });
  await g.ready();
  const ship = g.data.fleet.ships[1];
  const card = g.page.locator('.matrix-card[data-module="fleet"]');
  const action = card.getByRole("button", {name: "Открыть корабль"});
  await card.getByRole("meter", {name: "Топливо"}).waitFor();
  const [projection, telemetry] = await card.locator(".matrix-ship-summary")
    .evaluate(el => [el.getBoundingClientRect().width, el.querySelector(".matrix-ship-telemetry").getBoundingClientRect().width]);
  assert.ok(projection <= 450 && telemetry < projection);
  const confirm = g.page.locator("#window-confirm");
  await action.click();
  await confirm.getByText(new RegExp(ship.name)).waitFor();
  assert.equal(await g.page.locator("#window-ship[open]").count(), 0);
  await confirm.getByRole("button", {name: "Отмена"}).click();
  assert.equal(g.requests.filter(req => req.method === "POST").length, 0);
  await action.click();
  await confirm.getByRole("button", {name: "Подтвердить"}).click();
  const target = g.page.locator("#window-ship[open]");
  await target.locator(".ship-inline-model[data-ship='" + ship.id + "']").waitFor();
  assert.deepEqual(g.requests.filter(req => req.method === "POST").map(req => req.body), [
    {action: "select_ship", ship: ship.id},
  ]);
});
