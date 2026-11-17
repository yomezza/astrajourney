import assert from 'node:assert/strict';
import { readFileSync,mkdirSync } from 'node:fs';
import { chromium } from 'playwright';
const base=new URL(process.env.TERMINAL_TEST_URL),input=JSON.parse(readFileSync(0,'utf8'));
assert.ok(['127.0.0.1','localhost'].includes(base.hostname));assert.equal((await fetch(base)).headers.get('x-cosmos-test-run'),process.env.TERMINAL_TEST_NONCE);
const browser=await chromium.launch({executablePath:process.env.CHROMIUM_EXECUTABLE_PATH,args:process.env.CHROMIUM_NO_SANDBOX==='1'?['--no-sandbox']:[]});
try{
 const ctx=await browser.newContext({viewport:{width:1500,height:1000}});await ctx.addCookies([{name:'session',value:input.cookie,url:base.href}]);const page=await ctx.newPage();page.setDefaultTimeout(15000);
 const errors=[],frames=[];let scene;page.on('pageerror',e=>errors.push(e.message));page.on('response',async r=>{if(r.url().includes('/planet/api/v1/scene?'))scene=await r.json()});
 page.on('websocket',w=>{if(w.url().includes('/planet/ws/'))w.on('framereceived',e=>frames.push(JSON.parse(String(e.payload))))});
 await page.goto(base.href);await page.locator('#startup-screen').waitFor({state:'hidden'});await page.evaluate(async site=>{const{openVisit}=await import(new URL("./features/planet/visit.js", document.querySelector('script[type="module"][src$="bootstrap.js"]').src));await openVisit(site)},input.site);
 const canvas=page.locator('#planet-host canvas[data-ready=true]');await canvas.waitFor({state:'visible'});await page.waitForTimeout(250);
 const state=await canvas.evaluate(c=>({x:+c.dataset.xq/1024,y:+c.dataset.yq/1024,zoom:+c.dataset.zoom})),box=await canvas.boundingBox();
 const candidates=scene.objects.filter(o=>o.RX>0&&Math.hypot(o.position.X/1024-state.x,o.position.Y/1024-state.y)<25).sort((a,b)=>Math.hypot(a.position.X/1024-state.x,a.position.Y/1024-state.y)-Math.hypot(b.position.X/1024-state.x,b.position.Y/1024-state.y));
 let object;
 // Select by the title shown by the actual picking code; overlapping canopies
 // select the visible foreground object, never transparent PNG padding.
 for(const o of candidates){const x=box.width/2+(o.position.X/1024-state.x)*state.zoom,y=box.height/2+(o.position.Y/1024-state.y)*state.zoom-4;
  if(x<5||y<5||x>box.width-5||y>box.height-5)continue;
  await canvas.click({position:{x,y}});
  if(await page.getByRole('menuitem',{name:'Убрать',exact:true}).isVisible() && /дерево|Камень|Кустарник|Скальная/iu.test(await page.locator('.context-heading').innerText())){object=o;break}
  await page.keyboard.press('Escape');
 }
 assert.ok(object,'no natural menu');
 const output=process.env.PLANET_OBJECT_SCREENSHOT;if(output){mkdirSync(output.slice(0,output.lastIndexOf('/')),{recursive:true});await page.waitForTimeout(180);await page.screenshot({path:output})}
 await page.getByRole('menuitem',{name:'Убрать',exact:true}).click();await page.waitForFunction(()=>+document.querySelector('#planet-host canvas').dataset.removedCount===1);
 const snapshot=frames.filter(f=>f.type==='buildings.snapshot').at(-1).payload;const id=snapshot.removed[0];object=scene.objects.find(o=>o.id===id);assert.ok(object?.RX>0,'removed object was not solid');
 // Shared state reaches a second real account.
 const peer=await browser.newContext({viewport:{width:1500,height:1000}});await peer.addCookies([{name:'session',value:input.peer_cookie,url:base.href}]);const pp=await peer.newPage();let peerScene;pp.on('response',async r=>{if(r.url().includes('/planet/api/v1/scene?'))peerScene=await r.json()});
 await pp.goto(base.href);await pp.locator('#startup-screen').waitFor({state:'hidden'});await pp.evaluate(async site=>{const{openVisit}=await import(new URL("./features/planet/visit.js", document.querySelector('script[type="module"][src$="bootstrap.js"]').src));await openVisit(site)},input.site);await pp.locator('#planet-host canvas[data-ready=true]').waitFor({state:'visible'});assert.ok(peerScene.building.snapshot.removed.includes(id));await peer.close();
 // Walk into the removed object's former ground contact. The original scene
 // mask blocked this exact point, so successful movement proves collision removal.
 await page.waitForTimeout(350);const sx=await canvas.evaluate(c=>({x:+c.dataset.xq/1024,y:+c.dataset.yq/1024,zoom:+c.dataset.zoom}));
 await canvas.click({position:{x:box.width/2+(object.position.X/1024-sx.x)*sx.zoom,y:box.height/2+(object.position.Y/1024-sx.y)*sx.zoom},modifiers:["Shift"]});
 // A remaining canopy may intercept: close its menu and choose a point in the
 // restored footprint through the same bounded server route command.
 if(await page.getByRole('menu').isVisible())await page.keyboard.press('Escape');
 await page.waitForFunction(o=>{const c=document.querySelector('#planet-host canvas');return Math.hypot(+c.dataset.xq-o.position.X,+c.dataset.yq-o.position.Y)<800},object);
 await page.waitForTimeout(5500);assert.ok(!frames.some(f=>f.type==='error'&&f.payload.phase==='checkpoint_failed'));
 scene=null;await page.evaluate(async()=>{const{openVisit}=await import(new URL("./features/planet/visit.js", document.querySelector('script[type="module"][src$="bootstrap.js"]').src));await openVisit()});await canvas.waitFor({state:'visible'});await page.waitForTimeout(300);assert.ok(scene.building.snapshot.removed.includes(id));
 const now=await canvas.evaluate(c=>({x:+c.dataset.xq,y:+c.dataset.yq}));assert.ok(Math.hypot(now.x-object.position.X,now.y-object.position.Y)<800,'resume moved player off cleared footprint');
 assert.deepEqual(errors,[]);console.log('PASS: natural object context menu, server removal, shared persisted tombstone, collision restored, checkpoint and reconnect inside cleared footprint; no page errors');await ctx.close();
}finally{await browser.close()}
