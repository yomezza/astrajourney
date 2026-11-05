// This adds *only* the approved studies v13 overlay and a chat preview entry.
// All other module markup, CSS and interactions are the ORIGINAL web client.
import { Modules } from '/ui/runtime.js';

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
    .catch(error=>console.error('[Cosmosimulator News]',error));
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
  if(event.origin !== location.origin || event.data?.type !== 'cosmosimulator-demo:show-star')return;
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
  console.error('[Cosmosimulator Chat] '+err.message,err);
  const nav=document.querySelector('#sidebar [data-chat-open]');
  if(nav){nav.disabled=true;nav.title='Ошибка загрузки чата';}
});
if(document.readyState==='loading')document.addEventListener('DOMContentLoaded',bootChat);
else bootChat();

// Demo-only UI/UX component laboratory; original sidebar and modules stay untouched.
const bootUiLab=()=>import('/mock/ui-lab/lab.js')
  .then(({initUiLab})=>initUiLab())
  .catch(error=>console.error('[Cosmosimulator UI/UX]',error));
if(document.readyState==='loading')document.addEventListener('DOMContentLoaded',bootUiLab);
else bootUiLab();
