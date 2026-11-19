// Optional visual review of equipment UI with synthetic DTOs only.
import assert from 'node:assert/strict';
import { game } from './harness.mjs';
import { mkdir } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
const output = new URL('../../dist/equipment-preview/',import.meta.url);
const cleanup=[];
const g=await game({after(fn){cleanup.push(fn)}},{signedIn:true});
try {
 await g.ready();await mkdir(output,{recursive:true});
 const ship=g.data.expedition.ship;
 ship.module_limit=12;
 ship.modules=[
  {id:1,catalog_id:'cargo-1-C',name:'Грузовой расширитель',kind:'cargo',rating:'C',size:1,price:1200,paid:0,effects:{cargo_bonus:10},installed:true},
  {id:2,catalog_id:'extractor-universal-C',name:'Универсальный добытчик',kind:'extractor',rating:'C',size:2,price:800,paid:0,effects:{extractor:'universal'},installed:true},
  {id:3,catalog_id:'scanner-B',name:'Системный сканер',kind:'scanner',rating:'B',size:2,price:6000,paid:6000,effects:{scan_percent:20},installed:false},
 ];
 g.handlers.set('ship/modules',({json})=>json({ship,docked:true,used:3,cargo_used:10,stations:[{star:ship.star,object_id:ship.object_id,name:'Станция Перигей'}],star:ship.star,station:ship.object_id,offers:ship.modules.map(m=>({...m,quantity:4}))}));
 g.handlers.set('workbench',({req,json})=>{
  if(req.method()==='GET')return json(g.data.workbench);
  const body=req.postDataJSON(),m=ship.modules.find(m=>m.id===body.id);
  assert.equal(body.ship,ship.id);assert.ok(m);
  assert.ok(['module_install','module_remove'].includes(body.action));
  m.installed=body.action==='module_install';return json({ok:true});
 });
 await g.refresh();await g.open('ship');
 await g.page.locator('#window-ship').getByRole('button',{name:'Оснащение корабля',exact:true}).click();
 await g.page.locator('.equipment-workspace').waitFor();
 await g.page.evaluate(()=>{window.dragEvents=[];for(const type of ['dragstart','drop','dragend'])document.addEventListener(type,event=>window.dragEvents.push([type,event.target.className]),true);});
 const outfit=g.page.locator('#window-outfitting');
 await outfit.locator('.equipment-hold .equipment-card').dragTo(outfit.locator('.equipment-slot').filter({has:g.page.locator('h4',{hasText:'Сканер'})}));
 await outfit.locator('.equipment-installed .equipment-card').filter({hasText:'Системный сканер'}).waitFor();
 assert.equal(ship.modules[2].installed,true);
 await outfit.locator('.equipment-installed .equipment-card').filter({hasText:'Системный сканер'}).dragTo(outfit.locator('.equipment-hold h3'));
 await outfit.locator('.equipment-hold .equipment-card').filter({hasText:'Системный сканер'}).waitFor();
 assert.equal(ship.modules[2].installed,false);
 await g.page.screenshot({path:fileURLToPath(new URL('outfitting.png',output))});
 await g.page.locator('#window-outfitting').getByRole('button',{name:'Рынок модулей',exact:true}).click();
 await g.page.locator('.equipment-market').waitFor();
 await g.page.screenshot({path:fileURLToPath(new URL('market.png',output))});
 console.log('Equipment UI captures complete; browser errors:',JSON.stringify(g.errors));
}catch(error){console.log(JSON.stringify({requests:g.requests,errors:g.errors,dragEvents:await g.page.evaluate(()=>window.dragEvents)}));await g.page.screenshot({path:fileURLToPath(new URL('failure.png',output))});throw error;}finally{for(const fn of cleanup)await fn();}
