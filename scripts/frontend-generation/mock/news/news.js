/* Cosmosimulator demo: single SVG news projector, shared A/B/C typography.
 * Artwork: exact Git blobs from docs/ideas/news/, not generated illustrations.
 * All data come from one immutable local news.json fixture; no real game API.
 */
import { Modules } from '/ui/runtime.js';
import { Preferences } from '/core/preferences.js';
import { I18n } from '/core/i18n.js';

const ROOT = '/mock/news/';
const ASSETS = ROOT + 'assets/';
const KEY = 'cosmosim-demo-news-variant-v1';
const VARIANTS = Object.freeze(['a', 'b', 'c']);
const overlays = new WeakMap();
const assetFiles = [
  ['news-projection-beams.svg', 'beams'],
  ['news-panel-left.svg', 'panel left-image'],
  ['news-panel-right.svg', 'panel right-image'],
  ['news-all-button.svg', 'controller'],
  ['news-projector-base.svg', 'base'],
];
const iconPaths = {
  star: '<circle cx="12" cy="12" r="5"/><path d="M12 1.5v4m0 13v4m-10.5-10.5h4m13 0h4M4.5 4.5l3 3m9 9 3 3M4.5 19.5l3-3m9-9 3-3"/>',
  planet: '<ellipse cx="12" cy="12" rx="7" ry="7.2"/><path d="M2 17.5c3-5 16-10 20-9.5M3 17.8c4 1.8 12-1 18-8.4"/>',
  market: '<path d="M3 19.5h18M5 16V11m4 5V6m4 10V9m4 7V3m4 13V8"/><path d="M4 8.5 10 5l5 2 5-4"/>',
  offline: '<circle cx="12" cy="12" r="8"/><path d="M6 18 18 6"/>',
};
const translations = {
  ru: {
    widget: 'Голографические новости',
    variant: 'Отображение новостей',
    variants: { a:'A — Голографическая типографика', b:'B — Информационный терминал', c:'C — Объёмная сводка' },
    all: 'Все новости', uplink: 'UPLINK', empty:'Сигнал отсутствует', emptyKind:'НЕТ СОБЫТИЙ',
    wait:'Подключение к тестовому источнику…', none:'В тестовом источнике нет новостей.',
    error:'Не удалось загрузить тестовые новости.', retry:'Повторить',
    demo:'ВЫМЫШЛЕННЫЕ СОБЫТИЯ · ДЕМО', record:'Тестовая новость', count:'Всего',
    noSummary:'Без описания', opened:'Окно всех новостей', unavailable:'Новости не получены.',
  },
  en: {
    widget: 'Holographic news',
    variant: 'News presentation',
    variants: { a:'A — Holographic typography', b:'B — Information terminal', c:'C — Volumetric briefing' },
    all: 'All news', uplink:'UPLINK', empty:'No signal', emptyKind:'NO EVENTS',
    wait:'Connecting to demo news source…', none:'No news are available in the demo source.',
    error:'Unable to load demo news.', retry:'Retry',
    demo:'FICTIONAL EVENTS · DEMO', record:'Demo news item', count:'Total',
    noSummary:'No description', opened:'All news window', unavailable:'News are unavailable.',
  }
};
const el = (tag, className, text) => {
  const node = document.createElement(tag);
  if(className)node.className = className;
  if(text !== undefined)node.textContent = String(text);
  return node;
};
const language = () => I18n.language === 'en' ? 'en' : 'ru';
function testScenario() {
  const params = new URLSearchParams(location.search);
  const value = params.get('news_case') || 'normal';
  return ['normal','long','nodesc','one','empty','error'].includes(value) ? value : 'normal';
}
function scenarioItems(items, scenario) {
  const byID = id => items.find(item => item.id === id);
  if(scenario === 'empty') return [];
  if(scenario === 'one') return items.slice(0,1);
  if(scenario === 'long') return [byID('mock-news-008'),byID('mock-news-002')].filter(Boolean);
  if(scenario === 'nodesc') return [byID('mock-news-009'),byID('mock-news-002')].filter(Boolean);
  return items;
}
function image(file, role) {
  const img=el('img','news-layer '+role);
  img.src=ASSETS+file;
  img.alt='';
  img.draggable=false;
  img.loading='eager';
  img.setAttribute('aria-hidden','true');
  return img;
}
function iconMarkup(kind){
  // Same fixed vector paths as the artist's shared demo; kind is looked up,
  // never interpolated into SVG markup from fixture/user text.
  return '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.35" stroke-linecap="round" stroke-linejoin="round" focusable="false" aria-hidden="true">'+
    (iconPaths[kind] || iconPaths.offline)+'</svg>';
}
function makePanel(side) {
  const track=el('div','news-float-track '+side);
  const article=el('article','news-copy-surface');
  article.setAttribute('aria-live','off');
  const head=el('div','news-story-head');
  const pict=el('span','news-story-icon');
  pict.setAttribute('aria-hidden','true');
  const kind=el('span','news-story-kind');
  head.append(pict,kind);
  const heading=el('h3','news-story-heading');
  const desc=el('p','news-story-desc');
  const time=el('time','news-story-time');
  article.append(head,heading,desc,time);
  track.append(article);
  return {track,article,kind,pict,heading,desc,time};
}
function dateText(value,lang){
  const date=new Date(value);
  if(!Number.isFinite(date.getTime()))return '—';
  return new Intl.DateTimeFormat(lang==='en'?'en-GB':'ru-RU',{
    day:'2-digit',month:'short',hour:'2-digit',minute:'2-digit',
    timeZone:'UTC'
  }).format(date)+' UTC';
}
function isValid(item) {
  return item && typeof item.id==='string' &&
    typeof item.published_at==='string' &&
    typeof item.ru?.title==='string' &&
    typeof item.en?.title==='string' &&
    typeof item.category==='string';
}
export function mountNewsWidget(slot){
  if(!slot)throw Error('Missing news-home-slot');
  if(overlays.has(slot)&&overlays.get(slot).root.isConnected)return overlays.get(slot);
  const controller=new AbortController();
  const scenario=testScenario();
  let variant=Preferences.read(KEY,'b',value=>VARIANTS.includes(value));
  let allNews=[],visible=[],state='loading',allWindow=null;
  const root=el('section','news-hologram-widget variant-'+variant);
  root.setAttribute('aria-label',translations[language()].widget);
  root.dataset.variant=variant;
  root.dataset.demo='true';
  const bar=el('div','news-controlbar');
  const variantLabel=el('label','news-selector-label');
  const caption=el('span','news-selector-caption');
  const select=el('select','news-variant-select');
  select.id='demo-news-variant';
  for(const id of VARIANTS){
    const option=el('option',null);
    option.value=id;select.append(option);
  }
  select.value=variant;
  variantLabel.append(caption,select);
  const mark=el('small','news-demo-badge');
  bar.append(variantLabel,mark);
  const stage=el('div','news-stage');
  stage.setAttribute('role','group');
  for(const [file,role] of assetFiles)stage.append(image(file,role));
  const panels=['left','right'].map(side=>makePanel(side));
  for(const item of panels)stage.append(item.track);
  const action=el('button','news-all-news');
  action.type='button';
  const actionText=el('span','news-all-label');
  const uplink=el('small','news-uplink');
  action.append(actionText,uplink);stage.append(action);
  const foot=el('div','news-signal');
  foot.setAttribute('role','status');
  foot.setAttribute('aria-live','polite');
  const retry=el('button','news-retry');
  retry.type='button';retry.hidden=true;
  foot.append(el('span','news-signal-text'),retry);
  root.append(bar,stage,foot);
  // This is the one and only root. Changes of variant never rebuild this tree.
  slot.replaceChildren(root);
  overlays.set(slot,{root,get variant(){return variant;},get items(){return visible;},refresh:load});
  function setVariant(next){
    if(!VARIANTS.includes(next)||next===variant)return;
    root.classList.replace('variant-'+variant,'variant-'+next);
    root.dataset.variant=next;
    variant=next;
    select.value=next;
    Preferences.write(KEY,next);
  }
  select.addEventListener('change',()=>setVariant(select.value),{signal:controller.signal});
  action.addEventListener('click',openAll,{signal:controller.signal});
  retry.addEventListener('click',load,{signal:controller.signal});
  window.addEventListener('languagechange',render,{signal:controller.signal});
  window.addEventListener('pagehide',()=>controller.abort(),{once:true,signal:controller.signal});
  function updatePanel(panel,item,lang){
    const labels=translations[lang];
    const empty=!item;
    const text=item?.[lang];
    panel.track.classList.toggle('is-empty',empty);
    panel.track.classList.toggle('no-description',empty||!text.summary);
    panel.track.classList.toggle('is-long',!!text&&text.title.length>48);
    panel.kind.textContent=empty?labels.emptyKind:text.category;
    panel.pict.innerHTML=iconMarkup(empty?'offline':item.icon);
    panel.heading.textContent=empty?labels.empty:text.title;
    panel.heading.title=empty?'':text.title;
    panel.desc.textContent=empty?'':text.summary;
    panel.time.textContent=empty?'—':dateText(item.published_at,lang);
    if(empty)panel.time.removeAttribute('datetime');
    else panel.time.dateTime=item.published_at;
    panel.article.setAttribute('aria-label',empty?labels.empty:
      text.category+': '+text.title+(text.summary?'. '+text.summary:''));
  }
  function render(){
    const lang=language(),labels=translations[lang];
    root.setAttribute('aria-label',labels.widget);
    slot.setAttribute('aria-label',labels.widget);
    caption.textContent=labels.variant;
    mark.textContent=labels.demo;
    for(const option of select.options)option.textContent=labels.variants[option.value];
    actionText.textContent=lang==='ru'?'Все\nновости':'All\nnews';
    actionText.style.whiteSpace='pre-line';
    uplink.textContent=labels.uplink;
    action.setAttribute('aria-label',labels.all);
    select.setAttribute('aria-label',labels.variant);
    const message=state==='loading'?labels.wait:state==='error'?labels.error:
      visible.length===0?labels.none:'';
    foot.querySelector('.news-signal-text').textContent=message;
    retry.hidden=state!=='error';
    retry.textContent=labels.retry;
    panels.forEach((panel,i)=>updatePanel(panel,visible[i]||null,lang));
    if(allWindow?.isConnected&&Modules.isOpen('news-all-demo'))renderAllWindow();
  }
  function renderAllWindow(){
    if(!allWindow?.isConnected)return;
    const lang=language(),labels=translations[lang];
    Modules.title('news-all-demo',labels.all);
    allWindow.classList.add('news-demo-window');
    const head=el('header','news-list-heading');
    head.append(el('h2',null,labels.all),el('small','news-list-notice',labels.demo));
    const listing=el('div','news-list-entries');
    // The full fixture is shown in the news window. The 'one' and 'empty'
    // stress modes explicitly model an inventory with fewer available items.
    const listItems=['one','empty'].includes(scenario)?visible:allNews;
    if(state!=='ready'||!listItems.length){
      listing.append(el('p','news-list-empty',state==='error'?labels.error:labels.none));
    }else for(const item of listItems){
      const data=item[lang];
      const card=el('article','news-list-item');
      card.dataset.newsId=item.id;
      const top=el('div','news-list-meta');
      const category=el('span',null,data.category);
      const time=el('time',null,dateText(item.published_at,lang));
      time.dateTime=item.published_at;
      top.append(category,time);
      card.append(top,el('h3',null,data.title));
      if(data.summary)card.append(el('p',null,data.summary));
      listing.append(card);
    }
    allWindow.replaceChildren(head,listing);
  }
  function openAll(){
    // Modules manages focus, drag, resize and close. No nested home or iframe.
    if(!allWindow||!allWindow.isConnected){
      allWindow=Modules.create('news-all-demo',translations[language()].all,820,680,{private:false});
      allWindow.classList.add('news-demo-window');
    }
    renderAllWindow();
    Modules.open('news-all-demo');
  }
  async function load(){
    state='loading';render();
    try{
      if(scenario==='error')throw Error('simulated local news fixture failure');
      const response=await fetch(ROOT+'news.json',{cache:'no-store',signal:controller.signal});
      if(!response.ok)throw Error('HTTP '+response.status);
      const result=await response.json();
      if(!result?.fictional||!Array.isArray(result.items))throw Error('Invalid demo news format');
      if(!result.items.every(isValid))throw Error('Invalid news item in fixture');
      const ids=new Set(result.items.map(x=>x.id));
      if(ids.size!==result.items.length)throw Error('Duplicate news ID');
      allNews=result.items.slice().sort((a,b)=>
        Date.parse(b.published_at)-Date.parse(a.published_at));
      // Same stable objects and IDs under all three visual treatments.
      visible=scenarioItems(allNews,scenario);
      state='ready';
    }catch(error){
      if(controller.signal.aborted)return;
      console.warn('[News hologram] Demo fixture unavailable:',error);
      state='error';allNews=[];visible=[];
    }
    if(!controller.signal.aborted)render();
  }
  render();
  load();
  return overlays.get(slot);
}
