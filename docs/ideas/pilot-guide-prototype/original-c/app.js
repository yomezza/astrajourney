/* Astra Journey: Original C / knowledge-base design handoff.
   Local-only interactive simulation, no game services, no network. */
(() => {
  'use strict';
  const FIX=window.GUIDE_FIXTURES;
  if(!FIX) throw new Error('Local guide fixtures missing');
  const $=(selector,root=document)=>root.querySelector(selector);
  const $$=(selector,root=document)=>[...root.querySelectorAll(selector)];
  const node=(tag,cls='',text)=>{const e=document.createElement(tag);if(cls)e.className=cls;if(text!==undefined)e.textContent=String(text);return e;};
  const append=(host,...children)=>{for(const x of children)if(x)host.append(x);return host;};
  const button=(text,action,cls='')=>{const b=node('button',cls,text);b.type='button';b.onclick=action;return b;};
  const tiny=(text,cls='guide-eyebrow')=>node('span',cls,text);
  const state={selected:'start',query:'',tab:'all',category:'Все темы',favorites:new Set(),recent:['start'],stress:false,empty:false,preset:'default',closed:false,minimized:false};
  const scrollMemory=new Map();
  const OBJECT_IDS=new Set(['star','garden','rocky','icy','gas','volcanic','asteroid','icefield','station','blackhole']);
  function articles(){
    if(state.empty)return [];
    const base=FIX.articles;
    if(!state.stress)return base;
    const extras=[];
    const seed=base.filter(a=>a.id!=='experience');
    for(let i=0;i<40;i++){
      const original=seed[i%seed.length];
      extras.push({...original,id:'stress-'+i,title:original.title+' · архив '+String(i+1).padStart(2,'0'),summary:'Демо-материал для проверки длинного каталога. '+original.summary,source:'ДЕМО · нагрузочный сценарий',related:[original.id]});
    }
    return [...base,...extras];
  }
  const find=id=>articles().find(a=>a.id===id);
  const now=()=>find(state.selected)||articles()[0]||null;
  const normalize=v=>String(v||'').toLocaleLowerCase('ru').replace(/ё/g,'е').trim();
  function search(query,items=articles()){
    const words=normalize(query).split(/\s+/).filter(Boolean);
    if(!words.length)return items;
    return items.filter(a=>{
      const hay=normalize([a.title,a.category,a.summary,a.keywords,a.source,...a.sections.map(s=>[s.title,s.text,...(s.bullets||[])].join(' '))].join(' '));
      return words.every(word=>hay.includes(word));
    });
  }
  function status(text){$('#window-status').textContent=text;}
  function openArticle(id){
    const a=find(id);if(!a)return;
    state.selected=a.id;
    state.recent=[a.id,...state.recent.filter(x=>x!==a.id)].slice(0,12);
    render();status('СТАТЬЯ / '+a.title.toUpperCase().slice(0,36));
  }
  function favorite(id){
    if(state.favorites.has(id))state.favorites.delete(id);else state.favorites.add(id);
    render();status('ИЗБРАННОЕ ИЗМЕНЕНО · MOCK');
  }
  function placeholder(title='Ничего не найдено',description='Измените запрос или фильтр.'){
    return append(node('div','result-empty'),node('b','',title),node('span','',description));
  }
  function input(){
    const e=node('input');e.type='search';e.value=state.query;e.placeholder='Что вы хотите узнать?';e.setAttribute('aria-label','Поиск по заголовкам и текстам статей');e.dataset.focusKey='search';
    e.addEventListener('input',()=>{state.query=e.value;render();});return e;
  }
  function row(a){
    const b=button('',()=>openArticle(a.id),'result-row'+(state.selected===a.id?' active':''));
    b.dataset.focusKey='article-'+a.id;
    append(b,tiny(a.source.startsWith('PRODUCTION')?'◆':'◇','result-code'),append(node('span'),node('strong','',a.title),node('small','',a.summary)),tiny(a.category,'result-meta'));
    return b;
  }
  function model(a){
    const type=OBJECT_IDS.has(a.id)?a.id:a.id.startsWith('stress-')?a.related?.find(id=>OBJECT_IDS.has(id)):null;
    if(!type)return null;
    const frame=node('figure','holo-frame'),caption=node('figcaption','holo-caption');
    frame.dataset.object=type;
    append(caption,node('span','', '◈ SVG / АСТРОГРАФИЧЕСКАЯ ПРОЕКЦИЯ'),node('span','', 'ПОЛНЫЙ ОБОРОТ · 360°'));
    return append(frame,window.AstraGuideHologram.create(type,a.title),caption);
  }
  function article(a){
    const pane=node('article','article-pane');
    if(!a)return append(pane,placeholder('Каталог пуст','Отключите демонстрационный режим пустого каталога.'));
    append(pane,append(node('div','article-topline'),tiny('ACADEMY / '+a.category.toLocaleUpperCase('ru')),tiny(a.source.startsWith('PRODUCTION')?'ПО ПРОИЗВОДСТВЕННОМУ КОДУ':'ДЕМО-МАТЕРИАЛ','article-source')),
      node('h2','guide-title',a.title),node('p','guide-summary',a.summary));
    append(pane,model(a));
    append(pane,append(node('div','article-actions'),button(state.favorites.has(a.id)?'★ В избранном':'☆ В избранное',()=>favorite(a.id),state.favorites.has(a.id)?'saved':''),button('↪ Скопировать тему',()=>{
      if(!navigator.clipboard?.writeText){status('КОПИРОВАНИЕ НЕ ПОДДЕРЖИВАЕТСЯ');return;}
      navigator.clipboard.writeText(a.title).then(()=>status('ЗАГОЛОВОК СКОПИРОВАН')).catch(()=>status('НЕ УДАЛОСЬ СКОПИРОВАТЬ'));
    })));
    for(const s of a.sections){
      const section=append(node('section','article-section'),node('h3','',s.title),node('p','',s.text));
      if(s.bullets?.length){const ul=node('ul');for(const line of s.bullets)append(ul,node('li','',line));append(section,ul);}
      append(pane,section);
    }
    if(a.id==='experience'){
      const table=node('table','experience-table');append(table,node('caption','', 'ТАБЛИЦА ДИАПАЗОНОВ СТАЖА'));
      for(const [idx,values] of [['Стаж','Корабли'],['1–5','2'],['6–10','3'],['11–16','4'],['17–25','6'],['26–35','7'],['36+','7']].entries()){
        const r=node('tr');for(const value of values)append(r,node(idx?'td':'th','',value));append(table,r);
      }append(pane,table);
    }
    const related=a.related.map(find).filter(Boolean);
    if(related.length){const section=append(node('div','article-bottom'),tiny('СВЯЗАННЫЕ ТЕМЫ'));
      append(section,append(node('div','article-links'),...related.map(v=>button(v.title+' ›',()=>openArticle(v.id)))));append(pane,section);}
    const list=articles(),pos=list.findIndex(v=>v.id===a.id),prev=list[pos-1],next=list[pos+1],pagination=node('div','reader-pagination');
    const before=button('← Предыдущая',()=>prev&&openArticle(prev.id)),after=button('Следующая →',()=>next&&openArticle(next.id));before.disabled=!prev;after.disabled=!next;
    append(pagination,before,tiny((pos+1)+' / '+list.length),after);append(pane,pagination);
    return pane;
  }
  function render(options={}){
    const host=$('#window-body');
    const pageY=window.scrollY,pageX=window.scrollX,focus=document.activeElement,key=focus?.dataset?.focusKey;
    let caret=null;try{caret=typeof focus?.selectionStart==='number'?focus.selectionStart:null;}catch{}
    if(options.resetScroll)scrollMemory.clear();
    else for(const e of $$('[data-scroll-key]',host))scrollMemory.set(e.dataset.scrollKey,{top:e.scrollTop,left:e.scrollLeft});
    const body=node('div','mode-c'),hero=node('div','c-hero'),top=node('div','c-hero-top');
    append(top,node('strong','', 'База знаний пилота'),tiny(articles().length+' статей / ЛОКАЛЬНО','tiny-caption'));
    append(hero,top,append(node('div','c-big-search'),node('span','', '⌕'),input()));
    const tabs=node('div','c-tabs');for(const [code,text] of [['all','Все темы'],['favorites','★ Избранное'],['recent','↺ Недавние']]){
      append(tabs,button(text,()=>{state.tab=code;render();},state.tab===code?'active':''));
    }
    const main=node('div','c-main'),index=node('aside','c-index guide-scroll'),reading=node('div','c-reading guide-scroll');
    index.dataset.scrollKey='c-list';reading.dataset.scrollKey='c-reader';
    const cats=node('div','c-category-chips');
    for(const category of ['Все темы',...new Set(articles().map(a=>a.category))])append(cats,button(category,()=>{state.category=category;render();},'g-chip'+(state.category===category?' active':'')));
    append(index,cats);
    let results=articles();
    if(state.category!=='Все темы')results=results.filter(a=>a.category===state.category);
    if(state.tab==='favorites')results=results.filter(a=>state.favorites.has(a.id));
    if(state.tab==='recent')results=state.recent.map(find).filter(Boolean).filter(a=>results.some(x=>x.id===a.id));
    results=search(state.query,results);
    if(results.length)append(index,append(node('div'),...results.map(row)));
    else append(index,placeholder(state.tab==='favorites'?'Избранное пусто':state.tab==='recent'?'История просмотров пуста':'Совпадений не найдено'));
    append(reading,article(now()));append(main,index,reading);append(body,hero,tabs,main);host.replaceChildren(body);
    for(const el of $$('[data-scroll-key]',host)){const prev=scrollMemory.get(el.dataset.scrollKey);if(prev){el.scrollTop=prev.top;el.scrollLeft=prev.left;}}
    if(key){const el=$$('[data-focus-key]',host).find(x=>x.dataset.focusKey===key);if(el){el.focus({preventScroll:true});if(caret!==null)try{el.setSelectionRange(caret,caret);}catch{}}}
    if(window.scrollY!==pageY||window.scrollX!==pageX)window.scrollTo(pageX,pageY);
    $('#topics-count').textContent=articles().length+' TOPICS';
  }
  const win=$('#guide-window');
  $('#preset').onchange=e=>{state.preset=e.target.value;win.dataset.preset=state.preset;win.style.width='';win.style.height='';render();};
  $('#stress').onchange=e=>{state.stress=e.target.checked;render();status(state.stress?'65 ТЕСТОВЫХ ТЕМ':'25 ТЕМ');};
  $('#empty').onchange=e=>{state.empty=e.target.checked;render();status(state.empty?'КАТАЛОГ ПУСТ':'ЛОКАЛЬНЫЕ МАТЕРИАЛЫ');};
  $('#reset').onclick=()=>{
    Object.assign(state,{selected:'start',query:'',tab:'all',category:'Все темы',recent:['start'],stress:false,empty:false,preset:'default',closed:false,minimized:false});
    state.favorites.clear();$('#stress').checked=false;$('#empty').checked=false;$('#preset').value='default';
    win.dataset.preset='default';win.style.cssText='';win.hidden=false;win.classList.remove('is-minimized');$('#closed-panel').hidden=true;
    render({resetScroll:true});status('ДЕМО СБРОШЕНО');
  };
  $('#close').onclick=()=>{win.hidden=true;$('#closed-panel').hidden=false;};
  $('#reopen').onclick=()=>{win.hidden=false;$('#closed-panel').hidden=true;status('ОКНО ОТКРЫТО');};
  $('#minimize').onclick=()=>{state.minimized=!state.minimized;win.classList.toggle('is-minimized',state.minimized);};
  $('#maximize').onclick=()=>{const next=state.preset==='wide'?'default':'wide';state.preset=next;win.dataset.preset=next;win.style.width='';win.style.height='';$('#preset').value=next;render();};
  const bar=$('#titlebar'),stage=$('#stage');let drag=null;
  bar.addEventListener('pointerdown',e=>{
    if(e.button!==0||e.target.closest('button'))return;
    const r=win.getBoundingClientRect(),s=stage.getBoundingClientRect();
    drag={pointer:e.pointerId,dx:e.clientX-r.left,dy:e.clientY-r.top};
    win.style.position='absolute';win.style.left=r.left-s.left+'px';win.style.top=r.top-s.top+'px';
    bar.setPointerCapture(e.pointerId);
  });
  bar.addEventListener('pointermove',e=>{
    if(!drag||e.pointerId!==drag.pointer)return;
    const r=stage.getBoundingClientRect();
    win.style.left=Math.max(0,Math.min(r.width-win.offsetWidth,e.clientX-r.left-drag.dx))+'px';
    win.style.top=Math.max(0,Math.min(r.height-win.offsetHeight,e.clientY-r.top-drag.dy))+'px';
  });
  bar.addEventListener('pointerup',()=>drag=null);bar.addEventListener('pointercancel',()=>drag=null);
  if('ResizeObserver' in window)new ResizeObserver(()=>win.classList.toggle('is-compact',win.clientWidth<595)).observe(win);
  render();status('ГОТОВО К РАБОТЕ');
})();