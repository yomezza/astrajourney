
/* Chat Service UI prototype — isolated state, no network requests and no Chat WS. */
(()=>{
'use strict';
const $=id=>document.getElementById(id);
const el=(tag,cls,txt)=>{const n=document.createElement(tag);if(cls)n.className=cls;if(txt!==undefined)n.textContent=String(txt);return n};
const btn=(label,fn,cls='chat-small-btn')=>{const b=el('button',cls,label);b.type='button';b.addEventListener('click',fn);return b};
const tabs=[['favorites','★ Избранное'],['global','Global'],['planet','Planet'],['sector','Sector'],['corporation','Corporation'],['private','Private']];
const sid=id=>'cosmosim-chat-demo-commander-'+id;
// Authoritative user lookup belongs to the backend. This is strictly the local mock roster.
const basePilots=['Commander','Elena','NovaScout','Vega','StarFox','Atlas','Navigator'];
// 74 simulated online players to match the demo server footer; two more are offline.
const onlinePilots=[...basePilots,...Array.from({length:67},(_,i)=>'Pilot_'+String(i+1).padStart(3,'0'))];
const mockPilots=[...onlinePilots,'Sirius','Iona'];
const pilotCompanies={Commander:'NOVA',Elena:'NOVA',NovaScout:'NOVA',Vega:'NOVA',StarFox:'FREE',Atlas:'ATLAS',Navigator:'ORION',Sirius:'NOVA',Iona:'ORION'};
const pilotInfo=name=>({nick:name,corp:pilotCompanies[name]||(['FREE','ORION','ATLAS','NOVA'][Number(name.split('_')[1]||0)%4]),online:onlinePilots.includes(name)});
function resolvePilot(callsign){return mockPilots.find(p=>p.toLocaleLowerCase('ru')===String(callsign).trim().toLocaleLowerCase('ru'))||null}
function validateInvite(name,conversation){
 if(!String(name).trim())return 'Укажите позывной игрока.';
 const pilot=resolvePilot(name);
 if(!pilot)return 'Пользователя с таким позывным не существует.';
 if(pilot==='Commander')return 'Нельзя отправить приглашение самому себе.';
 if(conversation?.people.some(p=>p.toLocaleLowerCase('ru')===pilot.toLocaleLowerCase('ru')))return 'Этот игрок уже участвует в беседе.';
 return null;
}

let prefs={opacity:100,docked:false};try{prefs={opacity:Number(localStorage.getItem(sid('opacity'))) || 100,docked:localStorage.getItem(sid('dock'))==='true'}}catch{}
const chat={isOpen:false,docked:false,tab:'global',conversation:null,folder:null,settings:false,search:false,query:'',onPlanet:false,availability:'online',muted:false,opacity:Math.max(40,Math.min(100,prefs.opacity)),drafts:{},scrolls:{},replies:{},menuMessage:null,modalAction:null,seq:500,sendEvents:{},newGlobal:true,deletedInvitations:[],
  folders:[{id:'routes',title:'Маршруты'},{id:'useful',title:'Полезное'}],
  favorites:[
    {id:'fav1',folder:'routes',kind:'copy',source:'Global',author:'Navigator [ORION]',original:'14:21',saved:'14:29',text:'Безопасный путь к Эдему: Альтаир → Лира → Эдем. Проверьте запас топлива.'},
    {id:'fav2',folder:'useful',kind:'copy',source:'Corporation',author:'Elena [NOVA]',original:'13:55',saved:'14:03',text:'После обновления рынка цены на титан стабилизировались. Стоит сравнить три станции.'},
    {id:'fav3',folder:null,kind:'note',source:'Личная заметка',author:'Commander',original:'14:17',saved:'14:17',text:'Напомнить себе: пополнить водород перед следующей экспедицией.'},
    {id:'fav4',folder:'routes',kind:'note',source:'Личная заметка',author:'Commander',original:'вчера',saved:'вчера',text:'Исследовать сектор 12 и проверить месторождение A-12.'}
  ],
  conversations:[
    {id:'dm-elena',type:'dm',name:'Elena',people:['Commander','Elena'],unread:2,status:'active',owner:'Commander'},
    {id:'group-pilots',type:'group',name:'Пилоты Эдема',people:['Commander','Elena','Navigator','Atlas'],unread:3,status:'active',owner:'Commander'},
    {id:'dm-atlas',type:'dm',name:'Atlas',people:['Commander','Atlas'],unread:0,status:'active',owner:'Atlas'}
  ],
  invitations:[{id:'invite1',from:'NovaScout',type:'group',title:'Экспедиция Восток',expires:'через 6 дней'}],
  messages:{
    global:[
      {id:'g1',author:'Navigator',corp:'ORION',time:'14:20',text:'Кто-нибудь уже проверял новый торговый маршрут через Лиру?'},
      {id:'g2',author:'Elena',corp:'NOVA',time:'14:22',text:'Проверяла вчера. Переход безопасный, но топливо лучше взять с запасом.'},
      {id:'g3',author:'Atlas',corp:'ATLAS',time:'14:24',text:'На рынке Эдема сейчас выгодно покупать медь.'},
      {id:'g4',author:'Commander',corp:'NOVA',time:'14:26',text:'Спасибо! Сначала загляну на станцию и обновлю оборудование.',own:true},
      {id:'g5',author:'StarFox',corp:'FREE',time:'14:27',text:'🚀 Кто летит на Альтаир? Есть свободный транспорт.'},
      {id:'g6',author:'Vega',corp:'NOVA',time:'14:29',text:'Ресурсы в секторе A-12 скоро закончатся. Проверяйте залежи перед отправкой.'},
      {id:'g7',author:'Navigator',corp:'ORION',time:'14:31',text:'Справка по маршрутам доступна в руководстве пилота. Удачных полётов!'}
    ],
    planet:[
      {id:'p1',author:'Elena',corp:'NOVA',time:'14:21',sector:7,text:'Начинаем развёртывание солнечных панелей.'},
      {id:'p2',author:'StarFox',corp:'FREE',time:'14:23',sector:12,text:'В секторе 12 много каменистого грунта.'},
      {id:'p3',author:'Atlas',corp:'ATLAS',time:'14:25',sector:4,text:'Колониальный транспорт прибыл к площадке.'},
      {id:'p4',author:'Commander',corp:'NOVA',time:'14:27',sector:12,text:'Проверю ближайшее месторождение.',own:true}
    ],
    sector:[
      {id:'s1',author:'StarFox',corp:'FREE',time:'14:22',text:'Кто-нибудь видит рудное обнажение у северной границы?'},
      {id:'s2',author:'Commander',corp:'NOVA',time:'14:23',text:'Да, рядом с каменистой грядой.',own:true},
      {id:'s3',author:'Elena',corp:'NOVA',time:'14:25',text:'Я поставлю там маркер для команды.'}
    ],
    corporation:[
      {id:'c1',author:'Elena',corp:'NOVA',time:'14:02',text:'Коллеги, обновила график перевозок на сегодня.'},
      {id:'c2',author:'Commander',corp:'NOVA',time:'14:05',text:'Принято, спасибо. @all проверьте свои задачи.',own:true},
      {id:'c3',author:'NovaScout',corp:'NOVA',time:'14:06',text:'Сбор ресурсов на Эдеме идёт по плану.'},
      {id:'c4',author:'Vega',corp:'NOVA',time:'14:12',text:'Кто может отправить 20 единиц водорода?'},
      {id:'c5',author:'Elena',corp:'NOVA',time:'14:14',text:'@Vega, передам после завершения добычи.'}
    ],
    'private:dm-elena':[
      {id:'d1',author:'Commander',corp:'NOVA',time:'13:51',text:'Привет! Когда прибываешь на станцию?',own:true,read:true},
      {id:'d2',author:'Elena',corp:'NOVA',time:'13:52',text:'Через десять минут. Могу привезти водород.'},
      {id:'d3',author:'Commander',corp:'NOVA',time:'13:53',text:'Отлично, возьми 20 единиц, пожалуйста.',own:true,read:true},
      {id:'d4',author:'Elena',corp:'NOVA',time:'14:29',text:'Я уже у причала. Забирай груз 👋'},
      {id:'d5',author:'Elena',corp:'NOVA',time:'14:32',text:'И ещё: посмотри сообщение в корпоративном чате.'}
    ],
    'private:group-pilots':[
      {id:'gp0',type:'system',time:'13:10',text:'Elena присоединилась к беседе'},
      {id:'gp1',author:'Commander',corp:'NOVA',time:'13:12',text:'Добро пожаловать в группу! Планируем вылазку к Альтаиру.',own:true,read:true},
      {id:'gp2',author:'Navigator',corp:'ORION',time:'13:22',text:'Маршрут построен. Переход займёт примерно 35 секунд.'},
      {id:'gp3',author:'Elena',corp:'NOVA',time:'14:28',text:'Когда стартуем?'},
      {id:'gp4',author:'Atlas',corp:'ATLAS',time:'14:30',text:'Я готов. Нужно только дозаправиться.'},
      {id:'gp5',author:'Navigator',corp:'ORION',time:'14:34',text:'Отправил координаты в личные сообщения.'}
    ],
    'private:dm-atlas':[
      {id:'da1',author:'Atlas',corp:'ATLAS',time:'11:40',text:'Есть предложение по обмену титаном.'},
      {id:'da2',author:'Commander',corp:'NOVA',time:'11:42',text:'Давай обсудим после экспедиции.',own:true,read:false}
    ]
  }
};
// Only test UI state; no moderation commands are sent anywhere.
chat.moderationRole='owner';
chat.moderationPage='members';
chat.mutes={Vega:{duration:'30 минут',actor:'Elena',reason:'Флуд в корпоративном чате',until:Date.now()+25*60*1000}};
chat.moderationAudit=[{time:'14:24',actor:'Elena',action:'Ограничение отправки',target:'Vega',detail:'30 минут',reason:'Флуд в корпоративном чате'}];
chat.reports=[{id:'rep01',source:'corporation',messageId:'c4',author:'Vega',from:'NovaScout',reason:'Подозрение на спам',created:'14:33',status:'open'},
 {id:'rep02',source:'corporation',messageId:'c3',author:'NovaScout',from:'Elena',reason:'Нарушение правил общения',created:'14:34',status:'open'}];
function corpRole(name){if(name==='Commander')return chat.moderationRole==='admin'?'admin':chat.moderationRole;if(name==='Elena')return 'moderator';if(name==='Sirius')return chat.moderationRole==='owner'?'member':'owner';return 'member'}
const roleLabel=role=>({owner:'Руководитель',moderator:'Модератор',member:'Участник',admin:'Администратор'}[role]||'Участник');
function isModerator(){return ['owner','moderator','admin'].includes(chat.moderationRole)}
function canModerateTarget(name){if(!isAccessible('corporation')||!isModerator()||name==='Commander')return false;
 return chat.moderationRole==='admin'||chat.moderationRole==='owner'||(chat.moderationRole==='moderator'&&corpRole(name)==='member')}
function canUnmuteTarget(name){return isModerator()&&isAccessible('corporation')&&name!=='Commander'&&!!activeMute(name)}
function activeMute(name){const v=chat.mutes[name];if(v&&v.until>Date.now())return v;if(v)delete chat.mutes[name];return null}
const corpNames=['Commander','Elena','NovaScout','Vega','Sirius'];
function addAudit(action,target,detail,reason){chat.moderationAudit.unshift({time:timestamp(),actor:'Commander',action,target,detail,reason})}
const pref=(k,v)=>{try{localStorage.setItem(sid(k),String(v))}catch{}};
const names={'global':'Global','planet':'Planet','sector':'Sector','corporation':'Corporation','private':'Private','favorites':'Избранное'};
const timestamp=()=>new Date().toLocaleTimeString('ru-RU',{hour:'2-digit',minute:'2-digit'});
const findConversation=()=>chat.conversations.find(c=>c.id===chat.conversation);
const contentKey=()=>chat.tab==='private'&&chat.conversation?'private:'+chat.conversation:chat.tab;
const currentReply=()=>chat.replies[contentKey()];
function isAccessible(tab){if(['cosmic-off','both-off','chat-off'].includes(chat.availability))return false;if(['planet','sector'].includes(tab))return chat.onPlanet&&chat.availability==='online';return true}
function tell(message){const t=$('chat-toast');t.textContent=message;t.hidden=false;clearTimeout(tell.timer);tell.timer=setTimeout(()=>t.hidden=true,3400)}
function saveScroll(){if(chat.isOpen)chat.scrolls[contentKey()]=$('chat-area').scrollTop}
function restoreScroll(last=false){const a=$('chat-area');if(last){a.scrollTop=a.scrollHeight}else a.scrollTop=chat.scrolls[contentKey()]??a.scrollHeight}
function setDock(dock){closeEmojiPicker();
 if(!chat.isOpen)return;
 closeModerationWindow();
 closePlayersWindow();hideChatStatusTooltip();hideChatReadTooltip();
 const oldScroll=$('chat-area').scrollTop;
 chat.docked=dock;pref('dock',dock);
 $('chat-floating').hidden=dock;
 $('home-lower-grid').hidden=!dock;
 $('chat-root').hidden=false;
 (dock?$('chat-home-slot'):$('chat-floating')).append($('chat-root'));
 // Move the same chat root: no extra application window is created.
 document.body.classList.toggle('chat-docked',dock);
 $('chat-dock-toggle').textContent=dock?'↗ В окно':'▣ На главную';
 $('chat-dock-toggle').title=dock?'Открепить · Плавающее окно':'Закрепить на главной';
 $('chat-dock-toggle').setAttribute('aria-label',$('chat-dock-toggle').title);
 $('chat-close').hidden=dock;
 $('chat-area').scrollTop=oldScroll;
 updateActiveNav();
 if(dock)$('chat-home-slot').scrollIntoView({behavior:'smooth',block:'center'});
}
function openChat(){
 if(chat.isOpen){if(chat.docked)$('chat-home-slot').scrollIntoView({behavior:'smooth',block:'center'});else {$('chat-floating').hidden=false;$('chat-floating').focus({preventScroll:true});}return}
 chat.isOpen=true;$('chat-root').hidden=false;
 setDock(prefs.docked===true||chat.docked);
 renderAll();
 if(!chat.docked){$('chat-floating').hidden=false;restoreScroll(true);$('chat-floating').focus({preventScroll:true})}
 updateActiveNav();
}
function closeChat(){if(chat.docked)return;closeEmojiPicker();closeModerationWindow();closePlayersWindow();hideChatStatusTooltip();hideChatReadTooltip();saveScroll();chat.isOpen=false;$('chat-floating').hidden=true;hideMenu();updateActiveNav()}
function updateActiveNav(){const nav=document.querySelector('[data-chat-open]');nav?.classList.toggle('module-open',chat.isOpen);}
function showSettings(){chat.settings=!chat.settings;$('chat-settings-panel').hidden=!chat.settings;$('chat-settings-toggle').classList.toggle('active',chat.settings);}
function showSearch(){chat.search=!chat.search;$('chat-search-panel').hidden=!chat.search;$('chat-search-toggle').classList.toggle('active',chat.search);if(chat.search)$('chat-search-input').focus();else{chat.query='';$('chat-search-input').value='';renderBody()}}
function switchTab(tab){closeEmojiPicker();closeModerationWindow();if(!isAccessible(tab)){tell('Раздел недоступен в текущем игровом контексте');return}closePlayersWindow();saveScroll();chat.tab=tab;chat.conversation=null;chat.query='';$('chat-search-input').value='';hideMenu();renderAll();restoreScroll(true)}
function renderTabs(){const host=$('chat-tabs');host.replaceChildren();for(const [id,label] of tabs){const b=btn(label,()=>switchTab(id),'chat-tab');b.setAttribute('role','tab');b.setAttribute('aria-selected',String(chat.tab===id));b.setAttribute('title',!isAccessible(id)?(['planet','sector'].includes(id)?'Нужен активный визит на планету и доступный Planet Service':'Нет соединения'):label);b.classList.toggle('active',chat.tab===id);b.disabled=!isAccessible(id)&&id!==chat.tab;if(id==='global'&&chat.newGlobal&&chat.tab!=='global')b.append(el('span','chat-dot-new'));if(id==='private'){const unread=chat.conversations.reduce((n,c)=>n+c.unread,0)+chat.invitations.length;if(unread){b.append(el('span','chat-count',unread))}}host.append(b)} }
// The dot summarizes Cosmic/Planet health; Chat Service outage is a separate failure.
const serviceIndicator={
  online:{kind:'healthy',title:'Сервис чата работает',cosmos:true,planet:true},
  'planet-off':{kind:'partial',title:'Чат работает частично',cosmos:true,planet:false},
  'cosmic-off':{kind:'partial',title:'Чат недоступен',cosmos:false,planet:true},
  'both-off':{kind:'down',title:'Чат недоступен',cosmos:false,planet:false},
  'chat-off':{kind:'down',title:'Нет соединения с чатом',cosmos:true,planet:true}
};
function renderChatStatusTooltip(){
 const data=serviceIndicator[chat.availability]||serviceIndicator.online;
 const tooltip=$('chat-status-tooltip');tooltip.replaceChildren();
 tooltip.append(el('div','chat-status-tooltip-title',data.title));
 const list=el('div','chat-status-tooltip-list');
 for(const [name,available] of [['Cosmos',data.cosmos],['Planet',data.planet]]){
  const row=el('div','chat-status-tooltip-row');
  const square=el('span','chat-service-square '+(available?'online':'offline'));
  square.setAttribute('aria-hidden','true');
  row.append(square,el('span','chat-service-name',name),el('span','chat-service-state'+(available?'':' offline'),available?'Доступен':'Недоступен'));
  list.append(row);
 }
 tooltip.append(list);
}
function renderHeader(){
 const conv=findConversation();
 $('chat-title').textContent=chat.tab==='private'&&conv?'Чат · '+conv.name:'Чат · '+names[chat.tab];
 const badge=$('chat-signal'),data=serviceIndicator[chat.availability]||serviceIndicator.online;
 badge.classList.remove('healthy','partial','down');badge.classList.add(data.kind);
 badge.setAttribute('aria-label',data.title+'; Cosmos: '+(data.cosmos?'доступен':'недоступен')+'; Planet: '+(data.planet?'доступен':'недоступен'));
 if(!$('chat-status-tooltip').hidden){renderChatStatusTooltip();positionChatStatusTooltip()}
}
function positionChatStatusTooltip(){
 const r=$('chat-signal').getBoundingClientRect(),tooltip=$('chat-status-tooltip');
 tooltip.style.left=Math.max(8,Math.min(window.innerWidth-tooltip.offsetWidth-8,r.left-4))+'px';
 tooltip.style.top=(r.bottom+tooltip.offsetHeight+9<window.innerHeight?r.bottom+8:Math.max(8,r.top-tooltip.offsetHeight-8))+'px';
}
function showChatStatusTooltip(){
 renderChatStatusTooltip();$('chat-status-tooltip').hidden=false;positionChatStatusTooltip();
}
function hideChatStatusTooltip(){$('chat-status-tooltip').hidden=true}
// A lightweight, non-native tooltip shared by all outgoing Private read dots.
function positionChatReadTooltip(target){
 const tip=$('chat-read-tooltip');if(tip.hidden||!target?.isConnected)return;
 const r=target.getBoundingClientRect();
 const w=tip.offsetWidth,h=tip.offsetHeight;
 tip.style.left=Math.max(8,Math.min(window.innerWidth-w-8,r.left-8))+'px';
 tip.style.top=(r.bottom+h+8<=window.innerHeight-8?r.bottom+7:Math.max(8,r.top-h-7))+'px';
}
function showChatReadTooltip(target,read){
 const tip=$('chat-read-tooltip');tip.replaceChildren();
 const head=el('div','chat-read-tooltip-title');
 head.append(el('span','chat-read-tooltip-indicator'+(read?' read':'')),el('span',null,read?'Сообщение прочитано':'Сообщение не прочитано'));
 tip.append(head,el('div','chat-read-tooltip-description',read?'Прочитал хотя бы один другой участник.':'Пока никто из других участников не прочитал.'));
 tip.hidden=false;tip.dataset.target=target.dataset.messageId||'';
 positionChatReadTooltip(target);
}
function hideChatReadTooltip(){$('chat-read-tooltip').hidden=true}
function addPrivateReadDot(head,message){
 const d=el('span','chat-read-dot'+(message.read?' read':''));
 const description=message.read?'Сообщение прочитано хотя бы одним другим участником':'Сообщение пока не прочитано другими участниками';
 d.setAttribute('role','img');d.tabIndex=0;
 d.setAttribute('aria-label',description);d.setAttribute('aria-describedby','chat-read-tooltip');
 d.dataset.messageId=message.id;
 d.addEventListener('mouseenter',()=>showChatReadTooltip(d,message.read));
 d.addEventListener('mouseleave',hideChatReadTooltip);
 d.addEventListener('focus',()=>showChatReadTooltip(d,message.read));
 d.addEventListener('blur',hideChatReadTooltip);
 // Tapping the indicator also reveals the explanation on touch screens.
 d.addEventListener('pointerdown',e=>{if(e.pointerType==='touch'){e.stopPropagation();showChatReadTooltip(d,message.read)}});
 head.append(d);
}
// Context-aware roster uses the same Chat state (never a separate Chat connection).
// Only public online users are listed globally. Private and corp show participants,
// including offline members. Planet/Sector require the current authoritative visit.
function playersContext(){
 if(!isAccessible(chat.tab)||chat.tab==='favorites')return null;
 if(chat.tab==='global')return {title:'Игроки · Global',description:'Игроки онлайн на сервере Иггдрасиль',people:onlinePilots.map(pilotInfo),mode:'online'};
 if(chat.tab==='planet')return {title:'Игроки · Planet',description:'Эдем-IV · игроки на поверхности',people:['Commander','Elena','StarFox','Atlas','Navigator'].map(pilotInfo),mode:'present'};
 if(chat.tab==='sector')return {title:'Игроки · Sector',description:'Эдем-IV · сектор 12',people:['Commander','Elena','StarFox'].map(pilotInfo),mode:'present'};
 if(chat.tab==='corporation')return {title:'Игроки · Corporation',description:'Корпорация NOVA · все участники',people:['Commander','Elena','NovaScout','Vega','Sirius'].map(pilotInfo),mode:'corporation'};
 if(chat.tab==='private'){
  const conv=findConversation();if(!conv)return null;
  return {title:'Участники · '+conv.name,description:conv.type==='group'?'Групповая беседа · участники':'Личный диалог · собеседники',people:conv.people.map(name=>({...pilotInfo(name),role:name===conv.owner?'Владелец':conv.type==='dm'?'Собеседник':'Участник'})),mode:'private'};
 }
 return null;
}
let playersFocusReturn=null;
function renderPlayersWindow(){
 const context=playersContext();if(!context){closePlayersWindow();return}
 $('chat-players-title').textContent=context.title;
 $('chat-players-description').textContent=context.description;
 const search=$('chat-players-query').value.trim().toLocaleLowerCase('ru');
 const people=context.people.filter(p=>p.nick.toLocaleLowerCase('ru').includes(search)).sort((a,b)=>{
  if(a.nick==='Commander')return -1;if(b.nick==='Commander')return 1;
  return (Number(b.online)-Number(a.online))||a.nick.localeCompare(b.nick,'ru',{numeric:true});
 });
 $('chat-players-total').textContent=context.people.length+' '+(context.mode==='online'?'в сети':'всего');
 $('chat-players-found').textContent='Показано: '+people.length;
 const host=$('chat-players-list');host.replaceChildren();
 if(!people.length){host.append(el('div','chat-players-empty','По вашему запросу игроки не найдены.'));return}
 for(const p of people){
  const row=el('div','chat-player-row');row.setAttribute('role','listitem');
  const dot=el('span','chat-player-presence'+(p.online?'':' offline'));
  dot.setAttribute('aria-label',p.online?'В сети':'Не в сети');dot.setAttribute('title',p.online?'В сети':'Не в сети');
  const avatar=el('div','chat-player-avatar',p.nick.slice(0,1).toLocaleUpperCase('ru'));
  const identity=el('div','chat-player-name');identity.append(el('strong',null,p.nick),el('span',null,'['+p.corp+']'));
  const role=context.mode==='private'?(p.role||'Участник'):context.mode==='corporation'?roleLabel(corpRole(p.nick)):(p.online?'В сети':'Не в сети');
  const badge=el('div','chat-player-role'+(['Владелец','Руководитель','Администратор'].includes(role)?' owner':''),role);
  if(context.mode==='corporation'&&activeMute(p.nick)&& (isModerator()||p.nick==='Commander')){const tag=el('span','chat-moderation-role',' · запрет отправки');identity.append(tag)}
  row.append(dot,avatar,identity,badge);
  if(context.mode==='corporation'&&(canModerateTarget(p.nick)||canUnmuteTarget(p.nick))){
   const manage=el('button','chat-player-manage',activeMute(p.nick)?'Снять запрет':'Ограничить');manage.type='button';manage.setAttribute('aria-label','Модерация '+p.nick);
   manage.addEventListener('click',()=>{closePlayersWindow();activeMute(p.nick)?openUnmuteDialog(p.nick):openMuteDialog(p.nick)});row.classList.add('with-manage');row.append(manage)
  }host.append(row);
 }
}
function openPlayersWindow(){
 if(!playersContext())return;
 hideMenu();hideChatStatusTooltip();hideChatReadTooltip();
 playersFocusReturn=document.activeElement;
 $('chat-players-query').value='';$('chat-players-overlay').hidden=false;
 renderPlayersWindow();$('chat-players-query').focus();
}
function closePlayersWindow(){
 if($('chat-players-overlay').hidden)return;
 $('chat-players-overlay').hidden=true;
 if(playersFocusReturn?.isConnected&&!playersFocusReturn.disabled)playersFocusReturn.focus();
 playersFocusReturn=null;
}
function playersButton(){
 const button=btn('Игроки',openPlayersWindow,'chat-small-btn chat-players-trigger');
 button.prepend(Object.assign(document.createElementNS('http://www.w3.org/2000/svg','svg'),{}));
 const svg=button.firstChild;svg.setAttribute('viewBox','0 0 24 24');svg.setAttribute('aria-hidden','true');
 for(const path of ['M16 20v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2','M9 10a4 4 0 1 0 0-8a4 4 0 0 0 0 8','M22 20v-2a4 4 0 0 0-3-3.87','M16 2.13a4 4 0 0 1 0 7.75']){
  const p=document.createElementNS('http://www.w3.org/2000/svg','path');p.setAttribute('d',path);svg.append(p)
 }
 button.title='Показать список игроков этого раздела';button.disabled=!isAccessible(chat.tab);
 button.setAttribute('aria-label','Показать список игроков');return button;
}

const moderationDurations=[['300','5 минут'],['1800','30 минут'],['3600','1 час'],['86400','24 часа'],['604800','7 дней']];
function openMuteDialog(name){if(!canModerateTarget(name))return;
 showModal({title:'Ограничить отправку · '+name,description:'Запрет действует только в Corporation [NOVA]. Игрок сможет читать и искать историю.',fields:[
 {name:'duration',label:'Срок запрета',type:'select',options:moderationDurations,value:'1800'},
 {name:'reason',label:'Причина (обязательно)',placeholder:'Укажите причину ограничения',maxLength:200}],ok:'Применить запрет',submit:vals=>{
  if(!vals.reason){modalError('Укажите причину ограничения.','reason');return false}
  if(!canModerateTarget(name))return false;
  const label=moderationDurations.find(x=>x[0]===vals.duration)?.[1]||'30 минут';
  chat.mutes[name]={duration:label,reason:vals.reason,actor:'Commander',until:Date.now()+Number(vals.duration)*1000};
  addAudit('Ограничение отправки',name,label,vals.reason);renderAll();renderModerationWindow();tell('Отправка запрещена для '+name+' на '+label)
 }});
}
function openUnmuteDialog(name){if(!isModerator()||!isAccessible('corporation')||name==='Commander'||!activeMute(name))return;
 showModal({title:'Снять ограничение · '+name,description:'Разрешить отправлять сообщения в Corporation до истечения запрета.',fields:[{name:'reason',label:'Причина снятия (обязательно)',placeholder:'Например: нарушение устранено',maxLength:200}],ok:'Снять запрет',submit:({reason})=>{
 if(!reason){modalError('Укажите причину снятия ограничения.','reason');return false}
 delete chat.mutes[name];addAudit('Досрочное снятие ограничения',name,'',reason);renderAll();renderModerationWindow();tell('Ограничение снято: '+name)
 }});
}
function openModeratorDeleteDialog(m,k){if(k!=='corporation'||!canModerateTarget(m.author))return;
 showModal({title:'Удалить сообщение · '+m.author,description:'Модераторское удаление удалит оригинал и связанные сохранённые копии. Причина обязательна.',fields:[{name:'reason',label:'Причина удаления (обязательно)',placeholder:'Опишите нарушение правил',maxLength:200}],ok:'Удалить сообщение',submit:({reason})=>{
  if(!reason){modalError('Укажите причину модераторского удаления.','reason');return false}
  if(!canModerateTarget(m.author))return false;
  chat.messages[k]=(chat.messages[k]||[]).filter(x=>x.id!==m.id);
  chat.favorites=chat.favorites.filter(f=>!(f.kind==='copy'&&f.sourceId===m.id));
  addAudit('Модераторское удаление',m.author,'ID: '+m.id,reason);
  for(const r of chat.reports){if(r.messageId===m.id&&r.source===k&&r.status==='open')r.status='resolved'}
  renderAll();renderModerationWindow();tell('Сообщение удалено; связанные копии очищены (демо)')
 }});
}
function openReportDialog(m,k){showModal({title:'Пожаловаться на сообщение',description:'Автор: '+m.author+'. Жалоба попадёт на рассмотрение администрации. Текст сообщения не копируется в журнал.',fields:[{name:'reason',label:'Причина жалобы (обязательно)',maxLength:200,placeholder:'Опишите нарушение'}],ok:'Отправить жалобу',submit:({reason})=>{
 if(!reason){modalError('Опишите причину жалобы.','reason');return false}
 if(chat.reports.some(r=>r.source===k&&r.messageId===m.id&&r.from==='Commander'&&r.status==='open')){modalError('Вы уже отправили жалобу на это сообщение.','reason');return false}
 chat.reports.unshift({id:'rep'+(++chat.seq),source:k,messageId:m.id,author:m.author,from:'Commander',reason,created:timestamp(),status:'open'});
 tell('Жалоба отправлена на проверку (демо)');renderModerationWindow()
 }});}
function openModerationWindow(){if(!isModerator()||!isAccessible('corporation'))return;
 closePlayersWindow();hideMenu();hideChatStatusTooltip();hideChatReadTooltip();
 $('chat-moderation-overlay').hidden=false;chat.moderationPage='members';renderModerationWindow();$('chat-moderation-close').focus();}
function closeModerationWindow(){$('chat-moderation-overlay').hidden=true}
function renderModerationWindow(){if($('chat-moderation-overlay').hidden)return;
 if(!isModerator()||!isAccessible('corporation')){closeModerationWindow();return}
 $('chat-moderation-subtitle').textContent='Вы: '+roleLabel(chat.moderationRole)+' · действующие полномочия (демо)';
 const tabs=[['members','Участники'],['audit','Журнал']];if(chat.moderationRole==='admin')tabs.splice(1,0,['reports','Жалобы']);
 if(!tabs.some(([k])=>k===chat.moderationPage))chat.moderationPage='members';
 const tabHost=$('chat-moderation-tabs');tabHost.replaceChildren();
 tabs.forEach(([key,name])=>{const b=el('button',chat.moderationPage===key?'active':'',name+(key==='reports'?' ('+chat.reports.filter(r=>r.status==='open'&&r.source==='corporation').length+')':''));b.type='button';b.setAttribute('role','tab');b.setAttribute('aria-selected',String(key===chat.moderationPage));b.addEventListener('click',()=>{chat.moderationPage=key;renderModerationWindow()});tabHost.append(b)});
 const body=$('chat-moderation-body');body.replaceChildren();
 if(chat.moderationPage==='members'){
  const active=corpNames.filter(x=>activeMute(x));
  const metrics=el('div','chat-moderation-metrics');for(const [caption,value] of [['Участников',corpNames.length],['Ограничений',active.length]]){const box=el('div','chat-moderation-metric');box.append(el('span',null,caption),el('b',null,String(value)));metrics.append(box)}body.append(metrics);
  body.append(el('div','chat-moderation-note','Запрет отправки не лишает игрока права читать и искать историю Corporation. Список участников и полномочия выдаёт Cosmic Service.'));
  body.append(el('div','chat-moderation-section-title','УЧАСТНИКИ КОРПОРАЦИИ'));
  for(const name of corpNames){const row=el('div','chat-moderation-row'),info=el('div','chat-moderation-row-main'),headline=el('strong',null,name);info.append(headline,el('small',null,roleLabel(corpRole(name))+(name==='Commander'?' · вы':'')));
   const active=activeMute(name);
   if(active){info.append(el('small','chat-moderation-reason','Запрет отправки · '+active.duration));if(isModerator()||name==='Commander')info.append(el('small',null,'Причина: '+active.reason));}
   const actions=el('div','chat-moderation-row-actions');
   if(active?canUnmuteTarget(name):canModerateTarget(name))actions.append(btn(active?'Снять запрет':'Ограничить',()=>active?openUnmuteDialog(name):openMuteDialog(name),'chat-small-btn'));
   row.append(info,actions);body.append(row);
  }
  if(!corpNames.some(name=>canModerateTarget(name)||canUnmuteTarget(name)))body.append(el('div','chat-moderation-empty','Нет участников, на которых распространяются ваши права.'));
 } else if(chat.moderationPage==='reports'){
  body.append(el('div','chat-moderation-note','Жалобы в демоверсии доступны только в режиме игрового администратора. В реальной игре доступ ограничен полномочиями и проверяется сервером.'));
  const reports=chat.reports.filter(r=>r.source==='corporation'&&r.status==='open');
  if(!reports.length)body.append(el('div','chat-moderation-empty','Открытых жалоб нет.'));
  for(const report of reports){const row=el('div','chat-moderation-row'),info=el('div','chat-moderation-row-main');info.append(el('strong',null,'На '+report.author+' · '+report.created),el('small',null,'От '+report.from+' · сообщение '+report.messageId),el('small','chat-moderation-reason','Причина жалобы: '+report.reason));
   const msg=chat.messages[report.source]?.find(m=>m.id===report.messageId);
   info.append(el('small',null,msg?'Оригинал: '+msg.text:'Оригинальное сообщение недоступно'));
   const actions=el('div','chat-moderation-row-actions');
   if(msg&&canModerateTarget(msg.author))actions.append(btn('Удалить',()=>openModeratorDeleteDialog(msg,report.source),'danger-action'));
   actions.append(btn('Закрыть',()=>showModal({title:'Закрыть жалобу?',description:'Сообщение останется без изменений.',ok:'Закрыть жалобу',submit:()=>{report.status='closed';addAudit('Закрытие жалобы',report.author,'ID: '+report.messageId,'Жалоба проверена');renderModerationWindow();tell('Жалоба закрыта')}}),'chat-small-btn'));
   row.append(info,actions);body.append(row)}
 } else {
  body.append(el('div','chat-moderation-note','Здесь отображаются метаданные действий. Тексты удалённых сообщений в журнал не сохраняются. Причины доступны только уполномоченным лицам.'));
  if(!chat.moderationAudit.length)body.append(el('div','chat-moderation-empty','Действий пока нет.'));
  for(const log of chat.moderationAudit){const row=el('div','chat-moderation-row'),info=el('div','chat-moderation-row-main');info.append(el('strong',null,log.action+' · '+log.target),el('small',null,log.time+' · '+log.actor+(log.detail?' · '+log.detail:'')),el('small','chat-moderation-reason','Причина: '+log.reason));row.append(info);body.append(row)}
 }
}

function renderSection(){const sec=$('chat-section-header');sec.replaceChildren();const h=el('div','chat-section-name');const acts=el('div','chat-section-actions');sec.append(h,acts);
 if(chat.tab==='global'){h.textContent='ОБЩИЙ КАНАЛ · все пилоты';}
 if(chat.tab==='planet'){h.textContent='ПЛАНЕТА · Эдем-IV · все сектора';}
 if(chat.tab==='sector'){h.textContent='СЕКТОР 12 · Эдем-IV';}
 if(chat.tab==='corporation'){h.textContent='КОРПОРАЦИЯ · [NOVA]';if(isModerator()&&isAccessible('corporation')){acts.append(btn('⚑ Модерация',openModerationWindow,'chat-small-btn'))}}
 if(chat.tab==='favorites'){h.textContent='★ ЛИЧНОЕ ИЗБРАННОЕ';acts.append(btn('+ Заметка',()=>noteModal(),'chat-small-btn'));acts.append(btn('+ Папка',()=>folderModal(),'chat-small-btn'))}
 if(chat.tab==='private'){
  const conv=findConversation();if(conv){acts.append(btn('← Назад',()=>{closePlayersWindow();saveScroll();chat.conversation=null;renderAll()},'chat-small-btn'));h.textContent=(conv.type==='group'?'ГРУППА · ':'ЛИЧНОЕ · ')+conv.name;
   acts.append(btn('⋯',()=>conversationActions(conv),'chat-small-btn'));
  }else{h.textContent='ЛИЧНЫЕ БЕСЕДЫ И ПРИГЛАШЕНИЯ';acts.append(btn('+ Беседа',()=>newConversationModal(),'chat-small-btn'))}
 }
 if(['global','planet','sector','corporation'].includes(chat.tab)||(chat.tab==='private'&&findConversation()))acts.append(playersButton());
}
function errorState(){if(chat.availability==='chat-off')return['Нет соединения с Chat Service','Черновики сохраняются в открытой вкладке. После восстановления ничего не отправится автоматически.'];if(chat.availability==='both-off')return['Cosmic Service и Planet Service недоступны','Весь пользовательский чат временно заблокирован.'];if(chat.availability==='cosmic-off')return['Cosmic Service недоступен','Весь пользовательский чат временно заблокирован.'];if(['planet','sector'].includes(chat.tab)&&chat.availability==='planet-off')return['Planet Service недоступен','Global, Corporation, Private и Избранное продолжают работать.'];if(['planet','sector'].includes(chat.tab)&&!chat.onPlanet)return['Нет активного визита на планету','Откройте настройки ⚙ и включите «Тестовый визит на планету».'];return null}
function renderBody(){hideChatReadTooltip();const host=$('chat-area');host.replaceChildren();const state=errorState();if(state){const n=el('div','chat-empty');n.append(el('b',null,state[0]),el('span',null,state[1]));host.append(n);return}
 if(chat.tab==='favorites'){renderFavorites(host);return}
 if(chat.tab==='private'&&!chat.conversation){renderConversationList(host);return}
 const k=contentKey();const messages=chat.messages[k]||[];const visible=messages.filter(m=>!chat.query||((m.text||'')+' '+(m.author||'')).toLocaleLowerCase('ru').includes(chat.query.toLocaleLowerCase('ru')));
 const feed=el('div','chat-feed');host.append(feed);
 if(!visible.length){const empty=el('div','chat-empty');empty.append(el('b',null,chat.query?'Ничего не найдено':'Сообщений пока нет'),el('span',null,chat.query?'Попробуйте изменить поисковый запрос.':'Напишите первое сообщение.'));feed.append(empty)}
 for(const m of visible)feed.append(messageNode(m,k));
 if(chat.tab==='global')chat.newGlobal=false;
 if(chat.tab==='private'&&chat.conversation){const conv=findConversation();if(conv)conv.unread=0;renderTabs()}
}
function messageNode(m,k){if(m.type==='system')return el('div','chat-system-message',m.time+' · '+m.text);
 const row=el('article','chat-message'+(m.own?' own':''));row.dataset.id=m.id;
 if(chat.tab==='planet'&&m.sector){const sec=el('span','chat-sector-no','#'+m.sector);sec.tabIndex=0;sec.title='Сектор '+m.sector+' · '+(m.sector===12?'Каменистое плато':'Поверхность Эдем-IV');row.append(sec)}
 const content=el('div','chat-msg-content');const head=el('div','chat-msg-head');head.append(el('span','chat-time',m.time),el('span','chat-author',m.author));if(m.corp)head.append(el('span','chat-corp','['+m.corp+']'));
 if(k.startsWith('private:')&&m.own)addPrivateReadDot(head,m);
 content.append(head);
 if(m.reply){const orig=(chat.messages[k]||[]).find(x=>x.id===m.reply);const rp=el('div','chat-reply-origin',orig?'↳ '+orig.author+': '+orig.text:'↳ Исходное сообщение недоступно');content.append(rp)}
 const p=el('p','chat-msg-text',m.text);content.append(p);row.append(content);
 const more=btn('⋯',e=>{},'chat-more');more.setAttribute('aria-label','Действия с сообщением '+m.author);more.title='Действия с сообщением';more.addEventListener('click',e=>{e.stopPropagation();messageMenu(m,k,more)});row.append(more);return row}
function renderFavorites(host){const bar=el('div','chat-folder-bar');const all=btn('Все',()=>{chat.folder=null;renderBody()},'chat-folder'+(chat.folder===null?' selected':''));bar.append(all);
 chat.folders.forEach(f=>bar.append(btn('▤ '+f.title,()=>{chat.folder=f.id;renderBody()},'chat-folder'+(chat.folder===f.id?' selected':''))));
 if(chat.folder){const more=btn('⋯',()=>folderOptions(more),'chat-folder chat-folder-options');more.setAttribute('aria-label','Действия с папкой');more.setAttribute('aria-haspopup','menu');more.title='Действия с папкой';bar.append(more);}
 host.append(bar);const matches=chat.favorites.filter(f=>(!chat.folder||f.folder===chat.folder)&&(!chat.query||(f.text+' '+f.source+' '+f.author).toLocaleLowerCase('ru').includes(chat.query.toLocaleLowerCase('ru'))));
 if(!matches.length){const emp=el('div','chat-empty');emp.append(el('b',null,'Записей нет'),el('span',null,'Сохраните сообщение через «⋯» или создайте заметку.'));host.append(emp)}
 for(const f of matches){const card=el('article','chat-favorite-card');const head=el('div','chat-favorite-card-head');const meta=el('span');meta.append(el('span','chat-folder-label',f.kind==='note'?'Заметка':'Копия сообщения'),document.createTextNode(' · '+f.source+' · '+f.saved));head.append(meta);
 const more=btn('⋯',()=>favoriteOptions(f,more),'chat-small-btn');head.append(more);card.append(head,el('p',null,f.text),el('span','chat-mini',f.kind==='copy'?'Автор: '+f.author+' · оригинал: '+f.original:'Личная запись · редактирование разрешено'));host.append(card)}
}
function renderConversationList(host){if(chat.invitations.length){const cap=el('div','chat-mini','ВХОДЯЩИЕ ПРИГЛАШЕНИЯ');cap.style.cssText='padding:9px 10px 4px;color:#d4ab70';host.append(cap)}
 for(const inv of chat.invitations){if(chat.query&&!`${inv.title} ${inv.from}`.toLowerCase().includes(chat.query.toLowerCase()))continue;const row=el('div','chat-invitation-row');row.append(el('span','chat-avatar','✉'));const content=el('div');content.style.flex='1';content.append(el('strong',null,inv.title),el('small','chat-mini','Приглашение от '+inv.from+' · '+inv.expires));const actions=el('div','chat-row-actions');actions.append(btn('Принять',()=>{const id='demo-'+(++chat.seq);chat.conversations.unshift({id,type:'group',name:inv.title,people:['Commander',inv.from],status:'active',owner:inv.from,unread:0});chat.messages['private:'+id]=[{id:'sys-'+chat.seq,type:'system',time:timestamp(),text:'Commander принял приглашение'}];chat.invitations=chat.invitations.filter(x=>x.id!==inv.id);renderAll();tell('Приглашение принято')},'chat-small-btn'),btn('Отклонить',()=>showModal({title:'Отклонить приглашение?',description:'Повторное приглашение в эту беседу после отказа возможно не раньше чем через 24 часа.',ok:'Отклонить',submit:()=>{chat.invitations=chat.invitations.filter(x=>x.id!==inv.id);renderAll();tell('Приглашение отклонено')}}),'chat-small-btn'));content.append(actions);row.append(content);host.append(row)}
 if(chat.conversations.length){const cap=el('div','chat-mini','БЕСЕДЫ');cap.style.cssText='padding:10px 10px 4px';host.append(cap)}
 const matches=chat.conversations.filter(c=>!chat.query||c.name.toLowerCase().includes(chat.query.toLowerCase())||(chat.messages['private:'+c.id]||[]).some(m=>(m.text||'').toLowerCase().includes(chat.query.toLowerCase())));
 if(!matches.length&&!chat.invitations.length){const emp=el('div','chat-empty','Бесед пока нет');host.append(emp)}
 for(const conv of matches){const row=el('div','chat-conversation-row');row.tabIndex=0;row.setAttribute('role','button');row.setAttribute('aria-label','Открыть '+conv.name);const avatar=el('span','chat-avatar',conv.type==='group'?'♧':conv.name[0].toUpperCase());const info=el('div');info.append(el('strong',null,conv.name+(conv.type==='group'?' · группа':'')));const list=chat.messages['private:'+conv.id]||[];const last=list.at(-1);info.append(el('small',null,last?.text||'Сообщений пока нет'));row.append(avatar,info);if(conv.unread)row.append(el('span','chat-unread',conv.unread));row.addEventListener('click',()=>openConversation(conv.id));row.addEventListener('keydown',e=>{if(e.key==='Enter'||e.key===' '){e.preventDefault();openConversation(conv.id)}});host.append(row)}
}
function openConversation(id){closeEmojiPicker();saveScroll();chat.conversation=id;chat.query='';$('chat-search-input').value='';const conv=findConversation();if(conv)conv.unread=0;renderAll();restoreScroll(true)}
function renderCompose(){const compose=$('chat-compose');const k=contentKey();const blocked=errorState();const show=chat.tab!=='favorites' && (chat.tab!=='private'||!!chat.conversation);compose.hidden=!show;if(!show)return;
 const max=chat.tab==='private'?500:256;const input=$('chat-input');input.removeAttribute('maxlength');input.value=chat.drafts[k]||'';composerCaret={start:input.value.length,end:input.value.length};input.placeholder=chat.tab==='corporation'&&chat.muted?'Отправка ограничена модератором':blocked?'Нет соединения / нет доступа':'Введите сообщение…';
 const awaiting=chat.tab==='private'&&findConversation()?.status==='pending';input.disabled=!!blocked||!!awaiting||(chat.tab==='corporation'&&chat.muted);$('chat-send').disabled=input.disabled||!input.value.trim();$('chat-emoji-trigger').disabled=input.disabled; if(input.disabled)closeEmojiPicker();if(awaiting)input.placeholder='Ожидание принятия приглашения';const info=blocked?'':awaiting?'До принятия приглашения писать нельзя.':chat.tab==='corporation'&&chat.muted?'Вы можете читать и искать сообщения, но не отправлять.':'Enter — отправка · Shift+Enter — новая строка';$('chat-compose-hint').textContent=info;
 $('chat-char-counter').textContent=codepoints(input.value)+' / '+max;const reply=currentReply();$('chat-reply-preview').hidden=!reply;if(reply){const original=(chat.messages[k]||[]).find(m=>m.id===reply);$('chat-reply-text').textContent=original?'Ответ на '+original.author+': '+original.text:'Исходное сообщение недоступно'}
}
function renderAll(){renderHeader();renderTabs();renderSection();renderBody();renderCompose();if(!$('chat-players-overlay').hidden)renderPlayersWindow()}
function send(){const k=contentKey();const input=$('chat-input');const value=input.value.trim();if(!value||input.disabled||errorState())return;
 const lim=chat.tab==='private'?500:256;if(codepoints(value)>lim){tell('Превышен лимит '+lim+' символов');return}
 const span=chat.tab==='global'?3000:chat.tab==='planet'?2000:chat.tab==='private'?1000:1000;
 const stamp=Date.now(),recent=(chat.sendEvents[chat.tab]||[]).filter(t=>stamp-t<10000);
 if(chat.tab==='private'&&recent.length>=10){tell('Лимит: 10 сообщений за 10 секунд');return}
 if(chat.tab!=='private'&&recent.length&&stamp-recent.at(-1)<span){tell('Подождите перед следующей отправкой');return}
 recent.push(stamp);chat.sendEvents[chat.tab]=recent;
 const m={id:'mock-'+(++chat.seq),author:'Commander',corp:'NOVA',time:timestamp(),text:value,own:true,read:false};if(chat.replies[k]){m.reply=chat.replies[k];delete chat.replies[k]};if(chat.tab==='planet')m.sector=12;
 (chat.messages[k]??=[]).push(m);chat.drafts[k]='';input.value='';closeEmojiPicker();$('chat-mentions').hidden=true;renderAll();restoreScroll(true)
}
function hideMenu(){const menu=$('chat-pop-menu');menu.hidden=true;menu.replaceChildren();menu.classList.remove('folder-menu');chat.menuMessage=null}
function positionMenu(source){const menu=$('chat-pop-menu');menu.hidden=false;const rect=source.getBoundingClientRect();const w=menu.offsetWidth,h=menu.offsetHeight;menu.style.left=Math.max(8,Math.min(window.innerWidth-w-8,rect.right-w))+'px';menu.style.top=(rect.bottom+h+8>window.innerHeight?Math.max(8,rect.top-h):rect.bottom+3)+'px'}
function messageMenu(m,k,source){const menu=$('chat-pop-menu');menu.replaceChildren();const item=(text,fn)=>{const action=btn(text,()=>{hideMenu();fn()},'');action.setAttribute('role','menuitem');menu.append(action)};
 item('Ответить',()=>{chat.replies[k]=m.id;renderCompose();$('chat-input').focus()});
 item('★ Сохранить в избранное',()=>{chat.favorites.unshift({id:'fav-'+(++chat.seq),folder:null,kind:'copy',source:k.startsWith('private:')?'Private · '+(findConversation()?.name||'беседа'):names[chat.tab],author:m.author+' ['+(m.corp||'')+']',original:m.time,saved:timestamp(),text:m.text,sourceId:m.id});tell('Копия сохранена в Избранное → Все')});
 if(!m.own)item('Пожаловаться',()=>openReportDialog(m,k));
 if(m.own)item('Удалить своё сообщение',()=>showModal({title:'Удалить своё сообщение?',description:'Сообщение будет удалено для всех, но ранее сохранённые копии останутся.',ok:'Удалить',submit:()=>{chat.messages[k]=chat.messages[k].filter(x=>x.id!==m.id);renderAll();tell('Ваше сообщение удалено')}}));
 else if(k==='corporation'&&canModerateTarget(m.author))item('Удалить как модератор',()=>openModeratorDeleteDialog(m,k));
 positionMenu(source)
}
function modalError(message,fieldName='name'){
 const target=$('chat-modal-error');target.textContent=message;target.hidden=false;
 const input=Array.from($('chat-modal-fields').querySelectorAll('input,select')).find(x=>x.name===fieldName);
 if(input){input.setAttribute('aria-invalid','true');input.setAttribute('aria-describedby','chat-modal-error');input.focus()}
}
function clearModalError(){const target=$('chat-modal-error');target.hidden=true;target.textContent='';$('chat-modal-fields').querySelectorAll('[aria-invalid]').forEach(x=>{x.removeAttribute('aria-invalid');x.removeAttribute('aria-describedby')})}
function showModal(config){const modal=$('chat-modal');$('chat-modal-title').textContent=config.title;$('chat-modal-desc').textContent=config.description||'';$('chat-modal-fields').replaceChildren();chat.modalAction=config;clearModalError();
 const f=$('chat-modal-fields');for(const field of config.fields||[]){const label=el('label',null,field.label);let input;if(field.type==='select'){input=el('select');for(const [v,txt] of field.options)input.add(new Option(txt,v))}else{input=el('input');input.type='text';input.maxLength=field.maxLength||140;input.placeholder=field.placeholder||''}input.name=field.name;input.value=field.value||'';input.addEventListener('input',clearModalError);input.addEventListener('change',clearModalError);label.append(input);f.append(label);if(field.userLookup){f.append(el('div','chat-modal-pilots','Тестовые игроки: Elena, NovaScout, Vega, StarFox, Atlas, Navigator.'))}}
 $('chat-modal-ok').textContent=config.ok||'Подтвердить';modal.hidden=false;f.querySelector('input,select')?.focus();if(!f.children.length)$('chat-modal-ok').focus();}
function closeModal(){chat.modalAction=null;$('chat-modal').hidden=true;clearModalError()}
function submitModal(){const config=chat.modalAction;if(!config)return;const vals={};$('chat-modal-fields').querySelectorAll('input,select').forEach(x=>vals[x.name]=x.value.trim());if(config.submit?.(vals)===false)return;closeModal()}
function folderModal(){showModal({title:'Создать папку',description:'Новая папка в личном Избранном.',fields:[{name:'title',label:'Название папки',placeholder:'Например: Торговля',maxLength:40}],ok:'Создать',submit:({title})=>{if(!title){tell('Введите название папки');return false}const id='folder-'+(++chat.seq);chat.folders.push({id,title});chat.folder=id;renderAll();tell('Папка создана')}})}
function folderOptions(source){const folder=chat.folders.find(f=>f.id===chat.folder);if(!folder)return;const menu=$('chat-pop-menu');
 if(!menu.hidden&&menu.classList.contains('folder-menu')){hideMenu();return}
 hideMenu();menu.classList.add('folder-menu');menu.setAttribute('aria-label','Действия с папкой «'+folder.title+'»');
 const item=(label,callback,css='')=>{const choice=btn(label,()=>{hideMenu();callback()},css);choice.setAttribute('role','menuitem');menu.append(choice)};
 item('Переименовать',()=>showModal({title:'Переименовать папку',description:'Измените название папки «'+folder.title+'».',fields:[{name:'title',label:'Название папки',value:folder.title,maxLength:40}],ok:'Сохранить',submit:({title})=>{if(!title){modalError('Введите название папки.','title');return false}folder.title=title;renderAll();tell('Папка переименована')}}));
 item('Удалить',()=>showModal({title:'Удалить папку «'+folder.title+'»?',description:'Папка будет удалена, но все записи сохранятся в разделе «Все».',ok:'Удалить',submit:()=>{chat.folders=chat.folders.filter(f=>f.id!==folder.id);chat.favorites.forEach(f=>{if(f.folder===folder.id)f.folder=null});chat.folder=null;renderAll();tell('Папка удалена, записи сохранены в «Все»')}}),'danger-action');
 positionMenu(source);menu.querySelector('button')?.focus()
}
function noteModal(fav){showModal({title:fav?'Редактировать заметку':'Новая заметка',description:'Личная заметка хранится в Избранном. Копии чужих сообщений редактировать нельзя.',fields:[{name:'text',label:'Текст заметки',value:fav?.text||'',maxLength:2500,placeholder:'Введите заметку…'}],ok:fav?'Сохранить':'Добавить',submit:({text})=>{if(!text)return false;if(fav)fav.text=text;else chat.favorites.unshift({id:'note-'+(++chat.seq),folder:chat.folder,kind:'note',source:'Личная заметка',author:'Commander',original:timestamp(),saved:timestamp(),text});renderAll();tell(fav?'Заметка обновлена':'Заметка добавлена')}})}
function favoriteOptions(fav,source){
 const menu=$('chat-pop-menu');menu.replaceChildren();
 let submenu=null,trigger=null;
 const closeSubmenu=()=>{if(submenu){submenu.hidden=true;if(trigger)trigger.setAttribute('aria-expanded','false')}};
 const item=(text,fn)=>{const action=btn(text,()=>{hideMenu();fn()},'');action.setAttribute('role','menuitem');action.addEventListener('pointerenter',closeSubmenu);action.addEventListener('focus',closeSubmenu);menu.append(action)};
 if(fav.kind==='note')item('Редактировать заметку',()=>noteModal(fav));
 trigger=btn('Переместить в папку',()=>openSubmenu(false),'chat-submenu-trigger');
 trigger.setAttribute('role','menuitem');trigger.setAttribute('aria-haspopup','menu');trigger.setAttribute('aria-expanded','false');trigger.setAttribute('aria-controls','chat-folder-submenu');
 menu.append(trigger);
 item('Удалить из избранного',()=>showModal({title:'Удалить запись?',description:'Запись исчезнет из вашего личного Избранного.',ok:'Удалить',submit:()=>{chat.favorites=chat.favorites.filter(x=>x!==fav);renderBody()}}));
 submenu=el('div','chat-context-submenu');submenu.id='chat-folder-submenu';submenu.setAttribute('role','menu');submenu.setAttribute('aria-label','Переместить запись в папку');submenu.hidden=true;
 const folders=[{id:null,title:'Без папки (Все)'},...chat.folders];
 folders.forEach(folder=>{
  const selected=(fav.folder||null)===folder.id;
  const choice=btn((selected?'✓ ':'  ')+folder.title,()=>{fav.folder=folder.id;hideMenu();renderBody();tell(folder.id?'Запись перемещена в «'+folder.title+'»':'Запись перемещена в «Все»')},'chat-folder-choice');
  choice.setAttribute('role','menuitemcheckbox');choice.setAttribute('aria-checked',String(selected));
  submenu.append(choice);
 });
 menu.append(submenu);
 function openSubmenu(focusFirst){
  submenu.hidden=false;trigger.setAttribute('aria-expanded','true');
  const main=menu.getBoundingClientRect(),anchor=trigger.getBoundingClientRect(),width=submenu.offsetWidth,height=submenu.offsetHeight;
  const right=main.right-1,left=main.left-width+1;
  if(right+width<=window.innerWidth-8)submenu.style.left=right+'px';
  else if(left>=8)submenu.style.left=left+'px';
  else submenu.style.left=Math.max(8,Math.min(window.innerWidth-width-8,main.left))+'px';
  const belowNeitherFits=right+width>window.innerWidth-8&&left<8;
  const top=belowNeitherFits?main.bottom-1:anchor.top;
  submenu.style.top=Math.max(8,Math.min(window.innerHeight-height-8,top))+'px';
  if(focusFirst)submenu.querySelector('button')?.focus();
 }
 trigger.addEventListener('pointerenter',e=>{if(e.pointerType!=='touch')openSubmenu(false)});
 trigger.addEventListener('keydown',e=>{if(e.key==='ArrowRight'||e.key==='ArrowDown'){e.preventDefault();openSubmenu(true)}});
 submenu.addEventListener('keydown',e=>{
  const items=[...submenu.querySelectorAll('button')],index=items.indexOf(document.activeElement);
  if(e.key==='ArrowDown'||e.key==='ArrowUp'){
   e.preventDefault();items[(index+(e.key==='ArrowDown'?1:-1)+items.length)%items.length]?.focus();
  }else if(e.key==='ArrowLeft'){
   e.preventDefault();closeSubmenu();trigger.focus();
  }
 });
 positionMenu(source);
}
function newConversationModal(){showModal({title:'Новая беседа',description:'Личная беседа или группа (макет без серверных приглашений).',fields:[{name:'type',label:'Тип',type:'select',options:[['dm','Личный диалог'],['group','Группа']]},{name:'name',label:'Позывной игрока или название группы',placeholder:'Например: StarFox',maxLength:40}],ok:'Создать',submit:({type,name})=>{if(!name){modalError(type==='dm'?'Укажите позывной игрока.':'Укажите название группы.');return false}
 const isGroup=type==='group';if(!isGroup){const error=validateInvite(name);if(error){modalError(error);return false}name=resolvePilot(name);if(chat.conversations.some(c=>c.type==='dm'&&c.people.includes(name))){modalError('У вас уже есть личный диалог с этим игроком.');return false}}
 const id='new-'+(++chat.seq);chat.conversations.unshift({id,type,name,people:['Commander',...(isGroup?[]:[name])],unread:0,status:'pending',owner:'Commander'});chat.messages['private:'+id]=[];chat.tab='private';chat.conversation=id;renderAll();tell(isGroup?'Группа создана как черновик. Пригласите участника.':'Приглашение отправлено, ожидается принятие')}})}
function conversationActions(conv){const menu=$('chat-pop-menu');menu.replaceChildren();const item=(label,fn)=>{const action=btn(label,()=>{hideMenu();fn()},'');action.setAttribute('role','menuitem');menu.append(action)};
 if(conv.type==='group'){
  if(conv.owner==='Commander'){
   item('Пригласить участника',()=>showModal({title:'Пригласить в группу',description:'Для демонстрации приглашение будет принято сразу.',fields:[{name:'name',label:'Позывной игрока',placeholder:'NovaScout',userLookup:true}],ok:'Пригласить',submit:({name})=>{const error=validateInvite(name,conv);if(error){modalError(error);return false}if(conv.people.length>=50){modalError('Лимит группы: не более 50 участников.');return false}name=resolvePilot(name);conv.people.push(name);conv.status='active';chat.messages['private:'+conv.id].push({type:'system',id:'sys'+(++chat.seq),text:name+' присоединился к беседе',time:timestamp()});renderAll();tell('Приглашение принято (демонстрация)')}}));
   item('Переименовать группу',()=>showModal({title:'Название группы',description:'Изменить может только текущий владелец.',fields:[{name:'title',label:'Название группы',value:conv.name,maxLength:40}],ok:'Сохранить',submit:({title})=>{if(!title)return false;conv.name=title;renderAll()}}));
   item('Участники / Передать права',()=>showModal({title:'Управление участниками',description:'В группе: '+conv.people.join(', ')+'. Права владельца можно передать активному участнику.',fields:[{name:'owner',label:'Новый владелец',type:'select',options:conv.people.filter(n=>n!=='Commander').map(n=>[n,n])}],ok:'Передать права',submit:({owner})=>{if(!owner)return false;conv.owner=owner;renderAll();tell('Владелец изменён: '+owner)}}));
   item('Удалить группу для всех',()=>showModal({title:'Удалить группу для всех?',description:'Участники потеряют доступ к истории. Сохранённые копии останутся.',ok:'Удалить',submit:()=>{removeConversation(conv)}}));
  }
  item('Покинуть группу',()=>showModal({title:'Покинуть группу?',description:'Вы утратите доступ к беседе и её истории.',ok:'Выйти',submit:()=>removeConversation(conv)}));
 }else{
  if(conv.owner==='Commander'&&conv.status==='active')item('Пригласить игрока',()=>showModal({title:'Приглашение в личную беседу',description:'После принятия приглашения текущий DM станет группой с сохранением истории.',fields:[{name:'name',label:'Позывной игрока',placeholder:'StarFox',userLookup:true}],ok:'Пригласить',submit:({name})=>{const error=validateInvite(name,conv);if(error){modalError(error);return false}name=resolvePilot(name);conv.type='group';conv.people.push(name);conv.name=conv.name+' + '+name;chat.messages['private:'+conv.id].push({type:'system',id:'sys'+(++chat.seq),text:name+' принял приглашение. Личный диалог преобразован в группу.',time:timestamp()});renderAll();tell('Приглашение принято (демонстрация): DM → Group')}}));
  if(conv.status==='pending')item('Симулировать принятие приглашения',()=>{conv.status='active';renderAll();tell('Беседа активирована')});
  item('Выйти из личного диалога',()=>showModal({title:'Закрыть личный диалог?',description:'Выход любого участника удаляет беседу для обоих.',ok:'Выйти и удалить',submit:()=>removeConversation(conv)}));
 }
 const src=$('chat-section-header');const rect=src.getBoundingClientRect();menu.hidden=false;menu.style.left=Math.max(8,Math.min(window.innerWidth-menu.offsetWidth-8,rect.right-menu.offsetWidth))+'px';menu.style.top=Math.min(window.innerHeight-menu.offsetHeight-9,rect.bottom+3)+'px';
}
function removeConversation(conv){chat.conversations=chat.conversations.filter(x=>x.id!==conv.id);delete chat.messages['private:'+conv.id];chat.conversation=null;renderAll();tell('Беседа удалена (демо)')}
function updateMentions(){const wrap=$('chat-mentions'),input=$('chat-input');wrap.replaceChildren();wrap.hidden=true;if(input.disabled)return;const before=input.value.slice(0,input.selectionStart);const match=before.match(/@([A-Za-zА-Яа-яЁё0-9_-]*)$/);if(!match)return;const candidates=chat.tab==='private'?(findConversation()?.people||[]):chat.tab==='corporation'?['Commander','Elena','NovaScout','Vega']:chat.tab==='global'?['Commander','Elena','Navigator','Atlas','Vega','StarFox']:['Elena','StarFox','Commander'];if(['corporation','private'].includes(chat.tab))candidates.unshift('all');const choices=candidates.filter(n=>n.toLowerCase().startsWith(match[1].toLowerCase())).slice(0,6);if(!choices.length)return;for(const n of choices){wrap.append(btn('@'+n,()=>{const start=before.slice(0,-match[0].length);input.value=start+'@'+n+' '+input.value.slice(input.selectionStart);input.dispatchEvent(new Event('input'));input.focus();wrap.hidden=true},''))}wrap.hidden=false}
// Emoji are Unicode text, not attachments; the same per-channel codepoint limit applies.
const emojiGroups={
 'Частые':[['😀','Улыбка'],['😂','Смех'],['👍','Одобрение'],['❤️','Сердце'],['🚀','Ракета'],['👋','Привет'],['✨','Блеск'],['🔥','Огонь'],['🎉','Праздник'],['😎','Круто'],['🪐','Планета'],['💎','Драгоценность']],
 'Эмоции':[['😀','Улыбка'],['😄','Радость'],['😁','Широкая улыбка'],['😂','Смех'],['🤣','Смеюсь'],['😊','Счастье'],['🙂','Спокойно'],['😉','Подмигивание'],['😍','Влюбленность'],['🥰','Любовь'],['😎','Круто'],['🤔','Думаю'],['😮','Удивление'],['😢','Грусть'],['😭','Плач'],['😡','Злость'],['😴','Сон'],['🤖','Робот'],['👽','Пришелец'],['💀','Череп']],
 'Жесты':[['👍','Одобрение'],['👎','Не нравится'],['👏','Аплодисменты'],['🙌','Ура'],['👋','Привет'],['🤝','Рукопожатие'],['🙏','Спасибо'],['💪','Сила'],['✌️','Победа'],['👌','Хорошо'],['👀','Глаза'],['🫡','Приветствие'],['👈','Влево'],['👉','Вправо']],
 'Космос':[['🚀','Ракета'],['🛸','НЛО'],['🪐','Планета'],['🌍','Земля'],['🌕','Луна'],['☀️','Солнце'],['⭐','Звезда'],['🌟','Сияющая звезда'],['🌌','Галактика'],['🌠','Падающая звезда'],['☄️','Комета'],['🛰️','Спутник'],['👨‍🚀','Космонавт'],['👩‍🚀','Астронавт'],['🔭','Телескоп'],['🧭','Компас'],['✨','Искры'],['🌙','Ночь']],
 'Предметы':[['⚒️','Инструменты'],['⛏️','Кирка'],['🔧','Ключ'],['🛠️','Ремонт'],['📡','Антенна'],['⚡','Энергия'],['🔋','Батарея'],['🧪','Исследование'],['📦','Груз'],['💎','Кристалл'],['🪙','Монета'],['🔑','Ключ'],['📍','Метка'],['🎯','Цель'],['✅','Готово'],['❌','Отмена'],['⚠️','Внимание'],['🔔','Уведомление'],['🔥','Огонь'],['🎉','Праздник']]
};
let emojiCategory='Частые';let composerCaret={start:0,end:0};
const codepoints=text=>[...text].length;
function rememberComposerCaret(){const input=$('chat-input');composerCaret={start:input.selectionStart,end:input.selectionEnd}}
function closeEmojiPicker(restoreFocus=false){const panel=$('chat-emoji-panel');if(panel.hidden)return;panel.hidden=true;$('chat-emoji-trigger').setAttribute('aria-expanded','false');if(restoreFocus&&!$('chat-input').disabled)$('chat-input').focus({preventScroll:true})}
function renderEmojiPicker(){
 const cats=$('chat-emoji-categories');cats.replaceChildren();
 for(const category of Object.keys(emojiGroups)){
  const control=btn(category,()=>{emojiCategory=category;renderEmojiPicker()},'chat-emoji-category');
  control.setAttribute('aria-pressed',String(category===emojiCategory));cats.append(control);
 }
 const query=$('chat-emoji-search').value.trim().toLocaleLowerCase('ru');
 const items=query?[...new Map(Object.values(emojiGroups).flat().map(([emoji,name])=>[emoji,[emoji,name]])).values()].filter(([symbol,name])=>name.toLocaleLowerCase('ru').includes(query)||symbol.includes(query)):emojiGroups[emojiCategory];
 const grid=$('chat-emoji-grid');grid.replaceChildren();
 if(!items.length){grid.append(el('p','chat-emoji-empty','Эмодзи не найдены'));return}
 for(const [emoji,name] of items){const control=btn(emoji,()=>insertEmoji(emoji),'chat-emoji-item');control.title=name;control.setAttribute('aria-label',name);grid.append(control)}
}
function openEmojiPicker(){
 if($('chat-input').disabled)return;
 const panel=$('chat-emoji-panel');panel.hidden=false;$('chat-emoji-trigger').setAttribute('aria-expanded','true');
 $('chat-emoji-search').value='';emojiCategory='Частые';renderEmojiPicker();$('chat-mentions').hidden=true;
}
function insertEmoji(emoji){
 const input=$('chat-input');if(input.disabled){closeEmojiPicker();return}
 const start=Math.min(composerCaret.start,input.value.length),end=Math.min(composerCaret.end,input.value.length);
 const result=input.value.slice(0,start)+emoji+input.value.slice(end);
 const limit=chat.tab==='private'?500:256;
 if(codepoints(result)>limit){tell('Достигнут лимит '+limit+' символов');return}
 input.focus({preventScroll:true});input.setRangeText(emoji,start,end,'end');rememberComposerCaret();
 input.dispatchEvent(new Event('input',{bubbles:true}));
 // Leave the palette open to allow inserting several emoji in a row.
}
function onInput(){
 const input=$('chat-input'),k=contentKey();const max=chat.tab==='private'?500:256;
 if(codepoints(input.value)>max){input.value=[...input.value].slice(0,max).join('');tell('Достигнут лимит '+max+' символов')}
 chat.drafts[k]=input.value;$('chat-char-counter').textContent=codepoints(input.value)+' / '+max;
 $('chat-send').disabled=!input.value.trim()||input.disabled;rememberComposerCaret();updateMentions()
}
function applyOpacity(v){chat.opacity=Number(v);$('chat-opacity-label').textContent=v+'%';$('chat-root').style.setProperty('--chat-alpha',String(chat.opacity/100));pref('opacity',chat.opacity)}
function init(){
 document.querySelector('[data-chat-open]').addEventListener('click',e=>{e.preventDefault();e.stopPropagation();openChat()});
 $('chat-dock-toggle').addEventListener('click',()=>setDock(!chat.docked));$('chat-close').addEventListener('click',closeChat);
 $('chat-settings-toggle').addEventListener('click',showSettings);$('chat-search-toggle').addEventListener('click',showSearch);
 $('chat-players-close').addEventListener('click',closePlayersWindow);
 $('chat-moderation-close').addEventListener('click',closeModerationWindow);
 $('chat-moderation-overlay').addEventListener('pointerdown',e=>{if(e.target===$('chat-moderation-overlay'))closeModerationWindow()});
 $('chat-role-test').addEventListener('change',e=>{chat.moderationRole=e.target.value;closeModerationWindow();renderAll();tell('Тестовая роль: '+roleLabel(chat.moderationRole))});
 $('chat-self-mute').addEventListener('change',e=>{chat.muted=e.target.checked;renderAll();tell(chat.muted?'Тестовый запрет отправки включён':'Тестовый запрет снят')});
 $('chat-players-query').addEventListener('input',renderPlayersWindow);
 $('chat-players-overlay').addEventListener('pointerdown',e=>{if(e.target===$('chat-players-overlay'))closePlayersWindow()});
 $('chat-players-overlay').addEventListener('keydown',e=>{if(e.key==='Tab'){const focusables=[$('chat-players-close'),$('chat-players-query')];const index=focusables.indexOf(document.activeElement);if(e.shiftKey&&index===0){e.preventDefault();focusables.at(-1).focus()}else if(!e.shiftKey&&index===focusables.length-1){e.preventDefault();focusables[0].focus()}}});
 $('chat-search-input').addEventListener('input',e=>{chat.query=e.target.value;renderBody()});$('chat-search-clear').addEventListener('click',()=>{chat.query='';$('chat-search-input').value='';renderBody()});
 $('chat-opacity').value=chat.opacity;applyOpacity(chat.opacity);$('chat-opacity').addEventListener('input',e=>applyOpacity(e.target.value));
 $('chat-sounds').addEventListener('change',e=>tell(e.target.checked?'Звуки уведомлений включены (демо)':'Звуки уведомлений выключены'));
 $('chat-on-planet').addEventListener('change',e=>{chat.onPlanet=e.target.checked;if(!chat.onPlanet&&['planet','sector'].includes(chat.tab))chat.tab='global';renderAll();tell(chat.onPlanet?'Тестовый визит: Эдем-IV · сектор 12':'Возврат с планеты: Planet/Sector недоступны')});
 $('chat-availability').addEventListener('change',e=>{chat.availability=e.target.value;renderAll();tell('Тестовый статус служб изменён')});
 const signal=$('chat-signal');
 signal.addEventListener('mouseenter',showChatStatusTooltip);signal.addEventListener('mouseleave',hideChatStatusTooltip);
 signal.addEventListener('focus',showChatStatusTooltip);signal.addEventListener('blur',hideChatStatusTooltip);
 window.addEventListener('resize',()=>{hideChatStatusTooltip();hideChatReadTooltip()});
 document.addEventListener('scroll',()=>{hideChatStatusTooltip();hideChatReadTooltip()},true);
 document.addEventListener('pointerdown',e=>{if(!e.target.closest('.chat-read-dot'))hideChatReadTooltip()},true);
 $('chat-emoji-trigger').addEventListener('click',()=>{$('chat-emoji-panel').hidden?openEmojiPicker():closeEmojiPicker(true)});
 $('chat-emoji-trigger').addEventListener('pointerdown',e=>{if(e.pointerType==='mouse')e.preventDefault()});
 $('chat-emoji-close').addEventListener('click',()=>closeEmojiPicker(true));
 $('chat-emoji-search').addEventListener('input',renderEmojiPicker);
 $('chat-emoji-search').addEventListener('keydown',e=>{if(e.key==='Enter'){const first=$('chat-emoji-grid').querySelector('button');if(first){e.preventDefault();first.click()}}});
 document.addEventListener('pointerdown',e=>{if(!$('chat-emoji-panel').hidden&&!e.target.closest('#chat-emoji-panel')&&!e.target.closest('#chat-emoji-trigger'))closeEmojiPicker()},true);
 $('chat-input').addEventListener('select',rememberComposerCaret);
 $('chat-input').addEventListener('input',onInput);$('chat-input').addEventListener('click',updateMentions);$('chat-input').addEventListener('keyup',updateMentions);
 $('chat-input').addEventListener('keydown',e=>{if(e.key==='Enter'&&!e.shiftKey&&!e.isComposing){e.preventDefault();send()}if(e.key==='Escape')$('chat-mentions').hidden=true});
 $('chat-send').addEventListener('click',send);$('chat-cancel-reply').addEventListener('click',()=>{delete chat.replies[contentKey()];renderCompose()});
 $('chat-modal-cancel').addEventListener('click',closeModal);$('chat-modal-ok').addEventListener('click',submitModal);$('chat-modal').addEventListener('pointerdown',e=>{if(e.target===$('chat-modal'))closeModal()});
 $('chat-modal').addEventListener('keydown',e=>{if(e.key==='Enter'&&e.target.tagName==='INPUT'){e.preventDefault();submitModal()}});
 document.addEventListener('pointerdown',e=>{if(!e.target.closest('#chat-pop-menu')&&!e.target.closest('.chat-more')&&!e.target.closest('#chat-section-header')&&!e.target.closest('.chat-favorite-card-head'))hideMenu()},true);
 document.addEventListener('keydown',e=>{if(e.key==='Escape'){if(!$('chat-modal').hidden){e.stopImmediatePropagation();closeModal()}else if(!$('chat-moderation-overlay').hidden){e.stopImmediatePropagation();closeModerationWindow()}else if(!$('chat-players-overlay').hidden){e.stopImmediatePropagation();closePlayersWindow()}else if(!$('chat-pop-menu').hidden){e.stopImmediatePropagation();hideMenu()}else if(!$('chat-emoji-panel').hidden){e.stopImmediatePropagation();closeEmojiPicker(true)}else if(chat.search&&chat.isOpen){e.stopImmediatePropagation();showSearch()}else if(chat.isOpen&&!chat.docked){e.stopImmediatePropagation();closeChat()}}},true);
 // Drag only the chat's own header. CSS resize:both handles sizing; no second window.
 const floating=$('chat-floating'),handle=$('chat-draghandle');
 let drag=null;
 handle.addEventListener('pointerdown',event=>{
  if(chat.docked||!chat.isOpen||event.button!==0||event.target.closest('button,[role="img"],input,select,textarea'))return;
  const r=floating.getBoundingClientRect();
  drag={id:event.pointerId,dx:event.clientX-r.left,dy:event.clientY-r.top};
  handle.setPointerCapture(event.pointerId);event.preventDefault();
 });
 handle.addEventListener('pointermove',event=>{
  if(!drag||drag.id!==event.pointerId||chat.docked)return;
  const r=floating.getBoundingClientRect();
  floating.style.left=Math.max(4,Math.min(window.innerWidth-r.width-4,event.clientX-drag.dx))+'px';
  floating.style.top=Math.max(62,Math.min(window.innerHeight-r.height-4,event.clientY-drag.dy))+'px';
 });
 const stopDrag=()=>{drag=null};
 handle.addEventListener('pointerup',stopDrag);
 handle.addEventListener('pointercancel',stopDrag);
 handle.addEventListener('lostpointercapture',stopDrag);
 // The mock client contains one fictitious pilot, but it must still detach
 // the docked UI when the game logs out or resets the account session.
 window.addEventListener('signedout',()=>{
  if(!chat.isOpen)return;
  chat.isOpen=false;chat.docked=false;
  closeEmojiPicker();closeModerationWindow();closePlayersWindow();hideMenu();
  $('chat-root').hidden=true;
  $('chat-floating').append($('chat-root'));$('chat-floating').hidden=true;
  $('home-lower-grid').hidden=true;document.body.classList.remove('chat-docked');
  updateActiveNav();
 });
 renderAll();
}
init();
})();
