/* Cosmosimulator UI/UX laboratory. Demo only. No production API mutations. */
import { UI, Modules } from '/ui/runtime.js';
import { pager, cursorMore } from '/ui/pager.js';
import { actionAmount } from '/ui/quantity.js';
import { notify } from '/ui/feedback.js';
import { paintFlag } from '/features/corporations/flag.js';

const LAB_ID = 'uiux-lab';
const $ = (tag, text, className) => {
  const element = document.createElement(tag);
  if (text !== undefined && text !== null) element.textContent = String(text);
  if (className) element.className = className;
  return element;
};
const en = () => language === 'en';
let language = 'ru', registry = null, labWindow = null, currentCategory = 'menus';
let currentSubcategory = '', keyword = '', narrow = false, currentPopup = null;
let popupDisposers = [], infoPopup = null, activeCompare = new Set();
let activeWindows = new Set(), stateById = new Map(), demoWorld = null, worldPending;
let root, categoryList, results, title, search, languageButton, comparePanel, count;

function label(obj) {return obj[en() ? 'name_en' : 'name_ru'] || obj.name_ru;}
function report(text) {
  const indicator = root?.querySelector('.uiux-feedback');
  if (indicator) indicator.textContent = text;
}
function button(text, click, disabled = false) {
  return UI.button(text, click, disabled);
}
function append(parent, ...elements) {
  for (const e of elements) if (e) parent.append(e);
  return parent;
}
function safeID(id) {return id.replace(/[^a-z0-9-]/g, '-');}
function officialUrl(path) {
  return 'https://github.com/yomezza/cosmosimulator/blob/main/' + path;
}
async function copy(text) {
  try {
    if (navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(text);
    } else {
      const source = $('textarea');
      source.value = text;
      source.style.cssText = 'position:fixed;left:-10000px';
      document.body.append(source);
      source.select();
      if (!document.execCommand('copy')) throw new Error('Clipboard unavailable');
      source.remove();
    }
    report((en() ? 'Copied: ' : 'Скопировано: ') + text);
  } catch {
    report(en() ? 'Clipboard unavailable; select the ID to copy.' : 'Буфер недоступен — выделите ID вручную.');
  }
}

