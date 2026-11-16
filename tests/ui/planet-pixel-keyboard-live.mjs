import assert from 'node:assert/strict';
import { readFileSync, mkdirSync } from 'node:fs';
import { chromium } from 'playwright';
const base = new URL(process.env.TERMINAL_TEST_URL), input = JSON.parse(readFileSync(0,'utf8'));
assert.ok(['127.0.0.1','localhost'].includes(base.hostname));
assert.equal((await fetch(base)).headers.get('x-cosmos-test-run'),process.env.TERMINAL_TEST_NONCE);
const browser = await chromium.launch({executablePath:process.env.CHROMIUM_EXECUTABLE_PATH,args:process.env.CHROMIUM_NO_SANDBOX==='1'?['--no-sandbox']:[]});
try {
 const context=await browser.newContext({viewport:{width:1440,height:1000}});
 await context.addCookies([{name:'session',value:input.cookie,url:base.href}]);
 const page=await context.newPage();page.setDefaultTimeout(15000);
 const errors=[],assets=[],steering=[],snapshots=[];let connections=0,cached=0;
 page.on('pageerror',e=>errors.push(e.message));
 page.on('request',r=>{if(new URL(r.url()).pathname.endsWith('.png'))assets.push(new URL(r.url()).pathname)});
 const cdp=await context.newCDPSession(page);await cdp.send('Network.enable');cdp.on('Network.requestServedFromCache',()=>cached++);
 page.on('websocket',ws=>{
  if(!ws.url().includes('/planet/ws/'))return;connections++;
  ws.on('framesent',e=>{const f=JSON.parse(String(e.payload));if(f.type==='steer')steering.push(f)});
  ws.on('framereceived',e=>{const f=JSON.parse(String(e.payload));if(f.type==='state.delta')snapshots.push(f.payload)});
 });
 await page.goto(base.href);await page.locator('#startup-screen').waitFor({state:'hidden'});
 assert.equal(assets.some(p=>p.startsWith('/planet-pixelart-v1/')),false,'planet art loaded before entry');
 await page.evaluate(async site=>{const {openVisit}=await import('/features/planet/visit.js');await openVisit(site)},input.site);
 const canvas=page.locator('#planet-host canvas[data-ready="true"]');await canvas.waitFor({state:'visible'});
 assert.equal(await canvas.getAttribute('data-art'),'ready');
 assert.ok(assets.some(p=>p.includes('/compatibility-atlases/player_')));
 assert.ok(assets.every(p=>!p.startsWith('/planet-art/')&&!p.includes('surface-ground-')));
 const pos=()=>{const p=snapshots.at(-1);assert.ok(p,'missing authoritative state');return {x:p.x_q,y:p.y_q}};
 const distance=(a,b)=>Math.hypot(b.x-a.x,b.y-a.y)/1024;
 const settled=async()=>{await page.waitForTimeout(250);return pos()};
 await page.waitForTimeout(250);
 const start=pos();
 await page.keyboard.down('d');await page.waitForTimeout(350);await page.keyboard.up('d');
 assert.ok(distance(start,await settled())<.05,'mouse mode accepted WASD');
 await page.getByRole('button',{name:'Настройки',exact:true}).click();
 await page.locator('#planet-control-method').selectOption('keyboard');
 await page.locator('#planet-settings').getByRole('button',{name:'Закрыть',exact:true}).click();
 await canvas.focus();
 const before=pos();await page.keyboard.down('d');await page.waitForTimeout(350);await page.keyboard.up('d');
 const axisEnd=await settled(),axis=distance(before,axisEnd);
 assert.ok(axis>.7&&axis<2.2,'axis speed/position invalid '+axis);
 await page.keyboard.down('d');await page.keyboard.down('s');await page.waitForTimeout(350);await page.keyboard.up('d');await page.keyboard.up('s');
 const diagonalEnd=await settled(),diagonal=distance(axisEnd,diagonalEnd);
 assert.ok(diagonal>.6&&diagonal<2.2,'diagonal did not move safely '+diagonal);
 assert.ok(Math.abs(diagonal-axis)<.6,'diagonal speed differs '+axis+' / '+diagonal);
 const stopped=pos();await page.waitForTimeout(350);assert.ok(distance(stopped,pos())<.01,'release left movement running');
 // Physical codes, with Russian key labels, go through the real WebSocket.
 await page.evaluate(()=>window.dispatchEvent(new KeyboardEvent('keydown',{code:'KeyD',key:'в',bubbles:true,cancelable:true})));
 await page.waitForTimeout(300);
 await page.evaluate(()=>window.dispatchEvent(new KeyboardEvent('keyup',{code:'KeyD',key:'в',bubbles:true,cancelable:true})));
 assert.ok(distance(stopped,await settled())>.5,'Russian layout physical key ignored');
 // A focused real chat composer must not send movement intents.
 await page.evaluate(async()=>{const {Modules}=await import('/ui/runtime.js');Modules.open('chat')});
 const composer=page.locator('#window-chat textarea').first();await composer.waitFor({state:'visible'});await composer.focus();
 const chatStart=pos(),count=steering.length;await page.keyboard.type('wasd');await page.waitForTimeout(350);
 assert.equal(steering.length,count,'typing generated steering frames');assert.ok(distance(chatStart,pos())<.01,'chat typing moved player');
 await page.evaluate(async()=>{const {Modules}=await import('/ui/runtime.js');Modules.close('chat')});await canvas.focus();
 // Losing focus clears held keys immediately, independent of the server lease.
 await page.keyboard.down('d');await page.waitForTimeout(150);await page.evaluate(()=>window.dispatchEvent(new Event('blur')));
 const blurStop=await settled();await page.waitForTimeout(400);assert.ok(distance(blurStop,pos())<.01,'blur did not stop');await page.keyboard.up('d');
 // Hiding the tab clears the direction and never replays it on visibility restore.
 await page.keyboard.down('s');await page.waitForTimeout(120);
 await page.evaluate(()=>{Object.defineProperty(document,'hidden',{configurable:true,value:true});document.dispatchEvent(new Event('visibilitychange'))});
 const hiddenStop=await settled();await page.waitForTimeout(300);assert.ok(distance(hiddenStop,pos())<.01,'hidden tab kept steering');
 await page.evaluate(()=>{delete document.hidden;document.dispatchEvent(new Event('visibilitychange'))});await page.keyboard.up('s');
 // Mouse route is still available in keyboard mode and cancels the held direction.
 const current=await canvas.evaluate(c=>({x:+c.dataset.xq,y:+c.dataset.yq,zoom:+c.dataset.zoom})),box=await canvas.boundingBox();
 await canvas.click({position:{x:box.width/2-1*current.zoom,y:box.height/2}});
 await page.waitForTimeout(350);assert.equal(await canvas.getAttribute('data-control'),'mouse');
 await page.keyboard.down('s');await page.waitForTimeout(100);await page.keyboard.up('s');await settled();
 assert.equal(await canvas.getAttribute('data-control'),'keyboard');
 // Re-entry retains preference but not keys or duplicate handlers.
 await page.evaluate(async()=>{const {openVisit}=await import('/features/planet/visit.js');await openVisit()});
 await canvas.waitFor({state:'visible'});await page.waitForTimeout(350);
 assert.equal(await canvas.getAttribute('data-control-method'),'keyboard');
 const reentryStart=pos(),beforeFrames=steering.length;await canvas.focus();await page.keyboard.down('d');await page.waitForTimeout(350);await page.keyboard.up('d');await settled();
 assert.ok(steering.length-beforeFrames>=2&&steering.length-beforeFrames<=4,'duplicated keyboard handlers');
 assert.ok(distance(reentryStart,pos())>.3,'keyboard did not survive re-entry');
 assert.equal(connections,2,'unexpected reconnect beyond explicit re-entry');
 for(const f of steering){assert.deepEqual(Object.keys(f.payload).sort(),['dx','dy']);assert.ok(Math.abs(f.payload.dx)<=1&&Math.abs(f.payload.dy)<=1)}
 assert.deepEqual(errors,[]);
 // Reach a real inflated obstacle through A*, then walk against it with WASD.
 const obstacle=input.obstacle;
 const beforeObstacle=await canvas.evaluate(c=>({x:+c.dataset.xq,y:+c.dataset.yq,zoom:+c.dataset.zoom})), obstacleBox=await canvas.boundingBox();
 await canvas.click({position:{x:obstacleBox.width/2+(obstacle.x-beforeObstacle.x)/1024*beforeObstacle.zoom,y:obstacleBox.height/2+(obstacle.y-beforeObstacle.y)/1024*beforeObstacle.zoom}});
 await page.waitForFunction(o=>{const c=document.querySelector('#planet-host canvas');return Math.hypot(+c.dataset.xq-o.x,+c.dataset.yq-o.y)<220},obstacle);
 await settled();const wallStart=pos(),wallKey=obstacle.dx===1?'d':obstacle.dx===-1?'a':obstacle.dy===1?'s':'w';
 await canvas.focus();await page.keyboard.down(wallKey);await page.waitForTimeout(900);
 const wallStop=pos();await page.waitForTimeout(350);
 assert.ok(distance(wallStart,wallStop)<.6,'walked through collision boundary');
 assert.ok(distance(wallStop,pos())<.01,'kept moving into collision');
 await page.keyboard.up(wallKey);await settled();
 const cachedResources=await page.evaluate(()=>performance.getEntriesByType('resource').filter(e=>e.name.includes('/planet-pixelart-v1/')&&e.transferSize===0&&e.decodedBodySize>0).length);
 assert.ok(cachedResources>0,'versioned images did not reuse browser cache');
 const output=process.env.PLANET_PIXEL_SCREENSHOT;
 if(output){mkdirSync(output.slice(0,output.lastIndexOf('/')),{recursive:true});await page.screenshot({path:output});await page.getByRole('button',{name:'Настройки',exact:true}).click();await page.screenshot({path:output.replace('.png','-settings.png')});await page.keyboard.press('Escape')}
 console.log(`PASS: pixel art, mouse default/settings, WASD/Russian keys, release, diagonal ${diagonal.toFixed(2)} vs axis ${axis.toFixed(2)}, real chat typing, blur/hidden, collision, explicit re-entry, ${steering.length} bounded direction frames, browser cache events ${cached}, cached resources ${cachedResources}`);
 await context.close();
} finally {await browser.close()}
