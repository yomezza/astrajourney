import { notify } from '../../ui/feedback.js';
import { Format } from '../../core/format.js';
import { moduleCargo } from '../../core/ship-capacity.js';
import { api } from '../../core/api.js';
import { Session } from '../../core/session.js';
import { I18n } from '../../core/i18n.js';
import { UI, Modules } from '../../ui/runtime.js';
import { Game } from '../runtime.js';

export const moduleMime = 'application/x-astra-ship-module';
export const moduleCode = m => `${m.size}${m.rating}`;
export const packedModules = ship => (ship?.modules || []).filter(m => !m.installed);
export const packedSize = moduleCargo;
let dragging = false;
export const isModuleDragging = () => dragging;
const tr = text => I18n.t(text);
const names = {cargo:'Трюм',extractor:'Добытчик',scanner:'Сканер',drive:'Ускоритель перехода',scoop:'Топливозаборник'};
export function moduleEffects(m) {
  const e=m.effects || {};
  return [e.cargo_bonus ? `+${e.cargo_bonus} ${tr('Трюм')}` : '', e.yield_bonus ? `+${e.yield_bonus} ${tr('ед./цикл')}` : '', e.scan_percent ? `−${e.scan_percent}% ${tr('Сканирование')}` : '', e.jump_percent ? `−${e.jump_percent}% ${tr('Гиперпереход')}` : '', e.scoop_rate ? `+${e.scoop_rate} ${tr('Топливо')} / 10 s` : ''].filter(Boolean).join(' · ');
}
export function moduleTile(m, ship, activate) {
  const tile=UI.button('',activate);tile.classList.add('equipment-card');tile.dataset.rating=m.rating;
  tile.append(UI.node('b',moduleCode(m),'equipment-code'),UI.node('strong',tr(m.name)),UI.node('small',moduleEffects(m) || tr(names[m.kind] || m.kind)));
  tile.draggable=true;
  tile.ondragstart=event=>{dragging=true;event.stopPropagation();event.dataTransfer.setData(moduleMime,JSON.stringify({id:m.id,ship:ship.id}));event.dataTransfer.effectAllowed='move';};
  tile.ondragend=()=>{dragging=false;window.dispatchEvent(new Event("equipmentdragend"));};
  return tile;
}
export function openOutfitting() { Modules.open('outfitting'); }
export function initOutfitting() {
 const root=Modules.create('outfitting','Оснащение корабля',920,720,{private:true});
 let generation=0, mutation=0, busy=false;
 const n=UI.node;
 async function operate(action,id,ship,epoch) {
   if(busy || epoch!==Session.epoch || !Session.signedIn)return;
   busy=true;const operation=++mutation;
   try {await Game.command({action,id,ship});} finally {if(operation===mutation)busy=false;if(epoch===Session.epoch && Modules.isOpen('outfitting')){try{await Modules.update('outfitting',render);}catch(error){notify(error.message,'error');}}}
 }
 async function render(signal) {
   if(dragging)return false;
   const epoch=Session.epoch,key=++generation,sid=Session.data.expedition?.ship.id;
   if(!sid)return false;
   const data=await api(`ship/modules?ship=${sid}`,undefined,signal);
   if(dragging || signal?.aborted || epoch!==Session.epoch || key!==generation || !Modules.isOpen('outfitting'))return false;
   const ship=data.ship, installed=ship.modules.filter(m=>m.installed), packed=packedModules(ship);
   const content=n('div',undefined,'equipment-workspace'),hero=n('div',undefined,'equipment-heading');
   hero.append(n('div',`${ship.name} · ${tr(ship.spec.name)}`),n('strong',`${data.used} / ${ship.module_limit}`));
   content.append(hero,n('p',tr('Размер установленных модулей суммируется. 5C — размер 5, класс C.'),'muted'),UI.bar(data.used,ship.module_limit,'Оснащение'));
   if(!data.docked)content.append(n('p',tr('Для установки и снятия модулей стыкуйтесь со станцией.'),'equipment-notice'));
   const columns=n('div',undefined,'equipment-columns'),fit=n('section',undefined,'equipment-installed'),hold=n('section',undefined,'equipment-hold');
   fit.append(n('h3',tr('Установленные модули')));hold.append(n('h3',tr('Модули в трюме')),n('p',tr('Перетащите сюда установленный модуль, чтобы снять его.'),'muted'));
   for(const kind of Object.keys(names)) {
     const slot=n('div',undefined,'equipment-slot');slot.append(n('h4',tr(names[kind])));
     const list=installed.filter(m=>m.kind===kind);
     if(!list.length)slot.append(n('p',tr('Не установлен'),'muted'));
     for(const m of list) {const card=moduleTile(m,ship,()=>operate('module_remove',m.id,ship.id,epoch));card.disabled=!data.docked;card.title=tr('Снять в трюм');slot.append(card);}
     slot.ondragover=event=>{if(data.docked && [...event.dataTransfer.types].includes(moduleMime)){event.preventDefault();event.dataTransfer.dropEffect='move';}};
     slot.ondrop=event=>{if(!data.docked)return;event.preventDefault();event.stopPropagation();try {const value=JSON.parse(event.dataTransfer.getData(moduleMime));const m=packed.find(m=>m.id===value.id);if(value.ship===ship.id && m?.kind===kind)operate('module_install',m.id,ship.id,epoch);}catch{}};
     fit.append(slot);
   }
   for(const m of packed) {
     const item=n('div',undefined,'equipment-item'),card=moduleTile(m,ship,()=>operate('module_install',m.id,ship.id,epoch));card.disabled=!data.docked;item.append(card);
     const sell=UI.button(tr('Продать')+' · '+Format.credits(Math.floor((m.paid||0)/2)),()=>UI.confirm('Продажа модуля',`${tr(m.name)} · ${moduleCode(m)} · ${Format.credits(Math.floor((m.paid||0)/2))}`,()=>operate('module_sell',m.id,ship.id,epoch)),!data.docked);
     item.append(sell);hold.append(item);
   }
   if(!packed.length)hold.append(n('p',tr('Купленные модули появятся здесь. Перетащите их в соответствующий отсек или нажмите на карточку.'),'muted'));
   hold.append(n('p',`${tr('Занято')}: ${data.cargo_used} / ${ship.spec.cargo}`),UI.button('Открыть трюм',()=>Modules.open('cargo')),UI.button('Рынок модулей',()=>{Game.openModuleMarket?.();}));
   hold.ondragover=event=>{if(data.docked && [...event.dataTransfer.types].includes(moduleMime))event.preventDefault();};
   hold.ondrop=event=>{event.preventDefault();event.stopPropagation();try{const value=JSON.parse(event.dataTransfer.getData(moduleMime));if(value.ship===ship.id && installed.some(m=>m.id===value.id))operate('module_remove',value.id,ship.id,epoch);}catch{}};
   columns.append(fit,hold);content.append(columns);root.replaceChildren(content);
   return true;
 }
 Modules.prepare('outfitting',render);
 window.addEventListener('gamedata',()=>{if(Modules.isOpen('outfitting') && !Modules.isLoading('outfitting') && !busy)Modules.update('outfitting',render).catch(()=>{});});
 window.addEventListener('moduleclose',event=>{if(['outfitting','cargo'].includes(event.detail))dragging=false;if(event.detail==='outfitting')generation++;});
 window.addEventListener('equipmentdragend',()=>{if(Modules.isOpen('outfitting') && !busy)Modules.update('outfitting',render).catch(()=>{});});
 window.addEventListener('signedout',()=>{dragging=false;mutation++;busy=false;generation++;root.replaceChildren();});
}
