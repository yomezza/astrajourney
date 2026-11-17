import assert from "node:assert/strict";
import { test } from "node:test";
import { game } from "./harness.mjs";

test("system and route selectors follow viewed galaxy; market follows the active ship", async t => {
  const g = await game(t, {signedIn:true});
  await g.ready();
  g.data.galaxy.stars[1].galaxy = "asterion";
  g.data.galaxy.galaxies = [{id:"edem",name:"Edem",opened:true,radius:1200},{id:"asterion",name:"Asterion",opened:true,radius:1200}];
  await g.page.evaluate(async data => {
    (await import(new URL("./core/world-store.js", document.querySelector('script[type="module"][src$="bootstrap.js"]').src))).acceptWorld(data);
    window.dispatchEvent(new Event("universechange"));
  },g.data.galaxy);
  await g.open("galaxy");
  await g.page.locator("#map-system-id").click();
  assert.deepEqual(await g.page.locator('.picker-list [role="option"] .system-id').allTextContents(), ["1","3"]);
  await g.page.keyboard.press("Escape");
  await g.page.locator("#map-galaxy").click();
  await g.page.getByRole("option",{name:"Asterion",exact:true}).click();
  await g.page.locator("#map-system-id").click();
  assert.deepEqual(await g.page.locator('.picker-list [role="option"] .system-id').allTextContents(), ["2"]);
  await g.page.keyboard.press("Escape");
  await g.page.locator('#window-galaxy [data-tab="route"]').click();
  await g.page.locator("#route-to").click();
  assert.deepEqual(await g.page.locator('.picker-list [role="option"] .system-id').allTextContents(), ["2"]);
  await g.page.keyboard.press("Escape");
  await g.page.evaluate(() => window.__testApp.Modules.close("galaxy"));
  const original = structuredClone(g.data["market/global"].ports[0]);
  g.data["market/global"].ports.push({...original,star:2,name:"Other Galaxy Port",station:"other"});
  await g.open("market");
  await g.page.locator('#window-market [data-tab="global"]').click();
  await g.page.locator("#window-market .system-picker").click();
  assert.ok(!(await g.page.locator(".picker-list").textContent()).includes("Other Galaxy Port"));
  assert.deepEqual(await g.page.locator('.picker-list [role="option"] .system-id').allTextContents(),["1"]);
  assert.equal(g.requests.filter(r=>r.method==="POST").length,0);
});

test("profile retains its actions and every page version uses server metadata", async t => {
 const g=await game(t,{signedIn:true});await g.ready();await g.open("profile");
 assert.equal(await g.page.locator(".pilot-profile h2").textContent(),"test_pilot");
 assert.equal(await g.page.locator(".pilot-profile-stat strong").last().textContent(),"2");
 assert.deepEqual(await g.page.locator("[data-game-version]").allTextContents(),["v0.28.0","v0.28.0"]);
 await g.page.locator("#window-profile").getByRole("button",{name:"История финансов",exact:true}).click();
 await g.page.locator("#window-finance[open]").waitFor();
 assert.equal(g.requests.filter(r=>r.method==="POST").length,0);
});

test("colony filters, progress, actions and refresh preserve the selected project at narrow widths",async t=>{
 const g=await game(t,{signedIn:true});await g.ready();
 await g.page.evaluate(async()=>{
  const {mountColonyCatalog}=await import(new URL("./features/planet/colony-catalog.js", document.querySelector('script[type="module"][src$="bootstrap.js"]').src));
  const root=window.__testApp.Modules.create("colonies","Колонии",950,700);
  const data={projects:[{id:1,star:1,planet:1,name:"Haven",status:"building",progress:12,build_ticks:24,owner_user:51,owner:"test_pilot",sector:7},{id:2,star:1,planet:2,name:"Harbor",status:"complete"}]};
  window.__colonyActions=[];
  const options={language:()=>"ru",assetBase:"/",starName:()=>"Test Sol",resourceName:x=>x,onVisit(){},onProject:id=>window.__colonyActions.push(id),onSurface:(s,p)=>window.__colonyActions.push([s,p]),loadSystem:async()=>({star:{name:"Test Sol"},sites:[{id:"planet-1",planet:1,name:"Haven",kind:"Садовый мир"}]})};
  window.__colonies={root,data,options,mountColonyCatalog};mountColonyCatalog(root,data,options);window.__testApp.Modules.open("colonies");
 });
 const root=g.page.locator("#window-colonies");
 assert.equal(await root.locator("progress").getAttribute("value"),"12");
 await root.getByRole("button",{name:"Открыть проект",exact:true}).click();
 await root.getByRole("button",{name:"Поверхность планеты",exact:true}).click();
 assert.deepEqual(await g.page.evaluate(()=>window.__colonyActions),[1,[1,1]]);
 await root.locator('[data-filter="complete"]').click();
 assert.equal(await root.locator(".colony-catalog-entry").count(),1);
 await root.locator(".colony-catalog-search").fill("Missing");
 await root.getByRole("button",{name:"Сбросить фильтры",exact:true}).click();
 await root.locator('[data-project-id="2"]').click();
 await g.page.evaluate(()=>{const c=window.__colonies;c.mountColonyCatalog(c.root,c.data,c.options)});
 assert.equal(await root.locator('.colony-catalog-entry[aria-pressed="true"]').getAttribute("data-project-id"),"2");
 await g.page.setViewportSize({width:420,height:900});
 await root.evaluate(el=>{el.style.width="390px";el.style.height="760px";el.style.left="10px";el.style.top="65px"});
 assert.ok(await root.locator(".module-content").evaluate(el=>el.scrollWidth<=el.clientWidth+1));
 assert.equal(g.requests.filter(r=>r.method==="POST").length,0);
});
