// Optional visual review using synthetic DTOs; never connects to a game server.
import { game } from './harness.mjs';
import { mkdir } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
const output = new URL('../../dist/atlas-preview/', import.meta.url);
const cleanup = [];
const g = await game({after(fn){cleanup.push(fn)}}, {signedIn:true});
try {
  await mkdir(output, {recursive:true});
  g.data.state.user.username = "Пилигрим";
  g.data.fleet.ships.forEach((ship,i) => { ship.name = `Пилигрим ${i + 1}`; });
  g.data.system.sites[0].name = "Станция «Перигей»";
  g.data.system.sites[1].name = "Поле «Осколки»";
  g.data.system.region = "Рукав Эдема";
  await g.ready();
  g.data.galaxy.stars = Array.from({length:320},(_,i)=>({id:i+1,name:i===0?'ЭДЕМ · SOL':`ED-${String(i+1).padStart(4,'0')}`,galaxy:'edem',region:1,class:['G','B','M','K','A'][i%5],mass:1,luminosity:1,x:Math.cos(i*2.4)*Math.sqrt(i/320)*1080,y:Math.sin(i*2.4)*Math.sqrt(i/320)*1080}));
  g.data.galaxy.lanes = g.data.galaxy.stars.slice(1).map((s,i)=>({from: g.data.galaxy.stars.slice(0,i+1).reduce((best, candidate) => Math.hypot(candidate.x-s.x,candidate.y-s.y) < Math.hypot(best.x-s.x,best.y-s.y) ? candidate : best).id,to:s.id}));
  g.data.system.star=g.data.galaxy.stars[0];
  g.data.system.sites.push(...['Садовый мир','Газовый гигант','Ледяная','Каменистая'].map((kind,i)=>({id:`planet-${i}`,name:['Эдем I','Аврора','Таласса','Веста'][i],kind,planet:i+1,details:{id:i+1},orbit:7+i*9,resources:[]})));
  g.data.workbench.known = g.data.galaxy.stars.slice(0, 110).map(star=>({star:star.id}));
  await g.refresh();
  await g.open('galaxy');
  await g.page.locator('.atlas-identity h2').waitFor();
  await g.page.waitForTimeout(500);
  await g.page.screenshot({path:fileURLToPath(new URL('galaxy.png', output))});
  await g.page.locator('.atlas-modes button').nth(1).click();
  await g.page.locator('.galaxy-content[data-map-mode="system"]').waitFor();
  await g.page.waitForTimeout(500);
  await g.page.screenshot({path:fileURLToPath(new URL('system.png', output))});
  console.log('Visual captures created; browser errors:',JSON.stringify(g.errors));
} finally {for(const fn of cleanup) await fn();}
