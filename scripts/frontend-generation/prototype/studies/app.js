document.documentElement.classList.toggle('embedded', new URLSearchParams(location.search).has('embed'));

(()=>{'use strict';const $=id=>document.getElementById(id);const names=['Эдем','Альтаир','Веспер','Тау-Кита','Кассиопея','Нимбус','Ариадна','Фомальгаут','Зенит','Арктур','Сириус','Орион','Полярис','Лира','Дельта-7','Прометей','Кеплер','Эридан','Антарес','Кронос','Гелиос','Аврора','Атлас','Персей'];const regions=['Центральное скопление','Внешний рукав','Туманность Ориона','Пояс Эдема','Пограничные миры'];let entries=names.slice(1).map((name,i)=>({star:i+2,name,region:regions[i%regions.length],galaxy:i%7===0?'Пилигрим':'Эдем',class:['O','B','A','F','G','K','M'][i%7],source:i%4===0?'purchased':i%4===1?'received':'survey',received_at:new Date(Date.UTC(2026,9,31-i,12,20)).toISOString()}));let jobs=[{ship:'Атлас-01',star:'Эдем',scanned:7,total:12,seconds:18,paused:false},{ship:'Орион-3',star:'Гелиос',scanned:4,total:16,seconds:29,paused:true}];let page=1,scenario='scanning',maximized=false,pinned=false,callback=null;const sourceName=i=>i.source==='survey'?'Личное исследование':i.source==='purchased'?'Купленные разведданные':'Получено ранее';const escape=s=>String(s).replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));const fmt=t=>new Date(t).toLocaleString('ru-RU',{day:'2-digit',month:'2-digit',year:'numeric',hour:'2-digit',minute:'2-digit'});const bar=(v,t,label)=>`<div class="meter"><span>${label}</span><b>${v} / ${t}</b><progress max="${t}" value="${v}" aria-label="${label}"></progress></div>`;const STAR_HOLOGRAMS = Object.create(null);
const svgReady = Promise.all(['O','B','A','F','G','K','M'].map(async code => {
  const response = await fetch('/object-star-' + code.toLowerCase() + '.svg');
  if (!response.ok) throw Error('Не удалось загрузить игровую голограмму: ' + code);
  STAR_HOLOGRAMS[code] = await response.text();
})).then(() => { if(document.getElementById('studies-current')) update(); });;
const CURRENT_SYSTEM={star:1,name:'Эдем',region:'Центральное скопление',galaxy:'Эдем',class:'G',source:'survey',received_at:'2026-10-31T12:00:00.000Z'};
let selectedStar=1;let systemView=null;let systemViewSerial=0;const resourcesById=new Map();
const findSystem=(star)=>star===CURRENT_SYSTEM.star?CURRENT_SYSTEM:entries.find(x=>x.star===star);
const hologramFor=(letter)=>`<div class="hologram-surface object-hologram" role="img" aria-label="Голограмма звезды класса ${letter}">${STAR_HOLOGRAMS[letter]||STAR_HOLOGRAMS.G}</div>`;

