// Astra Journey UI/UX advanced labs. Original UI/renderers + isolated fictitious data.
// No mutation calls to game APIs. All async leases and event listeners are disposed on close.
import { UI, Modules } from '/ui/runtime.js';
import { createVisual, createSceneSprites } from '/features/planet/scene-actors.js';
import { acquireArt } from '/features/planet/scene-art.js';
import { terrain, texture, materials } from '/features/planet/terrain.js';
import { resourceTile } from '/features/inventory/resource-tile.js';
import { paintFlag } from '/features/corporations/flag.js';
import { registerTooltip } from '/ui/tooltip.js';
import { cursorMore } from '/ui/pager.js';

const demos = new Map();
const $ = (tag, text, className) => {
  const n = document.createElement(tag);
  if (text !== undefined && text !== null) n.textContent = String(text);
  if (className) n.className = className;
  return n;
};
const btn = (text, action, disabled = false) => UI.button(text, action, disabled);
const label = (text, child) => {
  const n = $('label');
  n.append($('span', text));
  if (child) n.append(child);
  return n;
};
const group = (...nodes) => {
  const n = $('div', null, 'uiux-advanced-group');
  n.append(...nodes);
  return n;
};
const say = (host, text) => {
  const n = host.querySelector('.uiux-advanced-status');
  if (n) n.textContent = text;
};
const status = (host, text = '') => {
  const n = $('p', text, 'uiux-advanced-status');
  n.setAttribute('role', 'status');
  host.append(n);
  return n;
};
const sample = (name, values) => ({ name, ...values });
function alive(host, controller) {
  return host.isConnected && !controller.signal.aborted;
}
function register(host, setup) {
  disposeAdvancedFor(host);
  const abort = new AbortController();
  const clean = [];
  const scope = {
    signal: abort.signal,
    alive: () => alive(host, abort),
    cleanup: fn => clean.push(fn),
    listen: (el, name, fn, options) => {
      el.addEventListener(name, fn, options);
      clean.push(() => el.removeEventListener(name, fn, options));
    },
    timer: fn => {const t = setInterval(fn, 280);clean.push(() => clearInterval(t));return t;},
    defer: fn => {const t = setTimeout(fn, 0);clean.push(() => clearTimeout(t));return t;}
  };
  demos.set(host, () => {
    abort.abort();
    for (const fn of clean.splice(0).reverse()) try {fn();} catch (error) {
      console.warn('[UI/UX cleanup]', error);
    }
  });
  setup(scope);
}
export function disposeAdvancedFor(host) {
  if (demos.has(host)) {
    const stop = demos.get(host);
    demos.delete(host);
    stop();
  }
}
export function disposeAdvanced() {
  for (const host of [...demos.keys()]) disposeAdvancedFor(host);
}
export function disposeAdvancedUnder(parent) {
  if (!parent) return;
  for (const host of [...demos.keys()])
    if (parent.contains(host)) disposeAdvancedFor(host);
}
const fixed = (v, n = 1) => Number(v).toFixed(n);
function makeGeography() {
  const w = 33, h = 33, heights = [], water = [];
  for (let y = 0; y < h; y++) for (let x = 0; x < w; x++) {
    const nx = x / (w - 1), ny = y / (h - 1);
    const lake = Math.exp(-((nx - .69) ** 2 + (ny - .31) ** 2) * 58);
    const z = 560 + 84 * Math.sin(x * .23) * Math.cos(y * .29) +
      65 * Math.sin(y * .51 + x * .11) - 230 * lake;
    heights.push(z);
    water.push(lake > .48 ? 2 : 0);
  }
  return {
    grid_w: w, grid_h: h, width: 256, height: 256, sea_level: 455,
    heights, water, temperature: 18,
    sectors: [
      { center: { x: 24, y: 34 }, biome: 'Сухие равнины' },
      { center: { x: 184, y: 148 }, biome: 'Тёплые равнины' },
      { center: { x: 96, y: 212 }, biome: 'Холодные равнины' }
    ]
  };
}
const geography = makeGeography();
const planetArts = ['atlas','tree_old','tree_young','bush_sparse','rock_flat',
                    'rock_large_cracked','grass_tuft','grass_dry','selection_ring',
                    'footstep_dust','landing'];
