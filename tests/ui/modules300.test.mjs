import assert from "node:assert/strict";
import { test } from "node:test";
import { game } from "./harness.mjs";

test("planner waits inside its window, offers retry and keeps its actions", async t => {
  let reply;
  const g = await game(t, {signedIn:true, faults:[["planner", ({json}) => new Promise(resolve => {reply = (body,status=200) => resolve(json(body,status));})]]});
  await g.ready();
  await g.page.locator('#sidebar [data-open="tasks"]').click();
  const root=g.page.locator("#window-tasks"), loader=root.locator(".module-loading");
  await loader.waitFor();
  assert.equal(await root.locator(".module-content").evaluate(el=>el.inert),true);
  await g.page.waitForFunction(()=>document.querySelector(".planner"));
  assert.equal(await root.getByRole("button",{name:"Добавить задачу",exact:true}).isVisible(),false);
  assert.ok(reply, "planner request must have started");
  await reply({error:"Planner unavailable"},503);
  await root.getByRole("button",{name:"Повторить попытку"}).waitFor();
  g.handlers.set("planner", ({json})=>json([]));
  await root.getByRole("button",{name:"Повторить попытку"}).click();
  await loader.waitFor({state:"hidden"});
  assert.equal(await root.locator(".planner-stats > div").count(),3);
  assert.equal(await root.getByRole("button",{name:"Добавить задачу",exact:true}).isVisible(),true);
  assert.equal(await g.page.locator("#nav-shipyards").count(),0);
});

test("future modules wait for eager artwork and cancel without reopening", async t => {
  const g=await game(t,{signedIn:true});await g.ready();
  let finish, arrived;
  const requested = new Promise(resolve=>{arrived=resolve;});
  await g.context.route("**/future-art.svg", route=>new Promise(resolve=>{finish=()=>resolve(route.fulfill({contentType:"image/svg+xml",body:'<svg xmlns="http://www.w3.org/2000/svg" width="10" height="10"/>'}));arrived();}));
  await g.page.evaluate(()=>{
    const {Modules}=window.__testApp;
    const root=Modules.create("future-module","Future module");
    Modules.prepare("future-module",()=>{root.replaceChildren();const img=new Image();img.src="/future-art.svg";root.append(img);});
    Modules.open("future-module");
  });
  const root=g.page.locator("#window-future-module");
  await root.locator(".module-loading").waitFor();
  assert.equal(await root.locator(".module-content").evaluate(el=>el.inert),true);
  await requested;
  assert.ok(finish, "image request must have started");
  await finish();
  await root.locator(".module-loading").waitFor({state:"hidden"});
  assert.equal(await root.locator("img").evaluate(img=>img.complete&&img.naturalWidth>0),true);
  await g.page.evaluate(()=>{
    const {Modules}=window.__testApp;
    Modules.load("future-module",signal=>new Promise(resolve=>{
      window.finishModule=()=>{if(!signal.aborted)document.querySelector("#window-future-module .module-content").textContent="late";resolve();};
    }));
  });
  await root.locator(".module-loading").waitFor();
  await root.locator('.window-actions button').last().click();
  await g.page.evaluate(()=>window.finishModule());
  assert.equal(await root.evaluate(el=>el.open),false);
  assert.ok(!(await root.textContent()).includes("late"));
});

test("administrator opens a categorized module and losing the role clears it", async t=>{
  const g=await game(t,{signedIn:true,role:"admin"});await g.ready();
  await g.page.locator("#admin-menu").click();
  const root=g.page.locator("#window-administration");
  await root.locator(".administration-card").first().waitFor();
  assert.equal(await g.page.locator("#admin-menu").evaluate(el=>el.tagName),"BUTTON");
  await root.locator('[data-category="world"]').click();
  await root.locator('[data-open="galaxies"]').waitFor();
  assert.equal(await root.getByRole("button",{name:"Пропустить тик",exact:true}).count(),1);
  g.data.state.user.role="player";await g.refresh();
  assert.equal(await root.evaluate(el=>el.open),false);
  assert.equal(await g.page.locator("#admin-menu").isVisible(),false);
  await g.page.evaluate(()=>window.__testApp.Modules.open("administration"));
  assert.equal(await root.evaluate(el=>el.open),false);
  assert.equal(g.requests.filter(r=>r.method==="POST").length,0);
});

test("research details wait for the system DTO and retry inside the same window", async t=>{
  const g=await game(t,{signedIn:true});await g.ready();await g.open("studies");
  let answer;
  g.handlers.set("system",({json})=>new Promise(resolve=>{answer=()=>resolve(json({error:"System temporarily unavailable"},503));}));
  const requested=g.page.waitForRequest(r=>new URL(r.url()).pathname==="/api/system");
  await g.page.locator("#window-studies").getByRole("button",{name:"Подробнее",exact:true}).first().click();
  await requested;
  const root=g.page.locator("#window-system-objects");
  await root.locator(".module-loading").waitFor();
  assert.equal(await root.locator(".module-content").evaluate(el=>el.inert),true);
  await answer();
  await root.getByRole("button",{name:"Повторить попытку"}).waitFor();
  g.handlers.delete("system");
  await root.getByRole("button",{name:"Повторить попытку"}).click();
  await root.locator(".module-loading").waitFor({state:"hidden"});
  assert.equal(await root.evaluate(el=>el.open),true);
  assert.ok(await root.locator(".research-system-object").count()>0);
  assert.equal(await root.locator(".research-system-view").count(),1);
});

test("a ship module waits for its SVG hologram template", async t=>{
  const g=await game(t,{signedIn:true});await g.ready();
  let finish, arrived;
  const requested=new Promise(resolve=>{arrived=resolve;});
  await g.context.route("**/space/ships/*.svg",route=>new Promise(resolve=>{
    finish=()=>resolve(route.fulfill({contentType:"image/svg+xml",body:'<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10"><path d="M0 0L10 10"/></svg>'}));arrived();
  }));
  await g.page.locator('#sidebar [data-open="ship"]').click();
  await requested;
  const root=g.page.locator("#window-ship");
  assert.equal(await root.locator(".module-loading").isVisible(),true);
  await finish();
  await root.locator(".module-loading").waitFor({state:"hidden"});
  assert.ok(await root.locator(".hologram-surface svg").count()>0);
});