function toast(text){const x=$('toast');x.textContent=text;x.classList.add('show');clearTimeout(toast.t);toast.t=setTimeout(()=>x.classList.remove('show'),2500)}function modal(title,html,onOk,onlyOk=false){$('modal-title').textContent=title;$('modal-body').innerHTML=html;callback=onOk;$('backdrop').classList.add('open');$('modal-cancel').style.display=onlyOk?'none':'';$('modal-ok').textContent=onlyOk?'Закрыть':'Подтвердить'}function currentStateLabel(){
  return scenario==='known'?'Система исследована':scenario==='paused'?'Исследование приостановлено':scenario==='scanning'?'Исследование выполняется':'Система не исследована';
}
function renderStatistics(){
  const knownCurrent=scenario==='known';
  const knownTotal=entries.length+(knownCurrent?1:0);
  const surveyedTotal=entries.filter(x=>x.source==='survey').length+(knownCurrent?1:0);
  const intelTotal=entries.filter(x=>x.source==='purchased').length;
  $('studies-stats').innerHTML=`<div><strong>${knownTotal}</strong><span>Известных систем</span></div><div><strong>${surveyedTotal}</strong><span>Лично исследовано</span></div><div><strong>${intelTotal}</strong><span>Получено из разведданных</span></div>`;
}
function renderDetail(){
  const system=findSystem(selectedStar)||CURRENT_SYSTEM;
  const shipHere=system.star===CURRENT_SYSTEM.star;
  const isKnown=shipHere?scenario==='known':true;
  const ongoing=shipHere&&(scenario==='scanning'||scenario==='paused');
  const status=shipHere?currentStateLabel():'Система исследована';
  let view=`<p class="eyebrow">${shipHere?'Текущая система':'Выбранная система'}</p>`;
  view+=`<div class="study-current-cover">${hologramFor(system.class)}</div>`;
  view+=`<h2>${escape(system.name)}</h2>`;
  view+=`<span class="study-status" data-known="${isKnown}">${escape(status)}</span>`;
  view+=`<p class="muted">${escape(system.region)}</p>`;
  if(!shipHere)view+=`<p class="study-detail-properties"><span>Звезда класса ${escape(system.class)}</span><span>${escape(system.galaxy)}</span></p>`;
  if(isKnown)view+=`<p class="muted">${sourceName(system)}</p>`;
  if(ongoing){
    const active=jobs.find(j=>j.star===CURRENT_SYSTEM.name)||{scanned:0,total:6,seconds:2};
    view+=bar(active.scanned,active.total,'Объекты');
    view+=`<p class="muted">${scenario==='paused'?'Приостановлено':'Сканирование'} · ${active.seconds} s / Объект</p>`;
    view+=`<div class="study-detail-actions"><button class="terminal-action" id="study-action">${scenario==='paused'?'Продолжить':'Приостановить'}</button><button class="terminal-action" id="detail-more">Подробнее</button></div>`;
  }else if(shipHere){
    view+=`<div class="study-detail-actions"><button class="terminal-action" id="study-action" ${isKnown?'disabled title="Система уже исследована"':scenario==='travel'?'disabled title="Корабль в пути"':''}>Исследовать систему · 10 CR</button><button class="terminal-action" id="detail-more">Подробнее</button></div>`;
  }else{
    view+=`<div class="study-detail-actions"><button class="terminal-action" id="detail-map">На карте</button><button class="terminal-action" id="detail-more">Подробнее</button></div>`;
  }
  let allJobs=jobs.filter(j=>j.star!==CURRENT_SYSTEM.name);
  if(scenario==='scanning'||scenario==='paused')allJobs.unshift({...jobs[0],paused:scenario==='paused'});
  view+='<section class="studies-active"><h3>Текущие экспедиции</h3>';
  view+=allJobs.length?allJobs.map(j=>`<div class="study-job"><b>${escape(j.ship)}</b><p>${escape(j.star)}</p>${bar(j.scanned,j.total,'Объекты')}<small>${j.paused?'Приостановлено':'Сканирование'}</small></div>`).join(''):'<p class="empty-state">Нет активных экспедиций</p>';
  view+='</section>';
  const container=$('studies-current');container.innerHTML=view;
  // Original UI.hologram synchronizes its inline SVG animation to the shared page clock.
  const svg=container.querySelector('.hologram-surface svg');
  if(svg)requestAnimationFrame(()=>{if(svg.isConnected){svg.setCurrentTime?.(performance.now()/1000);if(matchMedia('(prefers-reduced-motion: reduce)').matches)svg.pauseAnimations?.();}});
  const action=$('study-action');
  if(action)action.onclick=()=>{
    if(!shipHere)return;
    if(scenario==='new')modal('План экспедиции',`<p>${escape(CURRENT_SYSTEM.name)} · Объекты: 12 / 12<br>Время: 216 s · Топливо: 0<br>Стоимость: 10 CR</p>`,()=>{scenario='scanning';$('scenario').value=scenario;update();toast('Исследование начато · тестовые данные')});
    else if(ongoing){scenario=scenario==='paused'?'scanning':'paused';$('scenario').value=scenario;update();toast(scenario==='paused'?'Исследование приостановлено':'Исследование возобновлено');}
  };
  if($('detail-map'))$('detail-map').onclick=()=>openOnMap(system);
   if($('detail-more'))$('detail-more').onclick=()=>showSystemView(system);
}
function markSelection(){
  for(const row of document.querySelectorAll('.study-entry[data-star]')){
    const chosen=Number(row.dataset.star)===selectedStar;
    row.classList.toggle('study-entry-selected',chosen);
    row.setAttribute('aria-pressed',String(chosen));
  }
}
function openOnMap(system){
  if(window.parent!==window && new URLSearchParams(location.search).has('embed')){
    window.parent.postMessage({type:'astra-journey-demo:show-star',star:system.star},location.origin);
    return;
  }
  modal('Галактическая карта',`<div class="fake-map"><div class="starline"><b>✦ ${escape(system.name)}</b><p>${escape(system.galaxy)} · ${escape(system.region)} · Звезда класса ${escape(system.class)}</p></div><p>В настоящей игре «На карте» открывает модуль галактики с выбранной звездой.</p></div>`,()=>{},true);
}
function showSystemView(system){
  if(!system)return;
  const serial=++systemViewSerial;
  if(systemView){systemView.close();systemView=null;}
  selectedStar=system.star;
  markSelection();
  import('./system-view.js').then(({mountSystemLineup})=>{
    if(serial!==systemViewSerial)return;
    const host=document.querySelector('.studies-content');
    if(!host)return;
    systemView=mountSystemLineup({
      host,
      starId:system.star,
      assetBase:'/',
      fallbackAsset:'/prototype/studies/object-unknown.svg',
      language:()=>document.documentElement.lang,
      resolveResource:(id)=>resourcesById.get(id)||id,
      loadSystem:async(id,signal)=>{
        const response=await fetch('/api/system?id='+encodeURIComponent(id),{signal,cache:'no-store'});
        if(!response.ok)throw Error('HTTP '+response.status);
        const data=await response.json();
        if(!data?.star||data.star.id!==id)throw Error('Неверный ответ реестра');
        return data;
      },
      onBack:()=>{systemView=null;markSelection();}
    });
  }).catch(error=>{if(serial===systemViewSerial)toast('Состав системы недоступен: '+error.message);});
}
function rowHTML(item,isCurrent=false){
  const chosen=item.star===selectedStar;
  const status=isCurrent?currentStateLabel():'Система исследована';
  const tag=isCurrent?`<span class="study-status study-current-status" data-known="${scenario==='known'}">${escape(status)}</span>`:`<span class="study-source" data-source="${item.source}">${sourceName(item)}</span>`;
  const note=isCurrent?'':'<small>'+fmt(item.received_at)+'</small>';
  return `<article class="study-entry ${isCurrent?'study-entry-current':''} ${chosen?'study-entry-selected':''}" data-star="${item.star}" role="button" tabindex="0" aria-pressed="${chosen}" aria-label="${escape(item.name)}${isCurrent?' — система активного корабля':''}. Выбрать для просмотра">
    <span class="study-star">${escape(item.class)}</span>
    <div><div class="study-entry-heading"><strong>${escape(item.name)}</strong><span class="study-region" title="Регион: ${escape(item.region)}">${escape(item.region)}</span>${isCurrent?'<span class="study-ship-badge">⌖ КОРАБЛЬ ЗДЕСЬ</span>':''}</div>${tag}${note}</div>
    <div class="study-entry-actions"><button class="map-btn terminal-action" type="button" data-star="${item.star}" aria-label="На карте: ${escape(item.name)}">На карте</button><button class="detail-more-btn terminal-action" type="button" data-star="${item.star}" aria-label="Подробнее: ${escape(item.name)}">Подробнее</button></div>
  </article>`;
}
function attachRowHandlers(){
  for(const row of document.querySelectorAll('.study-entry[data-star]')){
    const choose=()=>{const next=Number(row.dataset.star);if(!findSystem(next))return;selectedStar=next;markSelection();renderDetail();};
    row.onclick=e=>{if(e.target.closest('button'))return;choose()};
    row.onkeydown=e=>{if(e.target!==row)return;if(e.key==='Enter'||e.key===' '){e.preventDefault();choose()}};
  }
  for(const btn of document.querySelectorAll('.map-btn[data-star]'))btn.onclick=e=>{e.stopPropagation();const system=findSystem(Number(btn.dataset.star));if(system)openOnMap(system)};
  for(const btn of document.querySelectorAll('.detail-more-btn[data-star]'))btn.onclick=e=>{e.stopPropagation();const system=findSystem(Number(btn.dataset.star));if(system)showSystemView(system)};
}
function update(){renderStatistics();renderDetail();list();}
function list(){
  const q=$('studies-search').value.trim().toLocaleLowerCase('ru');
  const scope=$('studies-scope').value;
  const matching=entries.filter(i=>(scope==='all'||(scope==='own'?i.source==='survey':i.source!=='survey'))&&[i.name,i.galaxy,i.region].some(t=>t.toLocaleLowerCase('ru').includes(q)));
  matching.sort((a,b)=>new Date(b.received_at)-new Date(a.received_at)||a.star-b.star);
  const pages=Math.max(1,Math.ceil(matching.length/10));
  page=Math.min(page,pages);
  // The ship's current location is pinned in the catalog regardless of search,
  // source filter or page. It is not falsely counted as a completed survey.
  $('studies-current-pin').innerHTML=rowHTML(CURRENT_SYSTEM,true);
  const subset=matching.slice((page-1)*10,page*10);
  $('studies-entries').innerHTML=matching.length?subset.map(i=>rowHTML(i)).join(''):'<p class="empty-state">Пока нет исследований по выбранным условиям</p>';
  $('pagination').innerHTML=`<button id="prev" aria-label="Предыдущая страница" ${page===1?'disabled':''}>‹</button><span>${page} / ${pages} · ${matching.length}</span><button id="next" aria-label="Следующая страница" ${page===pages?'disabled':''}>›</button>`;
  $('prev').onclick=()=>{page--;list()};
  $('next').onclick=()=>{page++;list()};
  attachRowHandlers();
}
$('studies-search').oninput=()=>{page=1;list()};$('studies-scope').onchange=()=>{page=1;list()};$('scenario').onchange=e=>{scenario=e.target.value;update()};$('modal-cancel').onclick=()=>{$('backdrop').classList.remove('open')};$('modal-ok').onclick=()=>{const f=callback;$('backdrop').classList.remove('open');if(f)f()};$('backdrop').addEventListener('click',e=>{if(e.target===$('backdrop'))$('backdrop').classList.remove('open')});document.addEventListener('keydown',e=>{if(e.key==='Escape')$('backdrop').classList.remove('open')});$('close-btn').onclick=()=>{$('window-studies').style.display='none';toast('Окно исследований закрыто')};$('nav-studies').onclick=$('open-studies-top').onclick=()=>{$('window-studies').style.display='flex'};$('settings-btn').onclick=()=>modal('Настройки демоверсии','<p>Поменять состояние исследования можно в выпадающем списке в верхней части главной страницы.</p><p>Поиск, фильтр и переходы по страницам работают на вымышленных данных.</p>',()=>{},true);$('home-link').onclick=e=>{e.preventDefault();toast('Демонстрация главной страницы')};let old={};$('max-btn').onclick=()=>{if(pinned)return;const w=$('window-studies');if(!maximized){old={left:w.style.left,top:w.style.top,width:w.style.width,height:w.style.height};w.style.left='5px';w.style.top='62px';w.style.width='calc(100vw - 10px)';w.style.height='calc(100dvh - 101px)';}else{Object.assign(w.style,old)}maximized=!maximized;w.classList.toggle('maximized',maximized);$('max-btn').textContent=maximized?'▣':'□';$('max-btn').setAttribute('aria-label',maximized?'Восстановить окно':'Развернуть окно')};$('pin-btn').onclick=()=>{pinned=!pinned;$('window-studies').classList.toggle('pinned',pinned);$('pin-btn').setAttribute('aria-pressed',String(pinned));toast(pinned?'Окно закреплено':'Окно откреплено')};let drag=null;$('drag-bar').addEventListener('dblclick',e=>{if(!e.target.closest('button'))$('max-btn').click()});$('drag-bar').addEventListener('pointerdown',e=>{if(pinned||maximized||e.target.closest('button'))return;const w=$('window-studies');drag={x:e.clientX,y:e.clientY,left:w.offsetLeft,top:w.offsetTop,pid:e.pointerId};$('drag-bar').setPointerCapture(e.pointerId)});$('drag-bar').addEventListener('pointermove',e=>{if(!drag)return;const w=$('window-studies');w.style.left=Math.max(4,Math.min(innerWidth-w.offsetWidth-4,drag.left+e.clientX-drag.x))+'px';w.style.top=Math.max(62,Math.min(innerHeight-w.offsetHeight-36,drag.top+e.clientY-drag.y))+'px'});$('drag-bar').addEventListener('pointerup',()=>drag=null);$('drag-bar').addEventListener('pointercancel',()=>drag=null);const resizeWatcher=new ResizeObserver(()=>{const r=$('window-studies').getBoundingClientRect();$('window-studies').classList.toggle('too-small',r.width<350||r.height<220)});resizeWatcher.observe($('window-studies'));$('drag-bar').addEventListener('keydown',e=>{const delta={ArrowLeft:[-20,0],ArrowRight:[20,0],ArrowUp:[0,-20],ArrowDown:[0,20]}[e.key];if(!delta||pinned||maximized||e.target!==$('drag-bar'))return;e.preventDefault();const w=$('window-studies');const r=w.getBoundingClientRect();w.style.left=Math.max(4,Math.min(innerWidth-r.width-4,r.left+delta[0]))+'px';w.style.top=Math.max(62,Math.min(innerHeight-r.height-36,r.top+delta[1]))+'px'});
// Демо-аналог существующего UI.menu: меню подключается к document.body, не к прокручиваемой панели.
const gear=$('research-gear'),gearPop=$('research-config-pop');
const researchOptions=[['new','Нет активной задачи'],['scanning','Исследование выполняется'],['paused','Приостановлено'],['known','Система исследована'],['travel','Корабль в пути']];
const closeGear=(restoreFocus=false)=>{if(gearPop.hidden)return;gearPop.hidden=true;gear.setAttribute('aria-expanded','false');if(restoreFocus)gear.focus()};
const positionGear=()=>{
 const r=gear.getBoundingClientRect(), w=gearPop.offsetWidth, h=gearPop.offsetHeight;
 const x=Math.max(6,Math.min(r.right-w,innerWidth-w-6));
 const y=r.bottom+4+h<=innerHeight-6?r.bottom+4:Math.max(6,r.top-h-4);
 gearPop.style.left=x+'px';gearPop.style.top=y+'px';
};
const showGear=()=>{
 gearPop.replaceChildren();
 const heading=document.createElement('p');heading.className='context-heading';heading.textContent='ТЕСТОВОЕ СОСТОЯНИЕ';gearPop.append(heading);
 for(const [value,label] of researchOptions){
  const btn=document.createElement('button');btn.type='button';btn.setAttribute('role','menuitemradio');
  const selected=value===scenario;btn.setAttribute('aria-checked',String(selected));btn.textContent=(selected?'✓  ':'   ')+label;
  btn.onclick=()=>{scenario=value;$('scenario').value=value;update();closeGear()};gearPop.append(btn);
 }
 gearPop.hidden=false;gear.setAttribute('aria-expanded','true');positionGear();gearPop.querySelector('button')?.focus();
};
gear.onclick=()=>gearPop.hidden?showGear():closeGear(true);
gearPop.addEventListener('keydown',e=>{
 if(e.key==='Escape'){e.preventDefault();closeGear(true);return}
 if(e.key==='ArrowDown'||e.key==='ArrowUp'){
  e.preventDefault();const items=[...gearPop.querySelectorAll('button')];
  const idx=items.indexOf(document.activeElement),step=e.key==='ArrowDown'?1:-1;
  items[(idx+step+items.length)%items.length]?.focus();
 }
});
document.addEventListener('pointerdown',e=>{if(!gearPop.contains(e.target)&&!gear.contains(e.target))closeGear()});
document.addEventListener('keydown',e=>{if(e.key==='Escape')closeGear(true)});
window.addEventListener('resize',()=>{if(!gearPop.hidden)positionGear()});
$('studies-current').addEventListener('scroll',()=>{if(!gearPop.hidden)closeGear()});