const plantObjects = [
  sample('tree_old', { id: 't1', x: 36, y: 39 }),
  sample('tree_young', { id: 't2', x: 57, y: 51 }),
  sample('bush_sparse', { id: 'b1', x: 42, y: 59 }),
  sample('rock_flat', { id: 'r1', x: 66, y: 38 }),
  sample('rock_large_cracked', { id: 'r2', x: 26, y: 57 }),
  sample('grass_tuft', { id: 'g1', x: 47, y: 34 })
];
function makeScene() {
  return {
    objects: plantObjects.map(o => ({
      id: o.id,
      asset: '/planet-art/' + o.name + '.png',
      position: { X: Math.round(o.x * 1024), Y: Math.round(o.y * 1024) }
    })),
    landing: {X: 68 * 1024, Y: 66 * 1024}
  };
}
function sceneDemo(item, host) {
  register(host, scope => {
    const pane = group();
    const canvas = $('canvas', null, 'uiux-advanced-canvas');
    canvas.tabIndex = 0;
    canvas.setAttribute('aria-label','Планетарная сцена с интерактивными объектами');
    pane.append(canvas);
    const panel = group();
    const help = $('small','Кликните дерево/камень; курсор показывает hit-test. WASD / стрелки перемещают персонажа.');
    const statusNode = $('p','Загрузка оригинальных PNG…','uiux-advanced-status');
    host.append(pane, panel, help, statusNode);
    const mode = item.id.split('.').at(-1);
    const focus = mode === 'tree' ? 'tree_old' : mode === 'rock' ? 'rock_flat' : 'actor';
    let player = {x: 48, y: 48, visual: createVisual(), latest:{
      moving:false, received:performance.now(),facing:0
    }, nickname:'Stellar_07',colour:'#e9c38b'};
    const view = {cx:47,cy:50,zoom:3.6,worldW:100,worldH:70};
    const scene = createSceneSprites(makeScene(), {get:() => null});
    let art = {get:()=>null,release:()=>{}};
    let raster = terrain(geography,'Садовый мир');
    let frame = null, selected = null;
    const paint = () => {
      if(!scope.alive())return;
      const w = Math.max(260,canvas.clientWidth||340),h=196,
        ratio = Math.min(window.devicePixelRatio || 1, 2);
      canvas.width = Math.round(w*ratio);canvas.height = Math.round(h*ratio);
      const ctx = canvas.getContext('2d');
      ctx.setTransform(ratio,0,0,ratio,0,0);
      ctx.clearRect(0,0,w,h);
      ctx.fillStyle='#101f24';ctx.fillRect(0,0,w,h);
      const z=view.zoom;
      ctx.translate(w/2-view.cx*z,h/2-view.cy*z);
      ctx.scale(z,z);
      ctx.drawImage(raster,0,0,256,256);
      const people = [player];
      scene.draw(ctx,{...view,worldW:w/z,worldH:h/z},people,performance.now(),false);
      if(selected){
        const o=plantObjects.find(e=>e.id===selected);
        if(o){
          ctx.save();ctx.strokeStyle='#efad58';ctx.lineWidth=.8;
          ctx.beginPath();ctx.arc(o.x,o.y,2.5,0,Math.PI*2);ctx.stroke();ctx.restore();
        }
      }
    };
    const pos = event => {
      const r=canvas.getBoundingClientRect();
      return {x:view.cx+(event.clientX-r.left-r.width/2)/view.zoom,
              y:view.cy+(event.clientY-r.top-r.height/2)/view.zoom};
    };
    const hit = p => scene.hit(p);
    canvas.onpointermove = event => {
      const info=hit(pos(event));
      canvas.style.cursor=info?'pointer':'crosshair';
      statusNode.textContent=info?info.name+' · '+info.text:'Выберите природный объект';
      paint();
    };
    canvas.onpointerleave=()=>{hit({x:-100,y:-100});paint();};
    canvas.onclick = event => {
      const p=pos(event),info=hit(p);
      if(info){
        const item=plantObjects.find(o=>Math.hypot(o.x-p.x,(o.y-p.y)*1.7)<2.2);
        selected=item?.id||null;
        statusNode.textContent='Выбрано: '+info.name+' ('+(selected||'?')+')';
      } else if(mode==='actor'||mode==='scene'){
        player.x=p.x;player.y=p.y;player.latest.received=performance.now();
        statusNode.textContent='Персонаж перемещён · '+fixed(player.x)+' / '+fixed(player.y);
      }
      paint();
    };
    const move=(dx,dy)=>{
      player.x=Math.max(10,Math.min(82,player.x+dx));
      player.y=Math.max(10,Math.min(78,player.y+dy));
      player.latest.moving=true;player.latest.received=performance.now();
      player.latest.facing= dx>0?2:dx<0?6:dy<0?0:4;
      statusNode.textContent='Stellar_07 · '+fixed(player.x)+' / '+fixed(player.y);
      paint();
    };
    const controls=[['←',()=>move(-3,0)],['↑',()=>move(0,-3)],
                   ['↓',()=>move(0,3)],['→',()=>move(3,0)]];
    controls.forEach(([text,fn])=>panel.append(btn(text,fn)));
    scope.listen(canvas,'keydown',e=>{
      const key=e.key.toLowerCase();
      const offsets={'arrowleft':[-3,0],a:[-3,0],'arrowright':[3,0],d:[3,0],
        'arrowup':[0,-3],w:[0,-3],'arrowdown':[0,3],s:[0,3]};
      if(offsets[key]){e.preventDefault();move(...offsets[key]);}
    });
    scope.cleanup(()=>{scene.stop();raster.width=0;raster.height=0;art.release();});
    scope.listen(window,'resize',paint);
    scope.listen(window,'moduleresize',paint);
    requestAnimationFrame(paint);
    acquireArt(planetArts,scope.signal).then(lease=>{
      if(!scope.alive()){lease.release();return;}
      art.release();art=lease;
      const actual = createSceneSprites(makeScene(),art);
      // Replace methods of the existing probe only after source artwork is available.
      scene.hit=actual.hit;scene.draw=actual.draw;scene.stop=actual.stop;
      statusNode.textContent=lease.missing.length ?
        'Часть PNG недоступна: '+lease.missing.join(', ') :
        'Оригинальные PNG загружены · интерактивная сцена готова';
      paint();
    }).catch(error=>{if(scope.alive())statusNode.textContent='PNG недоступны: '+error.message;});
    if(mode==='tree')panel.append(btn('Найти дерево',()=>{
      selected='t1';view.cx=36;view.cy=39;paint();statusNode.textContent='Старое дерево · t1';
    }));
    if(mode==='rock')panel.append(btn('Найти камень',()=>{
      selected='r1';view.cx=66;view.cy=38;paint();statusNode.textContent='Камень · r1';
    }));
    if(mode==='actor')canvas.focus({preventScroll:true});
  });
}
async function texturedRaster(kind) {
  const names=materials(kind,geography);
  const textures={};
  const ready=await Promise.allSettled(names.map(name=>texture(name)));
  ready.forEach((res,i)=>{if(res.status==='fulfilled')textures[names[i]]=res.value;});
  return {canvas:terrain(geography,kind,Object.keys(textures).length?textures:null),
          missing:names.filter(name=>!textures[name])};
}
function terrainDemo(item,host) {
  register(host,scope=>{
    const canvas=$('canvas',null,'uiux-advanced-canvas');
    canvas.setAttribute('aria-label','Оригинальный raster terrain');
    const select=$('select');
    for(const kind of ['Садовый мир','Ледяная','Пустынная','Лавовая']){
      const opt=$('option',kind);opt.value=kind;select.append(opt);
    }
    const detail=$('small','Загружает оригинальные surface-ground-*.png и строит raster штатным terrain().');
    host.append(group(select),canvas,detail,status(host,'Ожидание рендера…'));
    let raster=null,serial=0;
    const paint=()=>{
      if(!scope.alive()||!raster)return;
      const ratio=Math.min(2,window.devicePixelRatio||1),w=Math.max(260,canvas.clientWidth||350),h=196;
      canvas.width=w*ratio;canvas.height=h*ratio;
      const ctx=canvas.getContext('2d');ctx.setTransform(ratio,0,0,ratio,0,0);
      ctx.imageSmoothingEnabled=true;ctx.drawImage(raster,0,0,w,h);
    };
    const update=async()=>{
      const n=++serial,kind=select.value;
      say(host,'Загрузка текстур и генерация поверхности…');
      try{
        const ready=await texturedRaster(kind);
        if(!scope.alive()||n!==serial){ready.canvas.width=0;return;}
        if(raster)raster.width=raster.height=0;
        raster=ready.canvas;
        say(host,ready.missing.length?'Не найдены материалы: '+ready.missing.join(', '):'Текстуры получены; terrain() выполнен.');
        requestAnimationFrame(paint);
      }catch(error){if(scope.alive())say(host,error.message);}
    };
    select.onchange=update;scope.listen(window,'resize',paint);
    scope.listen(window,'moduleresize',paint);
    scope.cleanup(()=>{serial++;if(raster)raster.width=raster.height=0;});
    update();
  });
}
const routeWorld = {promise:null};
async function galaxy(){
  if(!routeWorld.promise)routeWorld.promise=fetch('/api/galaxy')
    .then(r=>{if(!r.ok)throw Error('demo galaxy unavailable');return r.json();})
    .catch(e=>{routeWorld.promise=null;throw e;});
  return routeWorld.promise;
}
function segmentDistance(p,a,b){
  const vx=b.x-a.x,vy=b.y-a.y,w=(vx*vx+vy*vy)||1;
  const t=Math.max(0,Math.min(1,((p.x-a.x)*vx+(p.y-a.y)*vy)/w));
  const x=a.x+vx*t,y=a.y+vy*t;
  return Math.hypot(p.x-x,p.y-y);
}
function routeDemo(host){
  register(host,scope=>{
    const canvas=$('canvas',null,'uiux-advanced-canvas');
    canvas.setAttribute('aria-label','Попадание по звёздам и коридорам');
    const selected=$('strong','Эдем → Альтаир');
    const info=status(host,'Загружаем тестовый реестр галактики…');
    const controls=group(
      btn('+',()=>{zoom=Math.min(3,zoom*1.3);draw();}),
      btn('−',()=>{zoom=Math.max(.6,zoom/1.3);draw();}),
      btn('Сброс',()=>{zoom=1;draw();})
    );
    host.prepend(canvas,selected,controls);
    let data=null,zoom=1,hover=null,active=1,points=[];
    const draw=()=>{
      if(!scope.alive())return;
      const w=Math.max(260,canvas.clientWidth||340),h=196;
      canvas.width=w;canvas.height=h;
      const ctx=canvas.getContext('2d');
      ctx.fillStyle='#0b1721';ctx.fillRect(0,0,w,h);
      if(!data)return;
      const stars=data.stars.filter(s=>!s.deleted);
      const factor=Math.min(w/900,h/700)*zoom;
      const convert=s=>({x:w/2+s.x*factor,y:h/2+s.y*factor,id:s.id,name:s.name});
      points=stars.map(convert);
      const map=new Map(points.map(p=>[p.id,p]));
      ctx.strokeStyle='#32536a';ctx.lineWidth=1;
      for(const lane of data.lanes){
        const a=map.get(lane.from),b=map.get(lane.to);if(!a||!b)continue;
        ctx.beginPath();ctx.moveTo(a.x,a.y);ctx.lineTo(b.x,b.y);ctx.stroke();
      }
      const first=map.get(1),last=map.get(3);
      if(first&&last){
        ctx.strokeStyle='#efad58';ctx.setLineDash([5,4]);ctx.lineWidth=2;
        ctx.beginPath();ctx.moveTo(first.x,first.y);ctx.lineTo(last.x,last.y);ctx.stroke();ctx.setLineDash([]);
      }
      for(const star of points){
        const chosen=star.id===active||star.id===hover?.id;
        ctx.fillStyle=chosen?'#efad58':'#91c2d0';
        ctx.beginPath();ctx.arc(star.x,star.y,chosen?4.7:2,0,Math.PI*2);ctx.fill();
      }
    };
    const where=e=>{
      const rect=canvas.getBoundingClientRect();
      const pt={x:(e.clientX-rect.left)*canvas.width/rect.width,
                y:(e.clientY-rect.top)*canvas.height/rect.height};
      const star=[...points].sort((a,b)=>Math.hypot(pt.x-a.x,pt.y-a.y)-
          Math.hypot(pt.x-b.x,pt.y-b.y))[0];
      if(star&&Math.hypot(star.x-pt.x,star.y-pt.y)<10)return {type:'star',...star};
      const byid=new Map(points.map(p=>[p.id,p]));
      for(const lane of data?.lanes||[]){
        const a=byid.get(lane.from),b=byid.get(lane.to);
        if(a&&b&&segmentDistance(pt,a,b)<4) return {type:'lane',a,b};
      }
      return null;
    };
    canvas.onpointermove=e=>{
      const hit=where(e);hover=hit?.type==='star'?hit:null;
      canvas.style.cursor=hit?'pointer':'crosshair';
      info.textContent=hit?(hit.type==='star'?'Звезда '+hit.name+' · #'+hit.id:
        'Гиперкоридор #'+hit.a.id+' → #'+hit.b.id):'Выберите объект карты';
      draw();
    };
    canvas.onclick=e=>{
      const hit=where(e);if(!hit)return;
      if(hit.type==='star'){active=hit.id;selected.textContent='Выбрана: '+hit.name+' (#'+hit.id+')';}
      else selected.textContent='Коридор '+hit.a.id+' → '+hit.b.id;
      draw();
    };
    scope.listen(window,'resize',draw);
    scope.listen(window,'moduleresize',draw);
    galaxy().then(value=>{if(!scope.alive())return;data=value;info.textContent='Систем: '+data.stars.length+'. Кликните звезду или коридор.';draw();})
      .catch(error=>{if(scope.alive())info.textContent=error.message;});
    requestAnimationFrame(draw);
  });
}
const contributions=Array.from({length:67},(_,i)=>({
  id:900-i, user:['Stellar_07','Navigator','Vega','Atlas'][i%4],
  amount:3+(i*7)%20, resource:['iron','silicon','credits','water'][i%4],
  date:new Date(Date.UTC(2026,10,3)-i*2400000).toLocaleString('ru-RU')
}));
function cursorDemo(host) {
  register(host,scope=>{
    const listing=$('div',null,'uiux-advanced-stack');
    const filters=group();
    const select=$('select');
    for(const [v,name] of [['all','Все участники'],['mine','Мои вклады']]){
      const opt=$('option',name);opt.value=v;select.append(opt);
    }
    filters.append(select);
    host.append(filters,listing,status(host));
    let cursor=0,filtered=[];
    const appendNext=()=>{
      const page=filtered.slice(cursor,cursor+8);
      if(!page.length)return;
      const table=UI.table(['ID','Вкладчик','Ресурс','Сумма'],page.map(x=>[
        '#'+x.id,x.user,x.resource,x.amount
      ]));
      listing.append(table);
      cursor+=page.length;
      const foot=listing.querySelector('.uiux-cursor-footer');
      foot?.remove();
      const more=cursorMore('Показать ещё · '+cursor+' / '+filtered.length,
        cursor<filtered.length?String(cursor):null,appendNext);
      const bar=$('div',null,'uiux-cursor-footer');bar.append(more);listing.append(bar);
      say(host,'Загружено '+cursor+' из '+filtered.length+' · курсор '+(cursor<filtered.length?cursor:'конец'));
    };
    const reset=()=>{
      filtered=contributions.filter(x=>select.value==='all'||x.user==='Stellar_07');
      cursor=0;listing.replaceChildren();appendNext();
    };
    select.onchange=reset;reset();
  });
}
function cargoDemo(host) {
  register(host,scope=>{
    const model={ship:{iron:22,silicon:8,water:14},
                 warehouse:{iron:2,silicon:19,water:30}};
    const res={iron:{code:'Fe',name:'Железо',kind:'Руда'},
      silicon:{code:'Si',name:'Кремний',kind:'Руда'},
      water:{code:'H₂O',name:'Вода',kind:'Жидкость'}};
    let source=null,dragToken=null,pending=null,amount=1;
    const grid=$('div',null,'uiux-cargo-grid');
    const form=$('div',null,'uiux-cargo-dialog');form.hidden=true;
    host.append(grid,form,status(host,'Перетащите ресурсы из трюма на склад или обратно.'));
    const refresh=()=>{
      grid.replaceChildren();
      for(const location of ['ship','warehouse']){
        const zone=$('section',null,'uiux-cargo-zone');
        zone.dataset.location=location;
        zone.append($('b',location==='ship'?'Трюм корабля':'Склад станции'));
        for(const [resource,count] of Object.entries(model[location])){
          if(!count)continue;
          const tile=resourceTile(res[resource],resource,count,()=>{
            pending={from:location,to:location==='ship'?'warehouse':'ship',resource,count};
            editTransfer();
          });
          tile.draggable=true;
          tile.setAttribute('aria-label',res[resource].name+' '+count+'. Нажмите для переноса.');
          tile.ondragstart=e=>{
            source={from:location,resource,count};
            dragToken='demo-'+resource+'-'+location;
            e.dataTransfer.effectAllowed='move';
            e.dataTransfer.setData('application/x-cosmos-cargo',dragToken);
          };
          tile.ondragend=()=>{dragToken=null;source=null;};
          zone.append(tile);
        }
        zone.ondragover=e=>{if(source&&source.from!==location){
          e.preventDefault();e.dataTransfer.dropEffect='move';zone.classList.add('over');
        }};
        zone.ondragleave=()=>zone.classList.remove('over');
        zone.ondrop=e=>{
          zone.classList.remove('over');
          if(!source||source.from===location)return;
          if(e.dataTransfer.getData('application/x-cosmos-cargo')!==dragToken)return;
          e.preventDefault();pending={...source,to:location};editTransfer();
          source=null;dragToken=null;
        };
        grid.append(zone);
      }
    };
    const editTransfer=()=>{
      if(!pending)return;
      form.replaceChildren();
      const heading=$('strong','Перемещение: '+res[pending.resource].name);
      const q=$('input');q.type='number';q.min=1;q.max=pending.count;q.value=pending.count;
      const apply=btn('Переместить',()=>{
        const n=Number(q.value);
        if(!Number.isInteger(n)||n<1||n>pending.count){
          say(host,'Некорректное количество');return;
        }
        model[pending.from][pending.resource]-=n;
        model[pending.to][pending.resource]+=n;
        say(host,'Перемещено '+n+' '+res[pending.resource].name+' · локальная операция');
        pending=null;form.hidden=true;refresh();
      });
      form.append(heading,label('Количество',q),apply,
                  btn('Отмена',()=>{form.hidden=true;pending=null;}));
      form.hidden=false;
    };
    refresh();
  });
}
function typedDelete(host){
  register(host,scope=>{
    const galaxyName='Астерия';let previewed=false,unblocked=false;
    const pane=$('div',null,'uiux-advanced-stack');
    const name=$('strong','Галактика «'+galaxyName+'»');
    const preview=btn('Предпросмотр удаления',()=>{
      previewed=true;render();
    });
    const free=$('label');
    const checkbox=$('input');checkbox.type='checkbox';
    free.append(checkbox,$('span','Очистить зависимости (тестовый сценарий)'));
    checkbox.onchange=()=>{unblocked=checkbox.checked;render();};
    const field=$('input');field.type='text';field.placeholder='Введите Астерия';field.autocomplete='off';
    field.oninput=()=>render();
    const danger=btn('Удалить галактику',()=>{
      if(!previewed||!unblocked||field.value!==galaxyName)return;
      UI.confirm('Тест удаления','Фактическая галактика не удаляется. Подтвердить демонстрационное действие?',async()=>{
        say(host,'Тестовый сценарий выполнен; данные игры не изменены.');
        previewed=false;field.value='';render();return true;
      });
    },true);
    const body=$('div');
    pane.append(name,preview,body,free,label('Подтверждение названием',field),danger);
    host.append(pane,status(host,'Предпросмотр покажет блокирующие зависимости.'));
    const render=()=>{
      body.replaceChildren();danger.disabled=!(previewed&&unblocked&&field.value===galaxyName);
      if(!previewed)return;
      const blockers=unblocked?[]:[['Корабли и маршруты','2'],['Станции игроков','1']];
      body.append($('p',blockers.length?'Удаление заблокировано':'Зависимостей не обнаружено','uiux-advanced-status'));
      body.append(UI.table(['Причина','Количество'],blockers.length?blockers:[['Нет блокировок','0']]));
    };
    render();
  });
}
function benchmarkDemo(host){
  register(host,scope=>{
    const runs=Array.from({length:8},(_,i)=>({
      id:200+i,scenario:['mixed','quota','stock'][i%3],players:10+i*3,
      status:'complete',requests:1100+i*130,errors:i%4===0?2:0,
      metrics:{rps:60+i*6,p95_ms:22+i*3,p50_ms:10+i,p99_ms:41+i*5}
    }));
    const controls=group();
    const players=$('input');players.type='number';players.min=1;players.max=500;players.value=20;
    const kind=$('select');
    for(const [v,label] of [['mixed','Смешанный'],['quota','Квоты'],['stock','Товары']]){
      const option=$('option',label);option.value=v;kind.append(option);
    }
    const charts=$('div',null,'uiux-advanced-stack');
    const start=btn('Начать тест (локально)',()=>{
      start.disabled=true;say(host,'Демо-бенчмарк выполняется…');
      let ticks=0;
      const ticker=setInterval(()=>{
        ticks++;
        if(ticks<5)return;
        clearInterval(ticker);
        const id=runs.at(-1).id+1;
        runs.push({id,scenario:kind.value,players:Number(players.value),status:'complete',
          requests:1800,errors:0,metrics:{rps:110,p95_ms:23,p50_ms:11,p99_ms:47}});
        start.disabled=false;draw();say(host,'Тестовый запуск #'+id+' завершён.');
      },300);
      scope.cleanup(()=>clearInterval(ticker));
    });
    controls.append(label('Игроки',players),label('Сценарий',kind),start);
    host.append(controls,charts,status(host,'Графики используют тот же класс benchmark-chart-row, что и оригинал.'));
    const draw=()=>{
      charts.replaceChildren();
      for(const key of ['rps','p95_ms','errors']){
        const row=$('div',null,'benchmark-chart-row');
        row.append($('span',key));
        const values=runs.slice(-8).map(run=>key==='errors'?run.errors:run.metrics[key]);
        const max=Math.max(1,...values);
        values.forEach((v,i)=>{
          const bar=$('i');
          bar.style.height=Math.max(2,v/max*65)+'px';
          bar.dataset.helpRaw='#'+runs[runs.length-values.length+i].id+' · '+v;
          bar.title=bar.dataset.helpRaw;
          row.append(bar);
        });
        charts.append(row);
      }
      charts.append(UI.table(['#','Игроки','Сценарий','RPS','p95, ms'],runs.slice(-4)
        .reverse().map(r=>['#'+r.id,r.players,r.scenario,r.metrics.rps,r.metrics.p95_ms])));
    };
    draw();
  });
}
function flagEditorDemo(host){
  register(host,scope=>{
    const pane=$('div',null,'uiux-advanced-stack');
    const flagHost=$('div',null,'flag-picker');
    let symbol='◈',dirty=false;
    const badge=$('div',null,'uiux-flag-large');
    const color=$('input');color.type='color';color.value='#efad58';
    const background=$('input');background.type='color';background.value='#17232d';
    const stripe=$('input');stripe.type='color';stripe.value='#7cc8d2';
    const palette=$('div',null,'flag-grid');
    palette.hidden=true;
    const trigger=btn('◈',()=>{palette.hidden=!palette.hidden;});
    trigger.className='flag-preview';
    for(const icon of [...'◈◆★☀☾⚑⚙✦']){
      const b=btn(icon,()=>{symbol=icon;palette.hidden=true;trigger.textContent=icon;update();});
      palette.append(b);
    }
    flagHost.append(trigger,palette);
    const show=()=>{
      badge.replaceChildren();
      paintFlag(badge,{flag:symbol,tag:'DEMO',color:color.value,
        flag_design:{background:background.value,stripe:stripe.value,symbol:color.value}});
      badge.style.background='linear-gradient(135deg,'+background.value+' 38%,'+
        stripe.value+' 39%,'+stripe.value+' 53%,'+background.value+' 54%)';
    };
    const update=()=>{dirty=true;show();say(host,'Есть несохранённые изменения.');};
    for(const input of [color,background,stripe])input.oninput=update;
    const save=btn('Сохранить локально',()=>{
      dirty=false;show();say(host,'Флаг сохранён в состоянии стенда (без API).');
    });
    pane.append(flagHost,badge,group(label('Цвет',color),label('Фон',background),
      label('Полоса',stripe)),save);
    host.append(pane,status(host,'Проверка конструктора флага с исходными символами и paintFlag().'));
    show();
  });
}
function serviceDemo(host){
  register(host,scope=>{
    let only='all',serial=1;
    const services=[
      {id:'cosmos',name:'Cosmic Service',status:'ready'},
      {id:'planet',name:'Planet Service',status:'unreachable'},
      {id:'database',name:'PostgreSQL (mock)',status:'ready'}
    ];
    const history=[{id:serial++,type:'ready',text:'Тестовое состояние служб создано'}];
    const filters=group();
    const select=$('select');
    for(const [v,text] of [['all','Все события'],['ready','Работает'],['unreachable','Нет связи']]){
      const option=$('option',text);option.value=v;select.append(option);
    }
    select.onchange=()=>{only=select.value;draw();};
    const cards=$('div',null,'service-cards');
    const log=$('div',null,'uiux-advanced-stack');
    const cycle=btn('Смоделировать проверку',()=>{
      const target=services[1];
      target.status=target.status==='ready'?'unreachable':'ready';
      history.unshift({id:serial++,type:target.status,
        text:'Planet Service → '+target.status});
      draw();
    });
    filters.append(select,cycle);
    host.append(filters,cards,log,status(host,'Нет фоновых запросов: каждая проверка запускается вручную.'));
    const draw=()=>{
      cards.replaceChildren();log.replaceChildren();
      for(const service of services){
        const card=$('section',null,'service-card');
        card.dataset.service=service.id;
        const header=$('div',null,'service-card-heading');
        const badge=$('span',service.status==='ready'?'Готова':'Нет связи','service-status-tag');
        badge.dataset.status=service.status;
        badge.setAttribute('role','status');
        header.append($('h4',service.name),badge);
        card.append(header);cards.append(card);
      }
      const rows=history.filter(e=>only==='all'||e.type===only);
      log.append(UI.table(['№','Событие','Статус'],rows.map(x=>['#'+x.id,x.text,x.type])));
    };
    draw();
  });
}
function twoPhaseDemo(host) {
  register(host,scope=>{
    const existing={tick_seconds:10,scan_seconds:15,company_cost:500};
    let applied={...existing},preview=null;
    const form=$('form',null,'uiux-advanced-form');
    const fields={};
    for(const [key,text] of [['tick_seconds','Длительность цикла, с'],
      ['scan_seconds','Сканирование, с'],['company_cost','Стоимость корпорации, CR']]){
      const input=UI.field(form,text,'uiux-advanced-'+key,applied[key],'number');
      input.min='1';input.required=true;fields[key]=input;
    }
    const result=$('div',null,'uiux-advanced-stack');
    const apply=btn('Применить после предпросмотра',()=>{
      if(!preview||!form.reportValidity())return;
      const current=Object.fromEntries(Object.entries(fields).map(([k,el])=>[k,Number(el.value)]));
      if(JSON.stringify(current)!==JSON.stringify(preview)){
        say(host,'Черновик изменился. Повторите предпросмотр.');return;
      }
      UI.confirm('Подтверждение изменения','Сохранить только тестовые настройки?',async()=>{
        applied={...preview};preview=null;apply.disabled=true;
        say(host,'Изменения применены в памяти лаборатории.');
        paint();return true;
      });
    },true);
    const previewButton=btn('Предпросмотр',()=>{
      if(!form.reportValidity()){say(host,'Проверьте значения полей');return;}
      const next=Object.fromEntries(Object.entries(fields).map(([k,el])=>[k,Number(el.value)]));
      if(Object.values(next).some(v=>!Number.isFinite(v)||v<=0)){
        say(host,'Значения должны быть положительными');return;
      }
      preview=next;apply.disabled=false;paint();
    });
    const reset=btn('Отменить черновик',()=>{
      for(const [key,el] of Object.entries(fields))el.value=applied[key];
      preview=null;apply.disabled=true;paint();say(host,'Черновик отменён.');
    });
    const paint=()=>{
      result.replaceChildren();
      if(!preview){result.append($('p','Сначала нажмите «Предпросмотр».'));return;}
      result.append(UI.table(['Параметр','Сейчас','После'],Object.entries(preview).map(([k,v])=>[
        k,applied[k],v
      ])));
    };
    for(const el of Object.values(fields))el.oninput=()=>{
      apply.disabled=true;preview=null;paint();say(host,'Изменено: требуется предпросмотр.');
    };
    form.onsubmit=e=>e.preventDefault();
    form.append(group(previewButton,apply,reset));
    host.append(form,result,status(host,'Правки не покидают лабораторию.'));
    paint();
  });
}
let tooltipProviderRegistered=false;
function resourceTooltipDemo(host) {
  register(host,scope=>{
    if(!tooltipProviderRegistered){
      registerTooltip('[data-uiux-rich-resource]',target=>{
        const value=target.dataset.uiuxRichResource;
        const record={iron:{name:'Железо',kind:'Руда',rarity:'Обычный',base:'5 CR',
          description:'Используется при строительстве кораблей и станций.'},
          silicon:{name:'Кремний',kind:'Руда',rarity:'Редкий',base:'9 CR',
          description:'Производство электроники и сенсоров.'}}[value];
        if(!record)return null;
        const node=$('section',null,'uiux-rich-tooltip');
        node.append($('b',record.name),$('p',record.description));
        for(const [k,v] of [['Тип',record.kind],['Редкость',record.rarity],['Базовая цена',record.base]])
          node.append(group($('span',k),$('strong',v)));
        return node;
      });
      tooltipProviderRegistered=true;
    }
    const row=group();
    for(const id of ['iron','silicon']){
      const b=btn(id==='iron'?'Железо · Fe':'Кремний · Si',()=>say(host,'Выбран '+id));
      b.dataset.uiuxRichResource=id;
      b.setAttribute('data-resource',id);
      row.append(b);
    }
    host.append(row,status(host,'Наведите курсор или сфокусируйте кнопку. Используется исходный tooltip host.'));
  });
}
function linkedWindowsDemo(host){
  register(host,scope=>{
    const ship='uiux-advanced-link-ship',cargo='uiux-advanced-link-cargo';
    let linked=true;
    const open=()=>{
      const a=Modules.create(ship,'UI/UX · Корабль',420,330,{transient:true,private:false});
      a.replaceChildren($('h3','Титанчик'),UI.bar(72,100,'Топливо'));
      const b=Modules.create(cargo,'UI/UX · Трюм',380,350,{transient:true,private:false});
      b.replaceChildren($('h3','Грузы корабля'),UI.table(['Ресурс','Количество'],[
        ['Железо',17],['Вода',24]
      ]));
      Modules.link(ship,cargo,linked);
      Modules.open(ship);Modules.open(cargo);
      Modules.adjacent(cargo,ship,'right');
      say(host,'Два настоящих окна Modules открыты. Перетащите заголовок «Корабль».');
    };
    const toggle=btn('Развязать окна',()=>{
      linked=!linked;Modules.link(ship,cargo,linked);
      toggle.textContent=linked?'Развязать окна':'Связать окна';
      say(host,linked?'Окна связаны':'Окна независимы');
    });
    host.append(group(btn('Открыть два связанных окна',open),toggle),
      status(host,'Используются оригинальные Modules.link() и Modules.adjacent().'));
    scope.cleanup(()=>{
      Modules.link(ship,cargo,false);
      for(const id of [ship,cargo]){Modules.close(id);Modules.destroy(id);}
    });
  });
}
const handlers={
  'ui.map.planet.scene':sceneDemo,
  'ui.map.planet.tree':sceneDemo,
  'ui.map.planet.rock':sceneDemo,
  'ui.map.planet.actor':sceneDemo,
  'ui.map.planet.terrain':terrainDemo,
  'ui.map.route.hit-testing':(_i,h)=>routeDemo(h),
  'ui.table.cursor.history':(_i,h)=>cursorDemo(h),
  'ui.card.cargo-transfer':(_i,h)=>cargoDemo(h),
  'ui.window.danger-typed':(_i,h)=>typedDelete(h),
  'ui.chart.benchmark':(_i,h)=>benchmarkDemo(h),
  'ui.corporation.flag-editor':(_i,h)=>flagEditorDemo(h),
  'ui.admin.service-log':(_i,h)=>serviceDemo(h),
  'ui.form.two-phase':(_i,h)=>twoPhaseDemo(h),
  'ui.tooltip.resource-rich':(_i,h)=>resourceTooltipDemo(h),
  'ui.nav.window-groups':(_i,h)=>linkedWindowsDemo(h),
  'ui.graphics.planet-textures':terrainDemo
};
export const advancedRenderers = Object.freeze(Object.keys(handlers));
export function renderAdvanced(item, host) {
  const renderer=handlers[item.id];
  if(!renderer)return false;
  renderer(item,host);
  return true;
}
