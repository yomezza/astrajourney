// This adds *only* the approved studies v13 overlay and a chat preview entry.
// All other module markup, CSS and interactions are the ORIGINAL web client.
import { Modules } from '/ui/runtime.js';
import { Session } from '/core/session.js';
import { I18n } from '/core/i18n.js';
import { api } from '/core/api.js';
import { world, loadWorld } from '/core/world-store.js';
import { loadPlanet } from '/features/planet/loader.js';
import { mountColonyCatalog, clearColonyCatalog } from '/mock/colony-catalog.js';

// Historical source snapshots retain their original title; the current mirror
// presents the current product name while preserving their source files.
function showProductName(){
  document.title=document.title.replace(/cosmosimulator/ig,'Astra Journey');
  for(const root of document.querySelectorAll('.apphead .brand, footer')){
    const walker=document.createTreeWalker(root,NodeFilter.SHOW_TEXT);
    for(let node;node=walker.nextNode();){node.nodeValue=node.nodeValue.replace(/cosmosimulator/ig,'Astra Journey');}
  }
}
if(document.readyState==='loading')document.addEventListener('DOMContentLoaded',showProductName,{once:true});
else showProductName();

let studiesRoot;
function openResearch() {
  if (!studiesRoot || !studiesRoot.isConnected) {
    studiesRoot = Modules.create('studies-v13-mock', 'Исследования', 1100, 730);
    studiesRoot.classList.add('mock-prototype-contents');
    const frame = document.createElement('iframe');
    frame.className = 'mock-prototype-frame';
    frame.title = 'Исследования — согласованный интерактивный прототип v13';
    frame.src = '/prototype/studies/index.html?embed=1';
    studiesRoot.append(frame);
  }
  Modules.open('studies-v13-mock');
  document.querySelectorAll('#sidebar [data-open="studies"]').forEach(el => el.classList.add('module-open'));
}
async function addChatEntry() {
  const sidebar=document.querySelector('#sidebar');
  const guide=sidebar?.querySelector('[data-open="guide"]');
  const game=document.querySelector('#game');
  const stats=game?.querySelector('.overview-stats');
  if(!guide||!stats||sidebar.querySelector('[data-chat-open]'))return;
  // The button is first in the bottom group, immediately above Guide.
  const button=document.createElement('button');
  button.className='sidebar-bottom chat-bottom';
  guide.classList.remove('sidebar-bottom');
  button.type='button';button.dataset.chatOpen='true';button.dataset.nav='Чат';
  button.setAttribute('aria-label','Чат');
  const icon=document.createElement('span');icon.className='nav-icon';icon.innerHTML='<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M4 5h16v12H9l-5 3V5zM8 9h8M8 13h5"/></svg>';
  const label=document.createElement('span');label.className='nav-label';label.textContent='Чат';
  button.append(icon,label);guide.before(button);
  // A single dock slot immediately under the original overview stats.
  const grid=document.createElement('div');grid.className='home-lower-grid';grid.id='home-lower-grid';grid.hidden=false;
  const slot=document.createElement('section');slot.id='chat-home-slot';slot.hidden=true;slot.setAttribute('aria-label','Чат на главной странице');
  const news=document.createElement('section');news.id='news-home-slot';news.setAttribute('aria-label','Голографические новости');
  grid.append(slot,news);stats.after(grid);
  // Independent local news feed, available even if chat is never opened.
  import('/mock/news/news.js').then(({mountNewsWidget})=>mountNewsWidget(news))
    .catch(error=>console.error('[Astra Journey News]',error));
  // Chat has its own draggable/resizable shell, not a nested Modules window.
  const floating=document.createElement('section');floating.id='chat-floating';floating.className='chat-floating';floating.hidden=true;
  floating.setAttribute('role','dialog');floating.setAttribute('aria-label','Чат');
  floating.tabIndex=-1;document.body.append(floating);
  // Extract ONLY the chat markup. Do not import its demo page/header/sidebar.
  const response=await fetch('/mock/chat-fragment.html');
  if(!response.ok)throw Error('Не удалось загрузить интерфейс чата');
  const html=await response.text();
  const template=document.createElement('template');template.innerHTML=html;
  document.body.append(template.content);
  floating.append(document.getElementById('chat-root'));
  await import('/mock/chat-native.js');
}