// Источник: компактное контекстное меню (ККМ) по дизайну Chat. Никакого поиска внутри.
// Состояние фильтра остаётся в sourcePicker.value, как и в старой демоверсии.
const sourcePicker=$('studies-scope'),sourcePop=$('studies-scope-pop');
const sourceOptions=[['all','Все исследования'],['own','Личные исследования'],['received','Полученные сведения']];
sourcePicker.value='all';
const sourceLabel=()=>{
 const selected=sourceOptions.find(([value])=>value===sourcePicker.value);
 sourcePicker.querySelector('.picker-label').textContent=selected?.[1]||'Все исследования';
};
const positionSourcePicker=()=>{
 if(sourcePop.hidden)return;
 // Menu is deliberately narrower than the trigger when possible.
 const r=sourcePicker.getBoundingClientRect(),m=sourcePop.getBoundingClientRect();
 const left=Math.max(8,Math.min(r.left,innerWidth-m.width-8));
 const below=r.bottom+3,above=r.top-m.height-3;
 sourcePop.style.left=left+'px';
 sourcePop.style.top=(below+m.height<=innerHeight-8||above<8?below:above)+'px';
};
const closeSourcePicker=(restoreFocus=false)=>{
 if(sourcePop.hidden)return;
 sourcePop.hidden=true;sourcePicker.setAttribute('aria-expanded','false');
 if(restoreFocus)sourcePicker.focus();
};
const chooseSource=value=>{
 sourcePicker.value=value;sourceLabel();closeSourcePicker(true);
 sourcePicker.dispatchEvent(new Event('change',{bubbles:true}));
};
const renderSourcePicker=()=>{
 sourcePop.replaceChildren();
 for(const [value,label] of sourceOptions){
   const b=document.createElement('button'),check=document.createElement('span');
   b.type='button';b.setAttribute('role','menuitemradio');
   b.setAttribute('aria-checked',String(value===sourcePicker.value));
   check.className='kkm-tick';check.setAttribute('aria-hidden','true');
   check.textContent=value===sourcePicker.value?'✓':'';
   b.append(check,document.createTextNode(label));
   b.onclick=()=>chooseSource(value);
   sourcePop.append(b);
 }
};
sourcePicker.onclick=()=>{
 if(!sourcePop.hidden){closeSourcePicker(true);return;}
 closeGear();renderSourcePicker();sourcePop.hidden=false;
 sourcePicker.setAttribute('aria-expanded','true');positionSourcePicker();
 sourcePop.querySelector('button[aria-checked="true"]')?.focus();
};
sourcePop.addEventListener('keydown',e=>{
 if(e.key==='Escape'){e.preventDefault();e.stopPropagation();closeSourcePicker(true);return;}
 if(['ArrowDown','ArrowUp','Home','End'].includes(e.key)){
   e.preventDefault();const buttons=[...sourcePop.querySelectorAll('button')];
   const i=buttons.indexOf(document.activeElement);
   const next=e.key==='Home'?0:e.key==='End'?buttons.length-1:(i+(e.key==='ArrowDown'?1:-1)+buttons.length)%buttons.length;
   buttons[next]?.focus();
 }
});
document.addEventListener('pointerdown',e=>{
 if(!sourcePop.hidden&&!sourcePop.contains(e.target)&&!sourcePicker.contains(e.target))closeSourcePicker();
});
document.addEventListener('keydown',e=>{if(e.key==='Escape'&&!sourcePop.hidden)closeSourcePicker(true)});
window.addEventListener('resize',()=>{if(!sourcePop.hidden)positionSourcePicker()});
$('studies-entries').addEventListener('scroll',()=>{if(!sourcePop.hidden)closeSourcePicker()});
$('window-studies').addEventListener('pointerdown',e=>{
 if(e.target.closest('.module-titlebar')&&!sourcePop.hidden)closeSourcePicker();
});
gear.addEventListener('click',()=>{if(!sourcePop.hidden)closeSourcePicker()},true);
sourceLabel();

