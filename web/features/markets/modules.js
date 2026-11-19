import { api } from '../../core/api.js';
import { Session } from '../../core/session.js';
import { UI } from '../../ui/runtime.js';
import { I18n } from '../../core/i18n.js';
import { Format } from '../../core/format.js';
import { moduleCode, moduleEffects } from '../outfitting/index.js';
export function createModulesMarket({state,command,renderMarket}) {
 return async function renderModules(root,lease) {
  const sid=Session.data.expedition.ship.id,epoch=Session.epoch;
  const location=state.moduleLocation || '';
  const query=new URLSearchParams({ship:String(sid)});
  if(location){const [star,...parts]=location.split('|');query.set('star',star);query.set('station',parts.join('|'));}
  const data=await api('ship/modules?'+query);
  if(epoch!==Session.epoch || state.tab!=='modules' || (lease && !lease()))return false;
  const n=UI.node,tr=s=>I18n.t(s);root.replaceChildren();
  const here=data.stations.find(p=>p.star===data.star && p.object_id===data.station);
  const current=here?`${data.star}|${data.station}`:'';
  const choices=[['','Выберите станцию'],...data.stations.map(p=>[`${p.star}|${p.object_id}`,p.name])];
  UI.choice(root,'Станция',choices,current,value=>{state.moduleLocation=value;return renderMarket();});
  root.append(n('p',tr('Ассортимент и классы отличаются между станциями. Покупка доступна только на станции продавца.'),'muted'));
  const filters=n('div',undefined,'inline-controls');
  UI.choice(filters,'Тип модуля',[['','Все типы'],['extractor','Добытчики'],['cargo','Трюм'],['scanner','Сканер'],['scoop','Топливозаборник'],['drive','Ускоритель перехода']],state.moduleKind||'',value=>{state.moduleKind=value;return renderMarket();});
  UI.choice(filters,'Класс',[['','Все классы'],...['S','A','B','C'].map(r=>[r,r])],state.moduleRating||'',value=>{state.moduleRating=value;return renderMarket();});
  root.append(filters);
  const grid=n('div',undefined,'equipment-market');
  const offers=(data.offers||[]).filter(m=>(!state.moduleKind||m.kind===state.moduleKind)&&(!state.moduleRating||m.rating===state.moduleRating));
  for(const m of offers) {
   const card=n('article',undefined,'equipment-offer');card.dataset.rating=m.rating;
   card.append(n('strong',moduleCode(m),'equipment-code'),n('h3',tr(m.name)),n('p',moduleEffects(m)),n('small',tr(m.description)),n('p',`${Format.credits(m.price)} · ${tr('В наличии')}: ${m.quantity}`));
   const canBuy=data.docked && data.ship.star===data.star && data.ship.object_id===data.station && m.quantity>0 && data.cargo_used+m.size<=data.ship.spec.cargo && m.price<=Session.data.expedition.credits;
   card.append(UI.button('Купить модуль',()=>command({action:'module_buy',ship:sid,target:m.catalog_id,price:m.price,star:data.star,station:data.station}),!canBuy));grid.append(card);
  }
  if(!offers.length)UI.empty(grid,here?'Модулей в продаже нет':'Выберите станцию');root.append(grid);
 };
}