function menuCleanup() {
  for (const dispose of popupDisposers.splice(0)) dispose();
  currentPopup?.remove();
  currentPopup = null;
  document.querySelectorAll('.uiux-submenu').forEach(e => e.remove());
  infoPopup?.remove();
  infoPopup = null;
}
function popupWithin(node, x, y) {
  node.style.left = '0px';
  node.style.top = '0px';
  const rect = node.getBoundingClientRect();
  const width = rect.width || 160, height = rect.height || 80;
  node.style.left = Math.max(6, Math.min(window.innerWidth - width - 6, x)) + 'px';
  node.style.top = Math.max(6, Math.min(window.innerHeight - height - 6, y)) + 'px';
}
function showPopup(trigger, menuClass, rows, { nested = false } = {}) {
  menuCleanup();
  const anchor = trigger.getBoundingClientRect();
  const menu = $('div', null, 'uiux-sample-popup ' + menuClass);
  menu.setAttribute('role','menu');
  menu.tabIndex = -1;
  menu.dataset.uiuxPopup = 'true';
  document.body.append(menu);
  currentPopup = menu;
  const entry = (row, host, isSub = false) => {
    if (row.separator) {
      host.append($('hr'));
      return;
    }
    if (row.heading) {
      host.append($('p', row.heading, 'uiux-menu-heading'));
      return;
    }
    const item = $('button', row.label);
    item.type = 'button';
    item.setAttribute('role', 'menuitem');
    item.disabled = !!row.disabled;
    if (row.selected) item.setAttribute('aria-checked','true');
    if (row.danger) item.classList.add('uiux-danger');
    if (row.children) {
      item.classList.add('uiux-submenu-parent');
      item.setAttribute('aria-haspopup', 'menu');
      item.setAttribute('aria-expanded', 'false');
      const showChild = () => {
        document.querySelectorAll('.uiux-submenu').forEach(n => n.remove());
        if (item.disabled) return;
        const sub = $('div', null, 'uiux-sample-popup uiux-submenu chat-context-submenu');
        sub.setAttribute('role','menu');
        row.children.forEach(c => entry(c, sub, true));
        document.body.append(sub);
        item.setAttribute('aria-expanded','true');
        const b = item.getBoundingClientRect();
        popupWithin(sub,b.right-3,b.top);
        sub.addEventListener('pointerenter', () => item.setAttribute('aria-expanded','true'));
      };
      item.addEventListener('pointerenter', showChild);
      item.addEventListener('click', () => {
        showChild();
        document.querySelector('.uiux-submenu button:not(:disabled)')?.focus();
      });
    } else {
      item.addEventListener('pointerenter', () => {
        if (!isSub) document.querySelectorAll('.uiux-submenu').forEach(n => n.remove());
      });
      item.addEventListener('click', () => {
        menuCleanup();
        row.action?.();
        report((en() ? 'Selected: ' : 'Выбрано: ') + row.label);
      });
    }
    host.append(item);
  };
  rows.forEach(r => entry(r, menu));
  popupWithin(menu, anchor.left, anchor.bottom + 4);
  const outside = e => {
    if (currentPopup?.contains(e.target) || e.target.closest('.uiux-submenu') || trigger.contains(e.target)) return;
    menuCleanup();
  };
  const keyboard = e => {
    if (!currentPopup) return;
    if (e.key === 'Escape') {e.preventDefault(); menuCleanup(); trigger.focus(); return;}
    if (!['ArrowDown','ArrowUp','Home','End','ArrowRight','ArrowLeft'].includes(e.key)) return;
    if (e.key === 'ArrowLeft') {
      document.querySelectorAll('.uiux-submenu').forEach(n => n.remove());
      trigger.focus(); e.preventDefault(); return;
    }
    if (e.key === 'ArrowRight') {
      document.activeElement?.matches('.uiux-submenu-parent') && document.activeElement.click();
      return;
    }
    const targetMenu = document.activeElement.closest('.uiux-submenu') || currentPopup;
    const buttons = [...targetMenu.querySelectorAll('button:not([disabled])')];
    if (!buttons.length) return;
    const i = buttons.indexOf(document.activeElement);
    const next = e.key === 'Home' ? 0 : e.key === 'End' ? buttons.length-1 :
      (i + (e.key === 'ArrowDown' ? 1 : -1) + buttons.length) % buttons.length;
    buttons[next].focus();
    e.preventDefault();
  };
  const dismiss = () => menuCleanup();
  document.addEventListener('pointerdown', outside, true);
  document.addEventListener('keydown', keyboard, true);
  window.addEventListener('resize', dismiss);
  document.addEventListener('scroll', dismiss, true);
  popupDisposers.push(
    () => document.removeEventListener('pointerdown', outside, true),
    () => document.removeEventListener('keydown', keyboard, true),
    () => window.removeEventListener('resize', dismiss),
    () => document.removeEventListener('scroll', dismiss, true)
  );
  menu.querySelector('button:not(:disabled)')?.focus();
}
function specimenMenus(item, host) {
  const trigger = button(en() ? 'Open menu' : 'Открыть меню', () => {
    const standard = item.renderer === 'menu-original';
    const studies = item.renderer === 'menu-studies';
    const nested = item.renderer === 'menu-nested' || item.variant === 'nested';
    const rows = [
      { heading: standard ? 'Система · Эдем' : undefined },
      { label: en() ? 'Rename' : 'Переименовать', action: () => {} },
      { label: en() ? 'Move to folder' : 'Переместить в папку', children: nested ? [
        {label:'Маршруты',action:()=>{}},{label:'Полезное',action:()=>{}},{label:'Архив',action:()=>{}}
      ] : null },
      {label: en() ? 'Unavailable' : 'Недоступное действие',disabled:true},
      {separator:true},
      {label: en() ? 'Delete' : 'Удалить',danger:true,action:()=>{}},
    ].filter(row => row.separator || row.heading || row.label);
    if (studies) {
      const options = ['Все исследования','Личные исследования','Полученные сведения'];
      const current = trigger.dataset.choice || options[0];
      showPopup(trigger,'studies-source-kkm',options.map(name => ({
        label: name, selected:name===current, action:() => {
          trigger.dataset.choice = name;
          trigger.textContent = name + ' ▾';
        }
      })));
    } else if (standard) {
      const rect = trigger.getBoundingClientRect();
      const native = [
        {heading: en() ? 'Test menu' : 'Тестовое меню'},
        {label:'На карте',action:()=>report('На карте')},
        ...(item.variant === 'long' ? Array.from({length:14},(_,i)=>({label:'Действие '+(i+1),action:()=>{}})) : []),
        {separator:true},
        {label:'Недоступно',disabled:true,reason:'Только демонстрация'},
        {label:'Переместить',children:[{label:'Маршруты',action:()=>report('Маршруты')},{label:'Полезное',action:()=>report('Полезное')}]},
        {label:'Удалить',action:()=>report('Удалить (демо)')}
      ];
      UI.menu(native,rect.left,rect.bottom+4);
    } else showPopup(trigger,item.renderer==='menu-nested'?'chat-context-menu':'chat-context-menu',
      rows.filter(r=>r.heading||r.separator||r.label));
  });
  host.append(trigger);
  if(item.renderer !== 'menu-original') {
    const note=$('span','21 px · compact','uiux-test-log');
    host.append(note);
  }
}
function specimenButton(item, host, state) {
  const labels = {
    standard:'Действие', compact:'Открыть', primary:'Подтвердить',
    secondary:'Отмена',icon:'✦', 'icon-text':'✦ Корабль', danger:'Удалить',
    confirm:'Подтвердить',cancel:'Отмена',toggle:'Активировать',
    busy:'Запустить',disabled:'Недоступно',refresh:'Обновить',
    copy:'Копировать ID',counter:'Сообщения · 3',loading:'Проверить'
  };
  const variant=item.variant;
  const b=button(en()?'Action':(labels[variant]||'Действие'),()=>{
    if(variant==='toggle') {
      b.setAttribute('aria-pressed', String(b.getAttribute('aria-pressed')!=='true'));
      b.classList.toggle('active',b.getAttribute('aria-pressed')==='true');
    }
    if(variant==='copy') copy(item.id);
    else if(variant==='busy'||variant==='loading') {
      b.disabled=true;b.classList.add('is-busy');b.setAttribute('aria-busy','true');
      setTimeout(()=>{if(b.isConnected){b.disabled=false;b.classList.remove('is-busy');b.removeAttribute('aria-busy');}},650);
    } else report((en()?'Activated: ':'Нажата: ')+label(item));
  },variant==='disabled'||state==='disabled');
  if(variant==='primary')b.classList.add('primary-action');
  if(variant==='danger') b.style.color='#e99690';
  if(variant==='compact'){b.style.minHeight='21px';b.style.padding='2px 5px';b.style.fontSize='10px';}
  if(variant==='icon') b.setAttribute('aria-label',en()?'Icon':'Иконка');
  if(variant==='toggle') b.setAttribute('aria-pressed','false');
  if(state==='selected')b.classList.add('active');
  if(state==='loading') {b.disabled=true;b.classList.add('is-busy');b.setAttribute('aria-busy','true');}
  host.append(b);
}
function specimenPicker(item, host) {
  const lots = Array.from({length:item.variant==='long'?80:7},(_,i) => [
    String(i+1), ['Эдем','Альтаир','Вега-9','Сириус','Арктур','Полярис','Антарес'][i%7]+(i>=7?' · '+(i+1):'')
  ]);
  if(item.renderer==='select'){
    const select=$('select');
    lots.slice(0,7).forEach(([id,title])=>{const option=$('option',title);option.value=id;select.append(option);});
    select.addEventListener('change',()=>report(select.value));
    host.append(select);return;
  }
  if(item.renderer==='dropdown'){
    const details=$('details',null,'dropdown'); const summary=$('summary',en()?'Open profile menu':'Меню профиля');
    const content=$('div',null,'uiux-mini-card');
    content.append(button('Профиль',()=>report('Профиль')),button('Настройки',()=>report('Настройки')));
    details.append(summary,content);host.append(details);return;
  }
  if(item.renderer==='choice'){
    UI.choice(host,'Источник',[['all','Все исследования'],['own','Личные исследования'],['received','Полученные сведения']],'all',
      v=>report('Источник: '+v));
    return;
  }
  const favorites = new Set(['1','3']);
  const options=lots.map(([id,title])=>({id,label:title,code:id,
    extra:favorites.has(id)?'★':''}));
  const config={
    system:item.variant==='system'||item.variant==='favorite',
    pageSize:item.variant==='long'?10:7
  };
  if(item.variant==='favorite')config.favorites={
    has:id=>favorites.has(String(id)),
    toggle:id=>favorites.has(String(id))?favorites.delete(String(id)):favorites.add(String(id))
  };
  const picker=UI.picker(options,'1',v=>report('Выбрано: '+v),config);
  picker.style.maxWidth='280px';
  host.append(picker);
}
function specimenField(item,host,state) {
  const variant=item.variant;
  const wrap=$('div',null,'uiux-column');
  const result=$('div','', 'uiux-result');
  if(item.renderer==='textarea') {
    const input=$('textarea');input.placeholder='Введите сообщение…';
    input.maxLength=500;input.value=variant==='composer'?'Привет, экипаж! 🚀':'Заметка о маршруте';
    input.oninput=()=>result.textContent=String([...input.value].length)+' / '+input.maxLength;
    append(wrap,input,result);
  } else if(['checkbox','radio','range','color'].includes(item.renderer)) {
    if(item.renderer==='checkbox'||item.renderer==='radio'){
      const controls=$('div',null,'uiux-row');
      for(const text of ['Планеты','Корабли','Маршруты']){
        const l=$('label');const input=$('input');input.type=item.renderer;input.name=item.id;input.checked=text==='Планеты';
        input.onchange=()=>result.textContent=text+(input.checked?' ✓':'');
        l.append(input,$('span',text));controls.append(l);
      }
      append(wrap,controls,result);
    } else {
      const input=$('input');input.type=item.renderer==='range'?'range':'color';
      if(input.type==='range'){input.min=40;input.max=100;input.value=80;}
      else input.value='#efad58';
      const output=$('span',input.value);
      input.oninput=()=>output.textContent=input.value;
      append(wrap,input,output);
    }
  } else if(item.renderer==='validation') {
    const f=$('form');
    const input=$('input');input.required=true;input.minLength=3;input.placeholder=en()?'Min. 3 characters':'Минимум 3 символа';
    const submit=button(en()?'Validate':'Проверить',()=>{result.textContent=input.checkValidity()?'✓ '+input.value:input.validationMessage;});
    f.onsubmit=e=>e.preventDefault();append(f,input,submit,result);wrap.append(f);
  } else if(item.renderer==='dirty') {
    const f=$('form');
    const field=UI.field(f,'Имя корабля','uiux-dirty-'+safeID(item.id),'Вектор');
    UI.dirty(f,()=>{report('Сохранено локально');return true;},()=>{field.value='Вектор';report('Изменение отменено')});
    wrap.append(f);
  } else {
    let type=variant==='password'?'password':variant==='search'?'search':variant==='number'||variant==='quantity'?'number':'text';
    const input=UI.field(wrap,en()?'Value':'Значение','uiux-'+safeID(item.id),'',type,
      en()?'Demo value':'Тестовое значение');
    if(type==='number'){input.min=1;input.max=variant==='quantity'?100:999;input.value=12;}
    else input.placeholder=variant==='search'?'Поиск систем…':'Введите текст';
    if(type==='search')input.style.borderRadius='0';
    if(state==='disabled') input.disabled=true;
    input.oninput=()=>result.textContent=input.value;
    wrap.append(result);
  }
  host.append(wrap);
}
function specimenWindow(item,host) {
  const open = () => {
    const id='uiux-specimen-'+safeID(item.id);
    activeWindows.add(id);
    const content=Modules.create(id, 'UI/UX · '+item.name_ru, 560, item.variant==='scroll'?530:360,
      {transient:true,private:false});
    content.replaceChildren($('h3',item.name_ru),$('p','Тестовое окно Cosmosimulator. Перетаскивайте заголовок, изменяйте размер, закрепляйте и разворачивайте окно.'));
    if(item.variant==='scroll')for(let i=0;i<18;i++)content.append($('p','Строка '+(i+1)+' · проверка прокрутки'));
    content.append(UI.bar(7,12,'Прогресс'));
    Modules.open(id);
  };
  host.append(button(en()?'Open original window':'Открыть штатное окно',open));
  const row=$('p','Перемещение · resize · pin · maximize · close','uiux-test-log');
  host.append(row);
}
function specimenTabs(item,host) {
  const panel=$('section',null,'uiux-column');
  const tabs=['Избранное','Global','Planet','Sector','Corporation','Private'];
  const compact=item.renderer==='chat-tabs';
  if(compact) {
    const bar=$('div',null,'uiux-tabs');
    for(const name of tabs){
      const b=$('button',name);
      b.setAttribute('role','tab');b.setAttribute('aria-selected',String(name==='Global'));
      b.onclick=()=>{
        [...bar.children].forEach(child=>child.setAttribute('aria-selected',String(child===b)));
        status.textContent=name;
      };
      bar.append(b);
    }
    const status=$('div','Global','uiux-result');
    host.append(bar,status);
  } else {
    const options=(item.variant==='overflow'?tabs:tabs.slice(0,3)).map((name,i)=>[String(i),name]);
    let selected='0';
    const draw=()=>{
      panel.replaceChildren();
      UI.tabs(panel,options,selected,next=>{selected=next;draw();report(options.find(e=>e[0]===next)?.[1]);});
      panel.append($('p','Раздел: '+options.find(e=>e[0]===selected)[1],'uiux-test-log'));
    };
    draw();host.append(panel);
  }
}
function specimenTable(item,host) {
  const data=[
    ['Железо','180','8 CR'],
    ['Кремний','72','12 CR'],
    ['Вода','340','5 CR'],
    ['Биомасса','15','16 CR']
  ];
  const rows=item.variant==='empty'?[]:data.map((row,i)=>item.variant==='actions'?
    [row[0],row[1],button('Купить',()=>report(row[0]+' × 1'))]:item.variant==='long'?
    ['Ресурсный лот: '+row[0]+' · Навигационный маяк Центрального скопления',...row.slice(1)]:row);
  const table=UI.table(item.variant==='actions'?['Ресурс','Остаток','Действие']:['Ресурс','Остаток','Цена'],rows);
  host.append(table);if(!rows.length) UI.empty(host,'Нет записей');
}
function specimenPager(item,host) {
  let page=1;
  const area=$('div',null,'uiux-column');
  const draw=()=>{
    area.replaceChildren();
    const label=$('p','Тестовые записи '+((page-1)*10+1)+'–'+(page*10),'uiux-test-log');
    area.append(label,pager({page,pages:5,total:48,change:value=>{page=value;draw();}}));
  };
  draw();host.append(area);
}
function specimenTree(item,host) {
  const outer=UI.fold('uiux:'+item.id,true);
  outer.append($('summary','Скопление Эдема · 4'));
  for(const name of ['Эдем','Альтаир','Вега-9','Арктур']){
    const inner=UI.fold('uiux:'+item.id+':'+name);
    inner.append($('summary',name),$('p','Планеты · Станции · Месторождения'));
    outer.append(inner);
  }
  host.append(outer);
}
function specimenStatus(item,host) {
  const variant=item.variant;
  const box=$('div',null,'uiux-mini-card');
  const p=$('p');
  if(variant==='loading'){
    p.textContent='◌ Подключение к серверу…';
  } else if(variant==='badge'){
    p.textContent='Уведомления: 3 · Онлайн: 17';
  } else if(variant==='error'){
    p.textContent='× Служба недоступна';p.style.color='#e9978e';
  } else if(variant==='warning'){
    p.textContent='! Ожидание ответа';p.style.color='#eec182';
  } else if(variant==='success'){
    p.textContent='✓ Действие выполнено';p.style.color='#9dd5af';
  } else if(variant==='connection'||variant==='server'){
    p.textContent='● Cosmic: подключено  ·  Planet: доступно';
    p.style.color='#9acdae';
  } else if(variant==='job'){
    p.textContent='Сканирование: 5 / 6';
  } else p.textContent=label(item);
  box.append(p);host.append(box);
}
function specimenCard(item,host){
  const card=$('div',null,'uiux-mini-card');
  const kind=item.variant;
  const texts={
    info:['Текущая система','Эдем · сектор 12'],
    ship:['Титанчик','Пилигрим · 74 / 100 топлива'],
    resource:['Железо','17 ед. · трюм Титанчика'],
    station:['Торговый маяк','Эдем · объект #104'],
    corp:['Союз Ориона','[ORION] · 3 участника'],
    object:['Эдем II','Садовый мир · орбита 2'],
    task:['Изучить сектор','Выполнено 5 из 6'],
    ships:['Титанчик','Вектор-12 · ожидание'],
    stations:['Торговый маяк','Маяк Альтаир'],
    members:['Vega · Navigator','Atlas · Pilot'],
    moderation:['Модерация NOVA','Жалобы · 2 | Ограничения · 0'],
    selection:['Система Эдем','Выбрано · нажмите для смены']
  };
  const data=texts[kind]||[label(item),'Демонстрационные данные'];
  const title=$('strong',data[0]);const details=$('p',data[1]);
  card.append(title,details);
  card.append(button(en()?'Select':'Выбрать',()=>{
    card.classList.toggle('selected');
    card.style.borderColor=card.classList.contains('selected')?'#efad58':'#345160';
    report('Выбран: '+data[0]);
  }));
  host.append(card);
}
function specimenStats(host) {
  const metrics=[['Известные системы','48'],['Корабли','2'],['CR','185 240']];
  const holder=$('div',null,'uiux-row');
  metrics.forEach(([key,value])=>{
    const card=$('div',null,'uiux-mini-card');append(card,$('b',value),$('p',key));holder.append(card);
  });host.append(holder);
}
function specimenEmpty(host) {UI.empty(host,'Нет активных задач');}
function specimenProgress(item,host){
  let n=item.variant==='scan'?5:7;
  const holder=$('div',null,'uiux-column');
  const render=()=>{
    holder.replaceChildren();
    holder.append(UI.bar(n,item.variant==='scan'?6:12,'Объекты'));
    holder.append(button('+1',()=>{n=Math.min(n+1,item.variant==='scan'?6:12);render();}));
  };
  render();host.append(holder);
}
function specimenTooltip(item,host){
  const trigger=button('Наведите курсор',()=>report('Подсказка показана'));
  trigger.dataset.help=item.variant==='rich'?
    'Железо · Руда · 17 единиц. Тестовые данные.':'Подсказка original UI.tooltip из data-help';
  trigger.setAttribute('aria-label','Тестовая подсказка');
  host.append(trigger);
}
function specimenToast(host){
  host.append(button('Показать уведомление',()=>notify('Тест UI/UX: действие выполнено','success',false)));
}
async function loadDemoWorld(){
  if(demoWorld)return demoWorld;
  if(!worldPending) worldPending=Promise.all([
    fetch('/api/galaxy').then(r=>r.ok?r.json():null),
    fetch('/api/system?id=1').then(r=>r.ok?r.json():null)
  ]).then(([galaxy,system])=>(demoWorld={galaxy,system})).catch(()=>{
    worldPending=null;return {galaxy:null,system:null};
  });
  return worldPending;
}
function drawCanvas(canvas,variant,selected=1) {
  const ctx=canvas.getContext('2d');
  if(!ctx)return;
  const scale=window.devicePixelRatio||1;
  const w=Math.max(300,canvas.clientWidth||350),h=150;
  canvas.width=Math.round(w*scale);canvas.height=Math.round(h*scale);
  ctx.setTransform(scale,0,0,scale,0,0);
  ctx.fillStyle='#0b151e';ctx.fillRect(0,0,w,h);
  const stars=demoWorld?.galaxy?.stars||[];
  const system=demoWorld?.system;
  if(variant==='system'&&system?.sites?.length){
    const site=system.sites;
    ctx.fillStyle='#ffdb78';ctx.shadowColor='#ffdb78';ctx.shadowBlur=9;
    ctx.beginPath();ctx.arc(w/2,h/2,8,0,Math.PI*2);ctx.fill();ctx.shadowBlur=0;
    const max=Math.max(1,site.length);
    for(let i=0;i<max;i++){
      const r=24+(i+1)*Math.min(12,Math.floor(h/(2*max)));
      const angle=i*2.4;
      const x=w/2+Math.cos(angle)*r*1.8,y=h/2+Math.sin(angle)*r;
      ctx.strokeStyle='#354957';ctx.beginPath();ctx.ellipse(w/2,h/2,r*1.8,r,0,0,Math.PI*2);ctx.stroke();
      ctx.fillStyle=site[i].kind==='Орбитальная станция'?'#87bac6':'#acb7c1';
      ctx.beginPath();ctx.arc(x,y,4,0,Math.PI*2);ctx.fill();
      ctx.font='9px monospace';ctx.fillText(site[i].name,x+6,y-5);
    }
  } else {
    if(!stars.length){
      ctx.fillStyle='#92a6b6';ctx.fillText('Загрузка тестового реестра…',12,24);return;
    }
    const sx=x=>w/2+x*.29,sy=y=>h/2+y*.17;
    ctx.strokeStyle='#33495e';ctx.lineWidth=.7;
    for(const lane of demoWorld.galaxy.lanes||[]){
      const a=stars[lane.from-1],b=stars[lane.to-1];
      if(a&&b){ctx.beginPath();ctx.moveTo(sx(a.x),sy(a.y));ctx.lineTo(sx(b.x),sy(b.y));ctx.stroke();}
    }
    for(const star of stars){
      const x=sx(star.x),y=sy(star.y);
      if(x<4||x>w-4||y<4||y>h-4)continue;
      ctx.fillStyle=star.id===selected?'#efad58':'#91b5c9';
      ctx.beginPath();ctx.arc(x,y,star.id===selected?4:2,0,Math.PI*2);ctx.fill();
    }
    const selectedStar=stars[selected-1]||stars[0];
    ctx.fillStyle='#efad58';ctx.font='10px monospace';
    ctx.fillText(selectedStar.name+' · '+selectedStar.class,8,14);
  }
}
function specimenMap(item,host) {
  if(item.variant==='filters'){
    const row=$('div',null,'uiux-row');
    for(const text of ['Звёзды','Корабли','Маршруты']){
      const l=$('label');const i=$('input');i.type='checkbox';i.checked=true;i.onchange=()=>report(text+(i.checked?' ✓':''));l.append(i,$('span',text));row.append(l);
    }host.append(row);return;
  }
  const wrapper=$('div',null,'uiux-column');
  const canvas=$('canvas',null,'uiux-preview-map');
  canvas.setAttribute('aria-label',item.variant==='system'?'Схема системы':'Схема галактики');
  const toolbar=$('div',null,'uiux-row');
  const output=$('span','','uiux-result');
  const original=button(en()?'Open game map':'Открыть оригинальную карту',()=>{
    Modules.open('galaxy');
    window.dispatchEvent(new CustomEvent('showstar',{detail:1}));
  });
  const draw=()=>{if(canvas.isConnected)drawCanvas(canvas,item.variant,1);};
  canvas.addEventListener('click',e=>{
    output.textContent='Эдем · #1';draw();
  });
  toolbar.append(original,output);
  wrapper.append(canvas,toolbar);host.append(wrapper);
  requestAnimationFrame(draw);
  loadDemoWorld().then(draw);
}
function specimenChatMessage(item,host){
  const msg=$('div',null,'uiux-chat-message');
  append(msg,$('strong',item.variant==='reply'?'Navigator':'Stellar_07'),
    $('time','14:32'),$('p',item.variant==='reply'?'Подтверждаю маршрут на Альтаир.':'Встречаемся на орбите Эдема 🚀'));
  if(item.variant==='reply')msg.append($('blockquote','Stellar_07: Проверяем координаты.'));
  msg.append(button('⋯',()=>{
    const anchor=msg.querySelector('button');
    showPopup(anchor,'chat-context-menu',[
      {label:'Ответить',action:()=>report('Ответ на сообщение')},
      {label:'В избранное',action:()=>report('Сохранено в демо')},
      {separator:true},{label:'Удалить',danger:true,action:()=>report('Удалено в демо')}
    ]);
  }));
  host.append(msg);
}
function specimenChatFavorite(host){
  const area=$('div',null,'uiux-column');
  ['Все','Маршруты','Полезное'].forEach(folder=>{
    area.append(button('▸ '+folder,()=>report('Папка: '+folder)));
  });host.append(area);
}
function specimenEmoji(host){
  const panel=$('div',null,'uiux-column');
  const text=$('textarea');text.placeholder='Напишите сообщение';
  const emojis=$('div',null,'uiux-row');
  ['🚀','🪐','😀','✨','👍','🌌','💎','🔥'].forEach(emoji=>{
    const b=button(emoji,()=>{text.value+=emoji;text.focus();});
    emojis.append(b);
  });
  panel.append(text,emojis);host.append(panel);
}
function specimenStudies(item,host) {
  if(item.variant==='row'||item.variant==='current'){
    const list=$('div',null,'uiux-star-list');
    ['Эдем','Альтаир','Вега-9'].forEach((name,i)=>{
      const row=$('button',name+(i===0?' · ⌖ Корабль здесь':''));
      row.className='uiux-star-row'+(i===0?' current':'');
      row.onclick=()=>{[...list.children].forEach(el=>el.classList.remove('selected'));row.classList.add('selected');report('Выбрана система: '+name);};
      list.append(row);
    });
    host.append(list);
  } else specimenMap({variant:'system'},host);
}
function specimenDrag(host){
  const item=$('div','Железо · ×17','uiux-drag-item');
  item.draggable=true;
  const target=$('div','Склад станции','uiux-dropzone');
  item.addEventListener('dragstart',e=>{e.dataTransfer.setData('text/plain','iron:17');});
  target.addEventListener('dragover',e=>{e.preventDefault();target.classList.add('over');});
  target.addEventListener('dragleave',()=>target.classList.remove('over'));
  target.addEventListener('drop',e=>{e.preventDefault();target.classList.remove('over');target.textContent='Принято: '+e.dataTransfer.getData('text/plain');});
  append(host,item,target);
}
function specimenFlag(host) {
  const wrapper=$('div',null,'uiux-column');
  const badge=$('div',null,'uiux-mini-card');
  const color=$('input');color.type='color';color.value='#7ad4cb';
  const draw=()=>paintFlag(badge,{flag:'✦',tag:'ORION',color:color.value,flag_design:{background:color.value,stripe:'#172935'}});
  color.oninput=draw;draw();
  wrapper.append(badge,color);host.append(wrapper);
}
function specimenPending(item,host){
  const p=$('p',en()?'Discovered in source; live preview is not ready.':'Найдено в исходниках, но живой стенд пока не готов.','uiux-test-log');
  host.append(p);
}
function renderDemo(item,host,options={}) {
  const state=stateById.get(item.id)||'default';
  const render=item.renderer;
  if(render==='button')specimenButton(item,host,state);
  else if(render.startsWith('menu-'))specimenMenus(item,host);
  else if(['picker','choice','select','dropdown'].includes(render))specimenPicker(item,host);
  else if(['field','textarea','checkbox','radio','range','color','validation','dirty'].includes(render))specimenField(item,host,state);
  else if(render==='window')specimenWindow(item,host);
  else if(render==='chat-window') {
    host.append(button('Открыть чат v13',()=>document.querySelector('[data-chat-open]')?.click()));
  }
  else if(render==='confirm')host.append(button('Открыть UI.confirm',()=>UI.confirm('Подтверждение','Только тестовая операция, данных игры не меняет.',async()=>{report('Подтверждено');return true;})));
  else if(render==='quantity')host.append(button('Диалог количества',()=>actionAmount('Количество','Ресурс',100,async n=>report('Количество: '+n),5)));
  else if(render==='tabs'||render==='chat-tabs')specimenTabs(item,host);
  else if(render==='fold'||render==='tree')specimenTree(item,host);
  else if(render==='sidebar')host.append(button('Показать левое меню',()=>{document.querySelector('#sidebar')?.scrollIntoView({behavior:'smooth'});}));
  else if(render==='table')specimenTable(item,host);
  else if(render==='pager')specimenPager(item,host);
  else if(render==='cursor') {
    const b=cursorMore('Показать ещё','cursor-1',()=>{report('Загружены дополнительные записи');});
    host.append(b);
  }
  else if(render==='cards'||render==='card')specimenCard(item,host);
  else if(render==='stats')specimenStats(host);
  else if(render==='empty')specimenEmpty(host);
  else if(render==='progress')specimenProgress(item,host);
  else if(render==='status')specimenStatus(item,host);
  else if(render==='tooltip')specimenTooltip(item,host);
  else if(render==='toast')specimenToast(host);
  else if(render==='map')specimenMap(item,host);
  else if(render==='map-filter')specimenMap({variant:'filters'},host);
  else if(render==='chat-message')specimenChatMessage(item,host);
  else if(render==='chat-favorite')specimenChatFavorite(host);
  else if(render==='emoji')specimenEmoji(host);
  else if(render==='studies-row')specimenStudies(item,host);
  else if(render==='hologram'||render==='ship') {
    const model=render==='ship'?'pilgrim.svg':item.variant==='planet'?'object-garden.svg':
      item.variant==='station'?'object-station.svg':item.variant==='asteroids'?'object-asteroids.svg':'object-star-g.svg';
    host.append(UI.hologram(model,item.name_ru,'uiux-holo'));
  }
  else if(render==='drag')specimenDrag(host);
  else if(render==='flag')specimenFlag(host);
  else specimenPending(item,host);
  if(options.narrow)host.classList.add('uiux-narrow');
}
function showInfoPopover(item,anchor) {
  infoPopup?.remove();
  const pop=$('div',null,'uiux-info-pop');
  pop.append($('h4',item.id));
  for(const [k,v] of [['Русский',item.name_ru],['English',item.name_en],['Сокр. RU/EN',item.short_ru+' / '+item.short_en],['Статус',item.status],['Тип',item.type],['Модуль',item.uses.join(', ')||'—']])
    pop.append($('div',k+': '+v));
  pop.append(button('Копировать ID',()=>copy(item.id)));
  document.body.append(pop);
  const r=anchor.getBoundingClientRect();
  popupWithin(pop,r.left,r.bottom+5);
  infoPopup=pop;
  pop.onpointerleave=()=>{if(infoPopup===pop){infoPopup=null;pop.remove();}};
}
function card(item,comparison=false) {
  const article=$('article',null,'uiux-card');
  article.dataset.uiId=item.id;
  const heading=$('div',null,'uiux-card-top');
  heading.append($('h4',label(item)));
  const badge=$('span',item.status==='original'?'ORIGINAL':item.status==='approved'?'v13 APPROVED':item.status==='pending'?'NOT REPRODUCED':'DEMO ADAPTER','uiux-status');
  badge.dataset.status=item.status;heading.append(badge);
  const ids=$('div',null,'uiux-card-id');
  const code=$('code',item.id,'uiux-id');code.tabIndex=0;
  code.setAttribute('aria-label','Информация об идентификаторе '+item.id);
  code.onmouseenter=()=>showInfoPopover(item,code);
  code.onfocus=()=>showInfoPopover(item,code);
  ids.append(code,$('span',item.short_ru+' / '+item.short_en,'uiux-test-log'));
  const preview=$('div',null,'uiux-card-demo');
  renderDemo(item,preview,{narrow});
  const meta=$('div',null,'uiux-card-detail');
  meta.append($('p',en()?item.description_en:item.description_ru));
  const dl=$('dl');
  const detail=(k,value)=>{dl.append($('dt',k),$('dd',value));};
  detail(en()?'Variant':'Вариант',item.variant);
  detail(en()?'States':'Состояния',item.states.join(', '));
  detail(en()?'Usage':'Модуль',item.uses.join(' / ')||'—');
  const files=$('div');
  item.source.forEach((file,i)=>{
    const link=$('a',file.replace(/^internal\/server\/web\//,'').replace(/^scripts\/frontend-generation\//,'demo/'));
    link.href=officialUrl(file);link.target='_blank';link.rel='noopener noreferrer';
    files.append(link);
    if(i<item.source.length-1)files.append($('span',' · '));
  });
  detail(en()?'Source':'Исходники',files);
  meta.append(dl);
  const foot=$('div',null,'uiux-card-footer');
  const copyID=button(en()?'Copy ID':'Копировать ID',()=>copy(item.id));
  const toggle=$('label');
  const check=$('input');check.type='checkbox';check.checked=activeCompare.has(item.id);
  check.onchange=()=>{
    if(check.checked&&activeCompare.size>=4) {
      check.checked=false;report('Сравнение: максимум четыре компонента');return;
    }
    check.checked?activeCompare.add(item.id):activeCompare.delete(item.id);
    showCompare();
  };
  toggle.append(check,$('span',en()?'Compare':'Сравнить'));
  foot.append(copyID,toggle);
  if(item.states.length) {
    const selector=$('select');selector.setAttribute('aria-label','Тестовое состояние '+item.id);
    for(const state of item.states){
      const option=$('option',state);option.value=state;selector.append(option);
    }
    selector.value=stateById.get(item.id)||item.states[0];
    selector.onchange=()=>{
      stateById.set(item.id,selector.value);
      preview.replaceChildren();renderDemo(item,preview,{narrow});
    };
    foot.append(selector);
  }
  article.append(heading,ids,preview,meta,foot);
  return article;
}
function metricsFor(item){
  const test=$('div',null, item.renderer==='menu-original'?'context-menu':item.renderer==='menu-studies'?'uiux-sample-popup studies-source-kkm':
    item.renderer.startsWith('menu-')?'uiux-sample-popup chat-context-menu':item.renderer==='button'?'terminal-action':'uiux-card');
  test.style.cssText='position:fixed;left:-10000px;top:0;visibility:hidden';
  document.body.append(test);
  const css=getComputedStyle(test);
  const result=[
    ['Font',css.fontSize],
    ['Padding',css.padding],
    ['Border',css.borderWidth],
    ['Radius',css.borderRadius],
    ['Background',css.backgroundColor],
  ];
  test.remove();
  return result;
}
function showCompare() {
  comparePanel.replaceChildren();
  const selected=[...activeCompare].map(id=>registry.components.find(v=>v.id===id)).filter(Boolean);
  if(!selected.length){comparePanel.hidden=true;return;}
  comparePanel.hidden=false;
  comparePanel.append($('h4',(en()?'Component comparison':'Сравнение компонентов')+' · '+selected.length));
  const grid=$('div',null,'uiux-compare-grid');
  selected.forEach(item=>{
    const wrapper=$('div',null,'uiux-mini-card');
    wrapper.append($('strong',label(item)), $('div',item.id,'uiux-component-source'));
    const demo=$('div',null,'uiux-card-demo');
    renderDemo(item,demo,{narrow});wrapper.append(demo);
    const metrics=$('div',null,'uiux-metrics');
    for(const [k,v] of metricsFor(item))metrics.append($('b',k),$('span',v));
    wrapper.append(metrics,$('p',item.source.join(' · '),'uiux-component-source'));
    grid.append(wrapper);
  });
  comparePanel.append(grid);
  comparePanel.append(button(en()?'Clear comparison':'Сбросить сравнение',()=>{
    activeCompare.clear();showCompare();refreshItems();
  }));
}
function categoryListRender() {
  categoryList.replaceChildren();
  const q=root.querySelector('#uiux-category-search').value.trim().toLocaleLowerCase('ru');
  const all=button(en()?'All · '+registry.components.length:'Все · '+registry.components.length,()=>{
    currentCategory='all';currentSubcategory='';categoryListRender();refreshItems();
  });
  all.className='uiux-category-main';all.setAttribute('aria-current',String(currentCategory==='all'));
  categoryList.append(all);
  for(const category of registry.categories){
    const items=registry.components.filter(c=>c.category===category.id);
    if(q && !(category.name_ru+' '+category.name_en+' '+items.map(i=>i.name_ru+' '+i.name_en+' '+i.id).join(' ')).toLocaleLowerCase('ru').includes(q))continue;
    const group=$('section',null,'uiux-category-group');
    const main=button((en()?category.name_en:category.name_ru),()=>{
      currentCategory=category.id;currentSubcategory='';
      categoryListRender();refreshItems();
    });
    main.className='uiux-category-main';
    main.setAttribute('aria-current',String(currentCategory===category.id));
    const label=$('span',en()?category.name_en:category.name_ru,'uiux-category-text');
    const num=$('span',items.length,'uiux-count');
    main.replaceChildren(label,num);group.append(main);
    if(currentCategory===category.id){
      const sub=$('div',null,'uiux-subcat');
      for(const variant of [...new Set(items.map(x=>x.subcategory))]){
        const b=$('button',variant+' ('+items.filter(x=>x.subcategory===variant).length+')');
        b.setAttribute('aria-current',String(currentSubcategory===variant));
        b.onclick=()=>{currentSubcategory=currentSubcategory===variant?'':variant;categoryListRender();refreshItems();};
        sub.append(b);
      }
      group.append(sub);
    }
    categoryList.append(group);
  }
}
function refreshItems(){
  menuCleanup();
  results.replaceChildren();
  const matching=registry.components.filter(item=>
    (currentCategory==='all'||item.category===currentCategory) &&
    (!currentSubcategory||item.subcategory===currentSubcategory) &&
    (!keyword||[
      item.id,item.name_ru,item.name_en,item.short_ru,item.short_en,item.variant,
      item.renderer,...item.source,...item.css,...item.uses
    ].join(' ').toLocaleLowerCase('ru').includes(keyword.toLocaleLowerCase('ru')))
  );
  const category=registry.categories.find(c=>c.id===currentCategory);
  title.textContent=category?(en()?category.name_en:category.name_ru):en()?'All components':'Все компоненты';
  count.textContent=(en()?'Found ':'Найдено ')+matching.length+' / '+registry.components.length;
  const note=$('p',en()?'ORIGINAL calls original UI APIs; APPROVED uses v13 styling in isolated previews; ADAPTER is not the production component.':'ORIGINAL вызывает штатный UI API; APPROVED использует согласованное оформление v13 в изолированном стенде; ADAPTER не подменяет production-компонент.','uiux-note');
  results.append(note);
  const grid=$('div',null,'uiux-card-grid');
  const limit=currentCategory==='all'&&!keyword?25:100;
  matching.slice(0,limit).forEach(item=>grid.append(card(item)));
  results.append(grid);
  if(matching.length>limit)results.append($('p','Показаны первые '+limit+' из '+matching.length+'. Уточните поиск или выберите категорию.','uiux-note'));
  if(!matching.length)results.append($('p',en()?'Nothing found':'Компоненты не найдены','empty-state'));
  showCompare();
}
function buildWindow(contents){
  root=$('div',null,'uiux-root');
  const side=$('aside',null,'uiux-sidebar');
  const head=$('div',null,'uiux-side-head');
  head.append($('div','COSMOSIMULATOR / UI LAB','uiux-eyebrow'),$('h3','UI/UX'),$('p','Реестр интерфейсных элементов'));
  const catSearch=$('input');catSearch.id='uiux-category-search';catSearch.className='uiux-control';catSearch.type='search';catSearch.placeholder='Поиск категории…';
  catSearch.oninput=categoryListRender;
  head.append(catSearch);
  categoryList=$('nav',null,'uiux-category-scroll');categoryList.setAttribute('aria-label','Категории UI/UX');
  side.append(head,categoryList);
  const main=$('main',null,'uiux-main');
  const toolbar=$('header',null,'uiux-toolbar');
  title=$('h3','Контекстные меню');
  search=$('input');search.id='uiux-search';search.type='search';search.className='uiux-control';search.placeholder='Название, ККМ, ID, CSS…';
  search.addEventListener('input',()=>{keyword=search.value.trim();refreshItems();});
  const narrowLabel=$('label');const narrowCheckbox=$('input');narrowCheckbox.type='checkbox';
  narrowCheckbox.onchange=()=>{narrow=narrowCheckbox.checked;refreshItems();};
  narrowLabel.append(narrowCheckbox,$('span','Узкая область'));
  languageButton=button('EN',()=>{language=language==='ru'?'en':'ru';languageButton.textContent=en()?'RU':'EN';categoryListRender();refreshItems();});
  count=$('span','','uiux-small');
  toolbar.append(title,search,narrowLabel,languageButton,count);
  const content=$('div',null,'uiux-content');
  comparePanel=$('section',null,'uiux-compare');comparePanel.hidden=true;
  results=$('div');
  const foot=$('div',null,'uiux-foot');
  foot.textContent='Демосреда · исходный web неизменён · все изменения локальны · каталог не является production-библиотекой';
  content.append(comparePanel,results,foot);
  const feedback=$('span','','uiux-feedback');
  feedback.setAttribute('aria-live','polite');
  feedback.style.cssText='position:absolute;bottom:3px;right:10px;font:10px monospace;color:#a9c4ce;pointer-events:none';
  main.append(toolbar,content,feedback);
  root.append(side,main);
  contents.replaceChildren(root);
  categoryListRender();refreshItems();
}
async function openUiLab() {
  if(!registry){
    const response=await fetch('/mock/ui-lab/catalog.json',{cache:'no-store'});
    if(!response.ok)throw Error('UI/UX catalog '+response.status);
    registry=await response.json();
  }
  const content=Modules.create(LAB_ID,'UI/UX · Каталог компонентов',1180,760,{private:false});
  content.classList.add('uiux-content');
  if(!root||!root.isConnected||labWindow!==content){
    labWindow=content;
    buildWindow(content);
  }
  Modules.open(LAB_ID);
}
export function initUiLab() {
  if(document.querySelector('[data-uiux-open]'))return;
  if(!document.getElementById('uiux-lab-css')){
    const stylesheet=$('link');stylesheet.id='uiux-lab-css';stylesheet.rel='stylesheet';stylesheet.href='/mock/ui-lab/lab.css';
    document.head.append(stylesheet);
  }
  const sidebar=document.querySelector('#sidebar');
  const reference=sidebar?.querySelector('[data-chat-open]')||sidebar?.querySelector('[data-open="guide"]');
  if(!sidebar||!reference)return;
  const nav=$('button');
  nav.type='button';nav.className='';nav.dataset.uiuxOpen='true';nav.dataset.nav='UI/UX';
  const icon=$('span',null,'nav-icon');
  const svg=document.createElementNS('http://www.w3.org/2000/svg','svg');
  svg.setAttribute('viewBox','0 0 24 24');
  svg.setAttribute('aria-hidden','true');
  const path=document.createElementNS('http://www.w3.org/2000/svg','path');
  path.setAttribute('d','M4 5h7v7H4zM13 5h7v7h-7zM4 14h7v6H4zM13 14h7v6h-7z');
  svg.append(path);icon.append(svg);
  const text=$('span','UI/UX','nav-label');
  nav.append(icon,text);reference.before(nav);
  nav.onclick=()=>openUiLab().catch(error=>{console.error('[UI/UX]',error);notify('Не удалось открыть UI/UX: '+error.message,'error');});
  window.addEventListener('moduleclose',event=>{
    if(event.detail===LAB_ID){
      menuCleanup();
      // Original UI.menu / UI.picker own their popups. A synthetic outside
      // pointerdown invokes the original dismissal controller and releases tracking.
      if(document.querySelector('.context-menu:not([hidden]), .picker-popover'))
        document.dispatchEvent(new PointerEvent('pointerdown',{bubbles:true}));
      for(const id of activeWindows) Modules.close(id);
      activeWindows.clear();
    }
  });
  window.addEventListener('signedout',()=>{menuCleanup();activeCompare.clear();stateById.clear();});
}