update();
// The standalone approved prototype retains its own offline fallback fixtures.
// In the integrated mirror, its data comes from the same registry as the maps.
if(new URLSearchParams(location.search).has('embed')){
  fetch('/api/demo/registry',{cache:'no-store'}).then(response=>{
    if(!response.ok)throw Error('Registry HTTP '+response.status);
    return response.json();
  }).then(registry=>{
    if(!Array.isArray(registry.stars)||!Array.isArray(registry.known)||
       !Array.isArray(registry.regions)||!registry.ship_star)throw Error('Некорректный реестр');
    resourcesById.clear();
    if(Array.isArray(registry.resources))
      for(const resource of registry.resources)resourcesById.set(resource.id,resource.name);
    const regionById=new Map(registry.regions.map(region=>[region.id,region.name]));
    const byId=new Map(registry.stars.map(star=>[star.id,star]));
    const shipStar=byId.get(registry.ship_star);
    if(!shipStar)throw Error('Неизвестная система корабля');
    const adapt=(star,known)=>({star:star.id,name:star.name,
      region:regionById.get(star.region)||'Регион неизвестен',galaxy:'Эдем',
      class:star.class,source:known?.source||'survey',
      received_at:known?.received_at||'2026-11-01T11:30:00Z'});
    const current=adapt(shipStar);
    Object.assign(CURRENT_SYSTEM,current);
    entries=registry.known.filter(item=>item.star!==registry.ship_star)
      .map(item=>{const star=byId.get(item.star);return star?adapt(star,item):null})
      .filter(Boolean);
    if(Array.isArray(registry.research)){
      jobs=registry.research.map(job=>({ship:job.ship===13?'Титанчик':'Вектор-12',
        star:byId.get(job.star)?.name||String(job.star),scanned:job.scanned,
        total:job.total,seconds:job.seconds,paused:job.paused}));
      const currentJob=registry.research.find(job=>job.star===registry.ship_star);
      scenario=currentJob?(currentJob.paused?'paused':'scanning'):
        registry.known.some(item=>item.star===registry.ship_star)?'known':'new';
      $('scenario').value=scenario;
    }
    selectedStar=registry.ship_star;page=1;update();
  }).catch(error=>console.warn('[Исследования] Автономные данные вместо общего реестра:',error));
}
})();
