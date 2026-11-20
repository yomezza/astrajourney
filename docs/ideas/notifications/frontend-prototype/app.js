/* Astra Journey · isolated notifications UI prototype.
 * No imports, fetch, WebSocket, persistence, commands or production integration.
 * Works via file:// with fixtures.js and styles.css in the same directory.
 */
(function () {
  'use strict';

  const fixture = globalThis.AstraNoticeFixtures;
  if (!fixture) {
    throw new Error('Missing local fixtures.js — open this prototype with all files in the same folder.');
  }

  const $ = (id) => document.getElementById(id);
  const create = (tag, className, text) => {
    const node = document.createElement(tag);
    if (className) node.className = className;
    if (text !== undefined) node.textContent = String(text);
    return node;
  };
  const els = {
    stage:$('preview-stage'), window:$('notice-window'), handle:$('window-drag-handle'),
    body:$('notice-body'), scroll:$('notice-scroll'), list:$('notice-list'),
    unreadCount:$('notice-count'), totalLabel:$('notice-total-label'),
    statusLine:$('notice-statusline'), markAll:$('mark-all'),
    all:$('filter-all'), unread:$('filter-unread'),
    preview:$('design-preview'), variantText:$('variant-description'), variantCode:$('preview-variant'),
    status:$('demo-status'), totalStat:$('stat-total'), unreadStat:$('stat-unread'),
    detail:$('notice-detail'), detailTitle:$('detail-title'), detailText:$('detail-text'),
    detailCategory:$('detail-category'), detailTime:$('detail-time'),
    detailRead:$('detail-mark-read'), detailClose:$('detail-close'), detailDone:$('detail-done'),
    minimize:$('minimize-window'), reopen:$('reopen-window')
  };

  const state = {
    items:fixture.createItems(),filter:'all',variant:'a',size:'regular',
    expandedId:null,returnFocus:null,seq:0,minimized:false,drag:null
  };
  const variants = Object.freeze({
    a:{description:'Рекомендуемый вариант: привычные 34-пиксельный заголовок, плотность и цветовая система чата.',code:'STYLE A · COMPACT CHAT'},
    b:{description:'Техническая консоль: код категории, тонкая цветная полоса и компактная служебная типографика.',code:'STYLE B · NOTIFICATION CONSOLE'},
    c:{description:'Минимальная лента: тише рамки, больше воздуха у текста, меньше системных меток.',code:'STYLE C · ACTIVITY FEED'}
  });
  const descriptions = Object.freeze({
    system:'Системное',economy:'Экономика',research:'Исследования',
    corporation:'Корпорация',gameplay:'Игровое'
  });
  // Icons are locally drawn with the same monochrome stroke treatment as chat UI.
  const glyphs = Object.freeze({
    system:'M12 3v2m0 14v2M3 12h2m14 0h2M5.6 5.6l1.4 1.4m10 10 1.4 1.4M18.4 5.6 17 7m-10 10-1.4 1.4M12 8a4 4 0 1 0 0 8a4 4 0 0 0 0-8Z',
    economy:'M3 7h18v11H3zM3 10h18M7 15h4M7 4h10',
    research:'M9 3h6M12 3v6m-5 12h10M7.5 21l2-6 5-2 2-4M12 8a3 3 0 1 0 0 6a3 3 0 0 0 0-6Z',
    corporation:'M3 20h18M5 20V9l7-4 7 4v11M9 13h1m4 0h1m-6 4h1m4 0h1M12 5V3',
    gameplay:'M12 3l7 4v5c0 4-3 7-7 9-4-2-7-5-7-9V7zM9 12l2 2 4-4'
  });
  function icon(kind) {
    const svg=document.createElementNS('http://www.w3.org/2000/svg','svg');
    svg.setAttribute('viewBox','0 0 24 24');
    svg.setAttribute('fill','none');
    svg.setAttribute('stroke','currentColor');
    svg.setAttribute('stroke-width','1.65');
    svg.setAttribute('stroke-linecap','round');
    svg.setAttribute('stroke-linejoin','round');
    svg.setAttribute('aria-hidden','true');
    const path=document.createElementNS('http://www.w3.org/2000/svg','path');
    path.setAttribute('d',glyphs[kind]||glyphs.system);
    svg.append(path);
    return svg;
  }
  function notify(text) {
    els.status.textContent=text;
  }
  function unreadCount() {return state.items.filter(item=>!item.read).length;}
  function when(value) {
    const date=new Date(value);
    const ago=Math.max(0,Math.floor((Date.now()-date.getTime())/60000));
    if (ago<1) return 'только что';
    if (ago<60) return ago+' мин';
    if (ago<24*60) return Math.floor(ago/60)+' ч';
    return new Intl.DateTimeFormat('ru-RU',{day:'numeric',month:'short'}).format(date);
  }
  function fullDate(value) {
    return new Intl.DateTimeFormat('ru-RU',{
      year:'numeric',month:'long',day:'numeric',hour:'2-digit',minute:'2-digit'
    }).format(new Date(value));
  }
  function actionButton(label,className,fn,ariaLabel) {
    const button=create('button',className,label);
    button.type='button';
    if (ariaLabel) button.setAttribute('aria-label',ariaLabel);
    button.addEventListener('click',fn);
    return button;
  }
  function setCounts() {
    const unread=unreadCount();
    els.unreadCount.textContent=unread>99?'99+':String(unread);
    els.unreadCount.hidden=unread===0;
    els.unreadCount.setAttribute('aria-label','Непрочитанных: '+unread);
    els.totalLabel.textContent=state.items.length;
    els.totalStat.textContent=state.items.length;
    els.unreadStat.textContent=unread;
    els.markAll.disabled=unread===0;
    els.statusLine.textContent=state.filter==='unread'?'Непрочитанные':'Лента событий';
    els.all.classList.toggle('active',state.filter==='all');
    els.unread.classList.toggle('active',state.filter==='unread');
    els.all.setAttribute('aria-pressed',String(state.filter==='all'));
    els.unread.setAttribute('aria-pressed',String(state.filter==='unread'));
  }
  function groupTitle(text,count) {
    const label=create('h3','notice-group-heading');
    label.append(create('span',null,text),create('small',null,String(count)));
    return label;
  }
  function createRow(item) {
    const row=create('article','notice-row');
    row.dataset.kind=item.kind;
    row.dataset.level=item.level;
    row.dataset.read=String(item.read);
    row.dataset.id=item.id;
    const glyph=create('div','notice-glyph');
    glyph.append(icon(item.kind));
    const content=actionButton('','notice-open',()=>openDetail(item.id),
      'Открыть уведомление: '+item.title);
    content.dataset.noticeId=item.id;
    const upper=create('span','notice-row-meta');
    const kindLabel=create('span','notice-kind',descriptions[item.kind]||'Система');
    kindLabel.dataset.code=fixture.kinds[item.kind]?.code||'SYS';
    upper.append(kindLabel);
    const time=create('time','notice-time',when(item.createdAt));
    time.dateTime=item.createdAt;
    time.title=fullDate(item.createdAt);
    upper.append(time);
    const title=create('span','notice-row-title',item.title);
    const body=create('span','notice-row-excerpt',item.text);
    content.append(upper,title,body);
    const actions=create('div','notice-row-end');
    if (!item.read) {
      actions.append(create('span','notice-unread-dot'));
      actions.append(actionButton('✓','notice-mark-one',()=>markRead(item.id),
        'Отметить прочитанным: '+item.title));
    } else {
      actions.append(create('span','notice-read-mark','✓'));
    }
    row.append(glyph,content,actions);
    return row;
  }
  function emptyPanel() {
    const host=create('div','notice-empty');
    const emblem=create('div','notice-empty-symbol','◇');
    host.append(emblem,create('strong',null,
      state.items.length?'Новых уведомлений нет':'Всё спокойно'));
    host.append(create('p',null,state.items.length?
      'Вы прочитали все события. Новые появятся здесь.':
      'Пока нет событий. Попробуйте добавить тестовое уведомление.'));
    return host;
  }
  function render({focusId=null,resetScroll=false}={}) {
    const oldTop=els.scroll.scrollTop;
    const fragment=document.createDocumentFragment();
    const visible=state.filter==='unread'?state.items.filter(v=>!v.read):state.items;
    if (!visible.length) fragment.append(emptyPanel());
    else {
      const groups=state.filter==='unread'?
        [['Непрочитанные',visible]]:
        [['Непрочитанные',visible.filter(v=>!v.read)],
         ['Прочитанные',visible.filter(v=>v.read)]];
      for (const [heading,items] of groups) {
        if (!items.length) continue;
        const section=create('section','notice-group');
        section.setAttribute('aria-label',heading);
        section.append(groupTitle(heading,items.length));
        for(const item of items) section.append(createRow(item));
        fragment.append(section);
      }
    }
    els.list.replaceChildren(fragment);
    setCounts();
    if (resetScroll) els.scroll.scrollTop=0;
    else els.scroll.scrollTop=oldTop;
    if (focusId) {
      const next=Array.from(els.list.querySelectorAll('.notice-open'))
        .find(btn=>btn.dataset.noticeId===focusId);
      (next||els.unread).focus({preventScroll:true});
    }
  }
  function markRead(id) {
    const item=state.items.find(row=>row.id===id);
    if (!item || item.read) return;
    item.read=true;
    const isDetailOpen=state.expandedId===id && !els.detail.hidden;
    render({focusId:isDetailOpen?null:id});
    if(isDetailOpen){els.detailRead.disabled=true;els.detailDone.focus();}
    notify('Прочитано: '+item.title);
  }
  function markEverything() {
    const count=unreadCount();
    if(!count)return;
    state.items.forEach(item=>{item.read=true;});
    render();
    if(state.expandedId)els.detailRead.disabled=true;
    notify('Отмечено прочитанными: '+count);
  }
  function setFilter(next) {
    if(!['all','unread'].includes(next))return;
    state.filter=next;
    render({resetScroll:true});
    notify(next==='all'?'Показаны все уведомления':'Показаны только непрочитанные');
  }
  function openDetail(id) {
    const item=state.items.find(row=>row.id===id);
    if(!item)return;
    state.returnFocus=document.activeElement;
    state.expandedId=id;
    els.detailCategory.textContent=descriptions[item.kind]||'Системное';
    els.detailCategory.dataset.kind=item.kind;
    els.detailTitle.textContent=item.title;
    els.detailTime.textContent=fullDate(item.createdAt);
    els.detailTime.dateTime=item.createdAt;
    els.detailText.textContent=item.text;
    els.detailRead.disabled=item.read;
    els.detail.hidden=false;
    els.detailClose.focus();
  }
  function closeDetail() {
    if(els.detail.hidden)return;
    els.detail.hidden=true;
    state.expandedId=null;
    const focus=state.returnFocus;
    state.returnFocus=null;
    if(focus?.isConnected)focus.focus({preventScroll:true});
    else els.scroll.focus({preventScroll:true});
  }
  function handleModalKeys(event) {
    if(els.detail.hidden)return;
    if(event.key==='Escape'){
      event.preventDefault();closeDetail();return;
    }
    if(event.key!=='Tab')return;
    const focusable=[els.detailClose,els.detailRead,els.detailDone].filter(x=>!x.disabled);
    const first=focusable[0],last=focusable[focusable.length-1];
    if(event.shiftKey&&document.activeElement===first){event.preventDefault();last.focus();}
    else if(!event.shiftKey&&document.activeElement===last){event.preventDefault();first.focus();}
  }
  function setVariant(variant) {
    if(!variants[variant])return;
    state.variant=variant;
    els.window.dataset.variant=variant;
    els.variantText.textContent=variants[variant].description;
    els.variantCode.textContent=variants[variant].code;
    for(const b of document.querySelectorAll('.variant-choice')){
      const chosen=b.dataset.variant===variant;
      b.classList.toggle('selected',chosen);
      b.setAttribute('aria-selected',String(chosen));
      b.tabIndex=chosen?0:-1;
      if(chosen)els.preview.setAttribute('aria-labelledby',b.id);
    }
    notify('Выбран вариант '+variant.toUpperCase());
  }
  function setSize(next) {
    if(!['compact','regular'].includes(next))return;
    state.size=next;
    els.window.dataset.size=next;
    els.window.style.width=(next==='compact'?366:414)+'px';
    els.window.style.height=(next==='compact'?444:512)+'px';
    for(const btn of document.querySelectorAll('[data-size]')){
      btn.setAttribute('aria-pressed',String(btn.dataset.size===next));
    }
    clampWindow();
    notify('Размер окна: '+(next==='compact'?'компактный':'обычный'));
  }
  function resetData() {
    closeDetail();
    state.items=fixture.createItems();
    state.filter='all';
    render({resetScroll:true});
    notify('Набор тестовых уведомлений восстановлен');
  }
  function newEvent() {
    closeDetail();
    state.seq++;
    state.items.unshift(fixture.newItem(state.seq));
    render({resetScroll:true});
    notify('Добавлено новое mock-событие');
  }
  function showEmpty() {
    closeDetail();state.items=[];state.filter='all';
    render({resetScroll:true});notify('Пустое состояние');
  }
  function showLong() {
    closeDetail();state.items=fixture.createItems(40);state.filter='all';
    render({resetScroll:true});notify('Загружено 40 mock-событий для проверки прокрутки');
  }
  function minimize() {
    state.minimized=!state.minimized;
    els.window.classList.toggle('is-minimized',state.minimized);
    els.minimize.textContent=state.minimized?'□':'−';
    els.minimize.setAttribute('aria-label',state.minimized?'Развернуть содержимое уведомлений':'Свернуть содержимое уведомлений');
    els.minimize.title=state.minimized?'Развернуть':'Свернуть';
    closeDetail();
    clampWindow();
    notify(state.minimized?'Окно свёрнуто':'Окно развёрнуто');
  }
  function closeWindow() {
    closeDetail();
    els.window.hidden=true;
    els.reopen.hidden=false;
    els.reopen.focus();
    notify('Окно закрыто. Открыть снова можно из панели предпросмотра.');
  }
  function reopenWindow() {
    els.window.hidden=false;
    els.reopen.hidden=true;
    clampWindow();
    els.handle.focus();
    notify('Окно уведомлений открыто');
  }
  function clampWindow() {
    const stage=els.stage.getBoundingClientRect();
    const box=els.window.getBoundingClientRect();
    if(stage.width===0)return;
    const maxX=Math.max(5,stage.width-box.width-6);
    const maxY=Math.max(5,stage.height-box.height-6);
    const rawX=Number.parseFloat(els.window.style.left);
    const rawY=Number.parseFloat(els.window.style.top);
    els.window.style.left=Math.min(maxX,Math.max(5,Number.isFinite(rawX)?rawX:Math.max(5,(stage.width-box.width)/2)))+'px';
    els.window.style.top=Math.min(maxY,Math.max(5,Number.isFinite(rawY)?rawY:Math.max(5,(stage.height-box.height)/2)))+'px';
  }
  function initDrag() {
    const bar=els.handle;
    bar.addEventListener('pointerdown',event=>{
      if(event.button!==0||event.target.closest('button'))return;
      const stage=els.stage.getBoundingClientRect();
      const win=els.window.getBoundingClientRect();
      state.drag={pointer:event.pointerId,startX:event.clientX,startY:event.clientY,
        x:win.x-stage.x,y:win.y-stage.y};
      bar.setPointerCapture(event.pointerId);
    });
    bar.addEventListener('pointermove',event=>{
      const drag=state.drag;
      if(!drag||drag.pointer!==event.pointerId)return;
      els.window.style.left=(drag.x+event.clientX-drag.startX)+'px';
      els.window.style.top=(drag.y+event.clientY-drag.startY)+'px';
      clampWindow();
    });
    const stop=event=>{if(state.drag?.pointer===event.pointerId)state.drag=null;};
    bar.addEventListener('pointerup',stop);
    bar.addEventListener('pointercancel',stop);
    bar.addEventListener('lostpointercapture',()=>{state.drag=null;});
    bar.addEventListener('keydown',event=>{
      const delta={ArrowLeft:[-12,0],ArrowRight:[12,0],ArrowUp:[0,-12],ArrowDown:[0,12]}[event.key];
      if(!delta||event.target!==bar)return;
      event.preventDefault();
      els.window.style.left=(parseFloat(els.window.style.left)||0)+delta[0]+'px';
      els.window.style.top=(parseFloat(els.window.style.top)||0)+delta[1]+'px';
      clampWindow();
    });
    window.addEventListener('resize',clampWindow);
    if(typeof ResizeObserver!=='undefined'){
      const observer=new ResizeObserver(clampWindow);
      observer.observe(els.window);
      observer.observe(els.stage);
    }
  }
  function installEvents() {
    els.all.addEventListener('click',()=>setFilter('all'));
    els.unread.addEventListener('click',()=>setFilter('unread'));
    els.markAll.addEventListener('click',markEverything);
    els.detailClose.addEventListener('click',closeDetail);
    els.detailDone.addEventListener('click',closeDetail);
    els.detailRead.addEventListener('click',()=>{
      if(state.expandedId)markRead(state.expandedId);
    });
    els.detail.addEventListener('pointerdown',event=>{
      if(event.target===els.detail)closeDetail();
    });
    document.addEventListener('keydown',handleModalKeys,true);
    document.querySelectorAll('.variant-choice').forEach(btn=>{
      btn.addEventListener('click',()=>setVariant(btn.dataset.variant));
      btn.addEventListener('keydown',event=>{
        if(!['ArrowLeft','ArrowRight','Home','End'].includes(event.key))return;
        event.preventDefault();
        const keys=['a','b','c'];const cur=keys.indexOf(btn.dataset.variant);
        const next=event.key==='Home'?0:event.key==='End'?2:
          (cur+(event.key==='ArrowRight'?1:-1)+keys.length)%keys.length;
        setVariant(keys[next]);
        document.querySelector('[data-variant="'+keys[next]+'"].variant-choice').focus();
      });
    });
    document.querySelectorAll('.size-choices [data-size]').forEach(btn=>{
      btn.addEventListener('click',()=>setSize(btn.dataset.size));
    });
    $('reset-data').addEventListener('click',resetData);
    $('new-event').addEventListener('click',newEvent);
    $('empty-data').addEventListener('click',showEmpty);
    $('long-data').addEventListener('click',showLong);
    $('minimize-window').addEventListener('click',minimize);
    $('close-window').addEventListener('click',closeWindow);
    els.reopen.addEventListener('click',reopenWindow);
  }
  installEvents();
  render({resetScroll:true});
  setSize('regular');
  initDrag();
  // Stable local test interface: no mutation operations leave this browser page.
  globalThis.AstraNoticeDemo=Object.freeze({
    snapshot:()=>({variant:state.variant,filter:state.filter,
      items:state.items.map(x=>({...x})),unread:unreadCount()}),
    setVariant, setFilter,markRead,markEverything,resetData,showEmpty,showLong,newEvent,
    minimize,closeWindow,reopenWindow
  });
})();
