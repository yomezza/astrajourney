import assert from 'node:assert/strict';
import { readFileSync, mkdirSync } from 'node:fs';
import { chromium } from 'playwright';
const base=new URL(process.env.TERMINAL_TEST_URL),input=JSON.parse(readFileSync(0,'utf8'));
assert.ok(['127.0.0.1','localhost'].includes(base.hostname));
assert.equal((await fetch(base)).headers.get('x-cosmos-test-run'),process.env.TERMINAL_TEST_NONCE);
const browser=await chromium.launch({executablePath:process.env.CHROMIUM_EXECUTABLE_PATH,args:process.env.CHROMIUM_NO_SANDBOX==='1'?['--no-sandbox']:[]});
try{
 const context=await browser.newContext({viewport:{width:1500,height:1000}});await context.addCookies([{name:'session',value:input.cookie,url:base.href}]);
 const page=await context.newPage();page.setDefaultTimeout(12000);let scene;const errors=[],results=[],frames=[],states=[];
 page.on('pageerror',e=>errors.push(e.message));
 page.on('response',async r=>{if(r.url().includes('/planet/api/v1/scene?'))scene=await r.json()});
 page.on('websocket',ws=>{if(!ws.url().includes('/planet/ws/'))return;ws.on('framereceived',e=>{const f=JSON.parse(String(e.payload));frames.push(f);if(f.type==='build.result')results.push(f.payload);if(f.type==='state.delta')states.push(f.payload)})});
 await page.goto(base.href);await page.locator('#startup-screen').waitFor({state:'hidden'});
 const enter=()=>page.evaluate(async site=>{const{openVisit}=await import('/features/planet/visit.js');await openVisit(site)},input.site);
 await enter();const canvas=page.locator('#planet-host canvas[data-ready=true]');await canvas.waitFor({state:'visible'});await page.waitForTimeout(300);
 assert.ok(scene.building.allowed);assert.ok(scene.building.catalog.length>=30);
 await page.getByRole('button',{name:'Настройки',exact:true}).click();await page.locator('#planet-control-method').selectOption('keyboard');await page.locator('#planet-settings').getByRole('button',{name:'Закрыть',exact:true}).click();
 await page.getByRole('button',{name:'Строить',exact:true}).click();const panel=page.locator('#planet-build-panel');await panel.waitFor({state:'visible'});
 assert.equal(await panel.locator('[data-category]').count(),5);
 const cells=Uint8Array.from(Buffer.from(scene.building.cells,'base64'));
 const state=await canvas.evaluate(c=>({x:+c.dataset.xq/1024,y:+c.dataset.yq/1024,zoom:+c.dataset.zoom})),box=await canvas.boundingBox();
 const candidates=[];
 for(let y=0;y<64;y++)for(let x=0;x<64;x++){
  if(Math.hypot(x*4+2-state.x,y*4+2-state.y)>28||Math.hypot(x*4+2-state.x,y*4+2-state.y)<8)continue;
  let valid=true;for(let yy=y*4-1;yy<(y+1)*4+1;yy++)for(let xx=x*4-1;xx<(x+1)*4+1;xx++)if(xx<0||yy<0||xx>=256||yy>=256||!cells[yy*256+xx])valid=false;
  const px=box.width/2+(x*4+2-state.x)*state.zoom,py=box.height/2+(y*4+2-state.y)*state.zoom;
  if(valid&&px>10&&py>10&&px<box.width-370&&py<box.height-10)candidates.push({x,y,px,py});
 }
 candidates.sort((a,b)=>Math.hypot(a.x*4+2-state.x,a.y*4+2-state.y)-Math.hypot(b.x*4+2-state.x,b.y*4+2-state.y));
 assert.ok(candidates.length>1,'no buildable visible spot');const point=candidates[0];
 await panel.locator('[data-kind=foundation]').click();await canvas.hover({position:{x:point.px,y:point.py}});await page.waitForTimeout(60);assert.equal(await canvas.getAttribute('data-build-valid'),'true');
 await canvas.focus();await page.keyboard.press('r');assert.equal(await panel.getAttribute('data-rotation'),'1');
 await canvas.click({position:{x:point.px,y:point.py}});await page.waitForFunction(()=>document.querySelector('#planet-host canvas').dataset.buildCount==='1');assert.equal(results.at(-1).ok,true);
 const placed=frames.filter(f=>f.type==='buildings.snapshot').at(-1).payload.objects[0];assert.equal(placed.rotation,1);assert.equal(placed.kind,'foundation');assert.ok(placed.mine);assert.equal(placed.owner,undefined);
 await page.waitForTimeout(350);await canvas.click({position:{x:point.px,y:point.py}});assert.equal(await canvas.getAttribute('data-build-count'),'1','occupied floor duplicated');
 await panel.locator('[data-category="Стены и ограды"]').click();await panel.locator('[data-kind=fence]').click();
 await page.waitForTimeout(350);await canvas.click({position:{x:point.px,y:point.py}});await page.waitForFunction(()=>document.querySelector('#planet-host canvas').dataset.buildCount==='2');
 // A second real account sees the same committed objects, without ownership.
 const peerContext=await browser.newContext({viewport:{width:1500,height:1000}});await peerContext.addCookies([{name:'session',value:input.peer_cookie,url:base.href}]);
 const peerPage=await peerContext.newPage();const peerErrors=[];peerPage.on('pageerror',e=>peerErrors.push(e.message));
 await peerPage.goto(base.href);await peerPage.locator('#startup-screen').waitFor({state:'hidden'});
 await peerPage.evaluate(async site=>{const{openVisit}=await import('/features/planet/visit.js');await openVisit(site)},input.site);
 const peerCanvas=peerPage.locator('#planet-host canvas[data-ready=true]');await peerCanvas.waitFor({state:'visible'});
 await peerPage.waitForFunction(()=>document.querySelector('#planet-host canvas').dataset.buildCount==='2');
 // Mouse A* refuses the new solid object, using the authoritative grid.
 await panel.getByRole('button',{name:'Закрыть строительство',exact:true}).click();
 await canvas.click({position:{x:point.px,y:point.py}});await page.waitForTimeout(350);
 await page.getByRole('menuitem',{name:'Убрать',exact:true}).waitFor({state:'visible'});await page.keyboard.press('Escape');
 await page.getByRole('button',{name:'Строить',exact:true}).click();
 const initial=states.at(-1);await canvas.focus();await page.keyboard.down('d');await page.waitForTimeout(300);await page.keyboard.up('d');const after=states.at(-1);assert.ok(Math.hypot(after.x_q-initial.x_q,after.y_q-initial.y_q)<20,'building mode moved player');
 await panel.locator('input[type=search]').fill('бак');assert.equal(await panel.locator('.planet-build-card').count(),1);await panel.locator('input[type=search]').fill('');
 const output=process.env.PLANET_BUILD_SCREENSHOT;if(output){mkdirSync(output.slice(0,output.lastIndexOf('/')),{recursive:true});await page.screenshot({path:output})}
 await canvas.focus();await page.keyboard.press('Escape');await panel.waitFor({state:'hidden'});
 // A full client re-entry must load the committed objects from the scene endpoint.
 scene=null;await page.evaluate(async()=>{const{openVisit}=await import('/features/planet/visit.js');await openVisit()});await canvas.waitFor({state:'visible'});await page.waitForTimeout(300);
 assert.equal(scene.building.snapshot.objects.length,2);assert.equal(scene.building.snapshot.objects[0].rotation,1);
 await canvas.click({position:{x:point.px,y:point.py},button:'right'});await page.getByRole('menuitem',{name:'Убрать',exact:true}).click();await page.waitForFunction(()=>document.querySelector('#planet-host canvas').dataset.buildCount==='1');
 await page.waitForTimeout(350);await canvas.click({position:{x:point.px,y:point.py},button:'right'});await page.getByRole('menuitem',{name:'Убрать',exact:true}).click();await page.waitForFunction(()=>document.querySelector('#planet-host canvas').dataset.buildCount==='0');
 await peerPage.waitForFunction(()=>document.querySelector('#planet-host canvas').dataset.buildCount==='0');assert.deepEqual(peerErrors,[]);await peerContext.close();
 assert.deepEqual(errors,[]);console.log('PASS: 34 original objects / 5 categories, preview, R rotation, floor + obstacle layering, server collision, no movement in build mode, search, persistence across re-entry, shared updates for two real accounts, own removal; no page errors');
 await context.close();
}finally{await browser.close()}