// Capture the real Studies button, avoiding simultaneous creation of the old
// Studies window. The original studies source remains in original-web unchanged.
document.addEventListener('click', event => {
  const button=event.target.closest?.('[data-open="studies"]');
  if(!button)return;
  event.preventDefault();
  event.stopImmediatePropagation();
  openResearch();
},true);
// The embedded studies preview opens the ORIGINAL galaxy map for its star.
window.addEventListener('message', event => {
  if(event.origin !== location.origin || event.data?.type !== 'astra-journey-demo:show-star')return;
  const frame=document.querySelector('#window-studies-v13-mock iframe');
  if(event.source !== frame?.contentWindow)return;
  const star=Number(event.data.star);
  if(!Number.isInteger(star) || star<1 || star>80)return;
  Modules.open('galaxy');
  window.dispatchEvent(new CustomEvent('showstar',{detail:star}));
});
window.addEventListener('moduleclose', ({ detail }) => {
  if(detail==='studies-v13-mock')
    document.querySelectorAll('#sidebar [data-open="studies"]').forEach(el=>el.classList.remove('module-open'));
});
const bootChat=()=>addChatEntry().catch(err=>{
  console.error('[Astra Journey Chat] '+err.message,err);
  const nav=document.querySelector('#sidebar [data-chat-open]');
  if(nav){nav.disabled=true;nav.title='Ошибка загрузки чата';}
});
if(document.readyState==='loading')document.addEventListener('DOMContentLoaded',bootChat);
else bootChat();

// Demo-only UI/UX component laboratory; original sidebar and modules stay untouched.
const bootUiLab=()=>import('/mock/ui-lab/lab.js')
  .then(({initUiLab})=>initUiLab())
  .catch(error=>console.error('[Astra Journey UI/UX]',error));
if(document.readyState==='loading')document.addEventListener('DOMContentLoaded',bootUiLab);
else bootUiLab();

// Reuse the same catalogue component as production, with existing demo RAM API.
// The archived original-web code is intentionally left untouched.
const colonyCSS=document.createElement('link');
colonyCSS.rel='stylesheet';colonyCSS.href='/mock/colony-catalog.css';
document.head.append(colonyCSS);
let colonyTicket=0, colonyObserver=null, colonyRendering=false;
async function openColonyCatalogue(background=false){
  const ticket=++colonyTicket, session=Session.epoch;
  try{
    const [data, planet]=await Promise.all([api('planet/projects'),loadPlanet()]);
    if(!world.data)await loadWorld().catch(()=>null);
    if(ticket!==colonyTicket||session!==Session.epoch||!Session.signedIn)return;
    if(background&&!Modules.isOpen('colonies'))return;
    const root=Modules.create('colonies','Колонии',950,700);
    colonyRendering=true;
    try{
      mountColonyCatalog(root,data,{
        language:()=>I18n.language,
        assetBase:'/',
        starName:id=>world.stars.get(Number(id))?.name||'',
        translateKind:kind=>I18n.t(kind),
        loadSystem:id=>api('system?id='+encodeURIComponent(id)),
        resourceName:id=>Session.data.expedition?.resources?.find(r=>r.id===id)?.name||id,
        onProject:id=>planet.project(id),
        onSurface:(star,site)=>planet.open(star,site),
        onVisit:()=>import('/features/planet/visit.js').then(x=>x.openVisit()),
        onRefund:async refund=>{
          try{
            await api('planet/action',{action:'claim_refund',id:refund.id,
              request:Array.from(crypto.getRandomValues(new Uint32Array(4)),x=>x.toString(16)).join('-')});
            await openColonyCatalogue();
          }catch(error){console.error('[Colonies]',error);}
        }
      });
    }finally{colonyRendering=false;}
    if(!colonyObserver){
      colonyObserver=new MutationObserver(()=>{
        if(!colonyRendering && Modules.isOpen('colonies') &&
          root.isConnected && !root.querySelector('.colony-catalog-layout'))
          openColonyCatalogue(true).catch(console.error);
      });
      colonyObserver.observe(root,{childList:true});
    }
    if(!background)Modules.open('colonies');
  }catch(error){console.error('[Astra Journey Colonies]',error);}
}
document.addEventListener('click',event=>{
  const button=event.target.closest?.('#nav-colonies');
  if(!button)return;
  event.preventDefault();event.stopImmediatePropagation();
  openColonyCatalogue();
},true);
window.addEventListener('languagechange',()=>{if(Modules.isOpen('colonies'))openColonyCatalogue(true);});
window.addEventListener('signedout',()=>{
  colonyTicket++;
  colonyObserver?.disconnect();colonyObserver=null;
  const root=document.querySelector('#window-colonies .module-content');
  if(root)clearColonyCatalog(root);
});
