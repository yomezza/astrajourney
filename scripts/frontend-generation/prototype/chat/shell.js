
(()=>{
  'use strict';
  const $=id=>document.getElementById(id);
  const state={name:'Commander',level:7,xp:370,required:500,upgradeCost:2400,credits:184520,admin:false,compact:false,lang:'ru',window:null,pinned:false,maximized:false,notifications:2};
  const names=['Галактическая карта','Карта текущей системы','Колонии','Корабль','Верфи','Добыча','Исследования','Рынок','Корпорация','Логистика','Планировщик задач','Руководство пилота','Предложить идею','Профиль игрока','Настройки','История финансов','Уведомления','Бенчмарк','Игровой процесс','Галактики','Журнал изменений','Выдача ресурсов','Предложения игроков (админ)','Пользователи','Пропустить тик'];
  let toastTimer=null;
  function toast(message){$('toast').textContent=message;$('toast').hidden=false;clearTimeout(toastTimer);toastTimer=setTimeout(()=>$('toast').hidden=true,3600)}
  function formatted(n){return Number(n).toLocaleString('ru-RU',{maximumFractionDigits:0})}
  function addRow(root,label,value){const d=document.createElement('div');d.className='row';const s=document.createElement('span');s.textContent=label;const b=document.createElement('b');b.textContent=value;d.append(s,b);root.append(d)}
  function action(root,label,onClick){const b=document.createElement('button');b.className='terminal-action';b.type='button';b.textContent=label;b.addEventListener('click',onClick);root.append(b);return b}
  function text(root,value,cls){const p=document.createElement('p');p.textContent=value;if(cls)p.className=cls;root.append(p);return p}
  function renderWindow(name){
    const c=$('module-content');c.replaceChildren();
    if(name==='Настройки'){
      const h=document.createElement('h3');h.textContent='Язык';c.append(h);
      const lang=document.createElement('select');[['ru','Русский'],['en','English']].forEach(([v,t])=>lang.add(new Option(t,v)));lang.value=state.lang;lang.addEventListener('change',()=>{state.lang=lang.value;toast('Демо: язык интерфейса переключается в основном приложении')});c.append(lang);
      const view=document.createElement('h3');view.textContent='Отображение';view.style.marginTop='16px';c.append(view);
      const density=document.createElement('select');[['normal','Обычный интерфейс'],['compact','Компактный интерфейс']].forEach(([v,t])=>density.add(new Option(t,v)));density.value=state.compact?'compact':'normal';density.addEventListener('change',()=>{state.compact=density.value==='compact';document.body.classList.toggle('compact-ui',state.compact);toast('Выбрано: '+density.selectedOptions[0].textContent)});c.append(density);
      text(c,'Плотность меняет размеры элементов во всех рабочих модулях.','muted');
      const ah=document.createElement('h3');ah.textContent='Управление окнами';c.append(ah);action(c,'Восстановить расположение окон',()=>{state.pinned=false;state.maximized=false;$('demo-window').style.cssText='';$('pin-window').setAttribute('aria-pressed','false');toast('Расположение окна восстановлено')});
      const sep=document.createElement('hr');sep.style.border='0';sep.style.borderTop='1px solid var(--border)';sep.style.margin='18px 0';c.append(sep);
      const l=document.createElement('label');const checkbox=document.createElement('input');checkbox.type='checkbox';checkbox.checked=state.admin;checkbox.style.width='auto';checkbox.addEventListener('change',()=>{state.admin=checkbox.checked;$('admin-menu').hidden=!state.admin;toast(state.admin?'Демо: права администратора включены':'Демо: обычный игрок')});l.append(checkbox,document.createTextNode(' Тестовый режим администратора'));c.append(l);
      text(c,'Переключатель добавлен только в демоверсию, чтобы проверить административное меню.','muted');
    }else if(name==='Профиль игрока'){
      addRow(c,'Позывной',state.name);addRow(c,'Стаж',String(state.level));addRow(c,'Опыт',state.xp+' / '+state.required+' XP');addRow(c,'Корпорация','NOVA Research');addRow(c,'Роль','Пилот');
    }else if(name==='Уведомления'){
      text(c,'Новые уведомления','muted');addRow(c,'Задание','Добыча завершена');addRow(c,'Исследования','Новый этап доступен');action(c,'Отметить всё прочитанным',()=>{state.notifications=0;$('notification-count').hidden=true;toast('Уведомления прочитаны')});
    }else if(name==='Корабль'){
      addRow(c,'Название','Странник');addRow(c,'Класс','Исследователь');addRow(c,'Местоположение','Эдем · Орбита');addRow(c,'Топливо','420 / 600');addRow(c,'Груз','42 / 125');const wrap=document.createElement('div');wrap.className='modal-footer';c.append(wrap);action(wrap,'Трюм',()=>toast('Демо: отдельное окно трюма'));action(wrap,'Найти на карте',()=>openWindow('Галактическая карта'));
    }else if(name==='Галактическая карта'||name==='Карта текущей системы'){
      const x=document.createElement('div');x.style.cssText='min-height:165px;margin:10px 0;border:1px solid #354d5b;background:radial-gradient(circle at 42% 40%,#253a49,#111923 72%);position:relative;display:grid;place-items:center;color:#8fcbd1;letter-spacing:2px;font-size:10px';x.textContent='✦ · ЭДЕМ · ✦';c.append(x);text(c,'Демонстрационная заглушка карты. Здесь воспроизводится только главный экран; данные карт и симуляция не загружаются.');
    }else if(name==='История финансов'){
      addRow(c,'Баланс',formatted(state.credits)+' CR');addRow(c,'Доход от добычи','+5 200 CR');addRow(c,'Рынок','−1 400 CR');
    }else if(name==='Пропустить тик'){
      text(c,'Демо-действие администратора. Реальное игровое время не изменяется.');action(c,'Симулировать игровой тик',()=>{$('star-date').textContent='0019 ЭД · 137 · 685';toast('Тестовый тик выполнен')});
    }else{
      text(c,'Этот модуль существует в навигации Astra Journey. В данной демонстрации подробно воспроизведена только главная страница.');
      text(c,'Все значения вымышленные; запросов к Go-серверу и PostgreSQL нет.','muted');
      action(c,'Вернуться на главную',closeWindow);
    }
    const note=document.createElement('div');note.className='demo-note';note.textContent='DEMO MODE · FRONTEND ONLY · NO BACKEND';c.append(note);
  }
  function closeMenus(){document.querySelectorAll('.dropdown').forEach(m=>m.open=false)}
  function openWindow(name){
    if(!names.includes(name))return;
    closeMenus();state.window=name;$('module-title').textContent=name;$('demo-window').hidden=false;
    document.querySelectorAll('#sidebar [data-module]').forEach(b=>b.classList.toggle('module-open',b.dataset.module===name));
    $('window-switcher').replaceChildren();const tab=action($('window-switcher'),name,()=>{$('demo-window').hidden=false});tab.classList.add('active');
    renderWindow(name);
  }
  function closeWindow(){state.window=null;$('demo-window').hidden=true;$('window-switcher').replaceChildren();document.querySelectorAll('#sidebar [data-module]').forEach(b=>b.classList.remove('module-open'))}
  document.addEventListener('click',e=>{
    const b=e.target.closest('[data-module]');if(b){openWindow(b.dataset.module);return}
    for(const menu of document.querySelectorAll('.dropdown'))if(!menu.contains(e.target))menu.open=false;
  });
  $('sidebar-toggle').addEventListener('click',()=>{
    const value=$('sidebar').classList.toggle('collapsed');document.body.classList.toggle('sidebar-collapsed',value);
    $('sidebar-toggle').setAttribute('aria-expanded',String(!value));$('sidebar-toggle').setAttribute('aria-label',value?'Развернуть панель':'Свернуть панель');$('sidebar-toggle').title=value?'Развернуть панель':'Свернуть панель';
  });
  $('brand').addEventListener('click',e=>{e.preventDefault();closeWindow();window.scrollTo({top:0,behavior:'smooth'})});
  $('notifications-button').addEventListener('click',()=>openWindow('Уведомления'));
  $('logout').addEventListener('click',()=>{closeMenus();toast('Демоверсия: выход не выполняется — авторизации нет')});
  $('overview-upgrade').addEventListener('click',()=>{
    if(state.xp<state.required||state.credits<state.upgradeCost)return;
    state.level++;state.xp-=state.required;state.required=Math.round(state.required*1.4);state.credits-=state.upgradeCost;
    state.upgradeCost=Math.round(state.upgradeCost*1.3);renderOverview();toast('Стаж игрока повышен!');
  });
  function renderOverview(){
    $('welcome-name').textContent=$('profile-nick').textContent=state.name;
    $('overview-level').textContent=String(state.level);$('overview-xp').value=state.xp;$('overview-xp').max=state.required;
    $('overview-xp-text').textContent=state.xp+' / '+state.required+' XP';
    $('overview-upgrade').disabled=state.xp<state.required||state.credits<state.upgradeCost;
    $('overview-upgrade').title=state.upgradeCost+' CR';$('credits').textContent=formatted(state.credits);
  }
  $('close-window').addEventListener('click',closeWindow);
  $('pin-window').addEventListener('click',()=>{state.pinned=!state.pinned;$('pin-window').setAttribute('aria-pressed',String(state.pinned));$('demo-window-titlebar').style.cursor=state.pinned?'default':'move';toast(state.pinned?'Окно закреплено':'Закрепление снято')});
  $('max-window').addEventListener('click',()=>{state.maximized=!state.maximized;$('demo-window').style.cssText=state.maximized?'left:8px;top:65px;width:calc(100vw - 16px);height:calc(100vh - 106px);resize:none;max-height:none;':'';$('max-window').textContent=state.maximized?'❐':'□'});
  const bar=$('demo-window-titlebar'),win=$('demo-window');let drag=null;
  bar.addEventListener('pointerdown',e=>{if(state.pinned||state.maximized||e.target.closest('button'))return;drag={x:e.clientX,y:e.clientY,left:win.getBoundingClientRect().left,top:win.getBoundingClientRect().top};bar.setPointerCapture(e.pointerId)});
  bar.addEventListener('pointermove',e=>{if(!drag)return;const box=win.getBoundingClientRect();win.style.left=Math.max(0,Math.min(window.innerWidth-box.width,drag.left+e.clientX-drag.x))+'px';win.style.top=Math.max(60,Math.min(window.innerHeight-box.height-25,drag.top+e.clientY-drag.y))+'px'});
  bar.addEventListener('pointerup',()=>drag=null);bar.addEventListener('pointercancel',()=>drag=null);
  document.addEventListener('keydown',e=>{if(e.key==='Escape'){if(document.querySelector('.dropdown[open]'))closeMenus();else closeWindow()}});
  let now=new Date('2026-10-30T14:36:00');function refreshClock(){
    now=new Date(now.getTime()+1000);$('connection').textContent='Данные обновлены: '+now.toLocaleTimeString('ru-RU',{hour12:false});
  }setInterval(refreshClock,1000);
  renderOverview();
})();
