import assert from "node:assert/strict";
import {test} from "node:test";
import {game} from "./harness.mjs";
import {planetFixture,planetState} from "./planet-fixture.mjs";

test("system background refresh stays interactive and cannot reopen over a planet", async t=>{
 const g=await game(t,{signedIn:true});
 g.data["planet/surface"]=planetFixture();g.data["planet/state"]=planetState();
 await g.ready();
 await g.page.locator('#maps-navigation > summary').click();
 await g.page.locator('#current-system-map').click();
 await g.page.waitForFunction(()=>document.querySelector('#map-breadcrumb')?.textContent.includes('Test Region'));
 const window=g.page.locator('#window-galaxy');
 await window.locator('.module-loading').waitFor({state:'hidden'});
 let release, arrived;
 const requested=new Promise(resolve=>{arrived=resolve;});
 const gate=new Promise(resolve=>{release=resolve;});
 let backgroundRequests=0;
 g.handlers.set('system',async({json,data})=>{
  backgroundRequests++;arrived();await gate;return json(data.system);
 });
 await g.page.evaluate(()=>window.dispatchEvent(new Event('gamedata')));
 await requested;
 assert.equal(await window.locator('.module-loading').isVisible(),false);
 assert.equal(await window.locator('.module-content').evaluate(el=>el.inert),false);
 await g.page.evaluate(async()=>{
  const {openPlanet}=await import(new URL('./features/planet/loader.js',document.querySelector('script[type="module"][src$="bootstrap.js"]').src));
  await openPlanet(1,1);
 });
 await g.page.waitForSelector('#planet-map[data-textures="ready"]');
 release();
 await g.page.evaluate(()=>new Promise(resolve=>requestAnimationFrame(()=>requestAnimationFrame(resolve))));
 await g.page.evaluate(()=>{
  window.__planetCanvas=document.querySelector('#planet-map');
  window.__mapLoadingFlashes=0;
  const dialog=document.querySelector('#window-galaxy');
  window.__mapLoadingObserver=new MutationObserver(records=>{
   if(dialog.classList.contains('module-pending')||records.some(record=>record.oldValue?.includes('module-pending')))window.__mapLoadingFlashes++;
  });
  window.__mapLoadingObserver.observe(dialog,{attributes:true,attributeFilter:['class'],attributeOldValue:true});
 });
 for(let n=0;n<5;n++)await g.refresh();
 assert.equal(backgroundRequests,1,'hidden system map must not poll underneath the planet');
 assert.equal(await window.locator('.module-loading').isVisible(),false);
 assert.equal(await g.page.evaluate(()=>document.querySelector('#planet-map')===window.__planetCanvas),true,'surface must keep its canvas and camera');
 assert.equal(await g.page.evaluate(()=>window.__mapLoadingFlashes),0,'background polls must not start a new readiness overlay');
 await g.page.evaluate(()=>window.__mapLoadingObserver.disconnect());
});

test("terminal modules refresh without loading overlays or stealing focus",async t=>{
 const g=await game(t,{signedIn:true});await g.ready();
 const ids=['ship','cargo','research','market','profile','tasks','finance','notifications'];
 for(const id of ids){
  await g.open(id);
  await g.page.locator('#window-'+id+' .module-loading').waitFor({state:'hidden'});
 }
 await g.page.evaluate(()=>{
  window.__allLoadingFlashes=0;
  window.__allLoadingObserver=new MutationObserver(records=>{
   if(records.some(record=>record.target.classList.contains('module-pending')||record.oldValue?.includes('module-pending')))window.__allLoadingFlashes++;
  });
  for(const dialog of document.querySelectorAll('dialog[open]'))window.__allLoadingObserver.observe(dialog,{attributes:true,attributeFilter:['class'],attributeOldValue:true});
  window.__testApp.Modules.focus('profile');
  window.__focusBeforePoll=document.activeElement;
 });
 for(let n=0;n<3;n++)await g.refresh();
 assert.equal(await g.page.evaluate(()=>window.__allLoadingFlashes),0);
 assert.equal(await g.page.evaluate(()=>document.activeElement===window.__focusBeforePoll),true);
 assert.equal(await g.page.locator('dialog[open].module-pending').count(),0);
 assert.equal(await g.page.locator('dialog[open] .module-content[inert]').count(),0);
 await g.page.evaluate(()=>window.__allLoadingObserver.disconnect());
});

test("shared background updates coalesce and cancel without blocking a module",async t=>{
 const g=await game(t,{signedIn:true});await g.ready();
 await g.page.evaluate(async()=>{
  const {Modules}=window.__testApp;
  const root=Modules.create('background-contract','Background');
  Modules.prepare('background-contract',()=>{root.textContent='ready';});
  await Modules.open('background-contract');
  window.__backgroundCalls=0;
  const task=async signal=>{
   window.__backgroundCalls++;
   await new Promise(resolve=>{window.__finishBackground=resolve;});
   if(!signal.aborted)root.textContent='updated';
  };
  window.__backgroundTask=Modules.update('background-contract',task);
  for(let n=0;n<10;n++)Modules.update('background-contract',task);
 });
 const dialog=g.page.locator('#window-background-contract');
 assert.equal(await dialog.locator('.module-loading').isVisible(),false);
 assert.equal(await dialog.locator('.module-content').evaluate(el=>el.inert),false);
 await g.page.evaluate(()=>window.__testApp.Modules.close('background-contract'));
 await g.page.evaluate(async()=>{await window.__backgroundTask;window.__finishBackground();});
 assert.equal(await dialog.evaluate(el=>el.open),false);
 assert.equal(await g.page.evaluate(()=>window.__backgroundCalls),1);
});
