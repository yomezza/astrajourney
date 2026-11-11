/* Read-only module adapters; the original fleet/mining factories are not imported. */
import {UI} from "/ui/runtime.js";
import {LABELS,resolveShip} from "./model.js";
import {RESOURCES} from "./fixtures.js";
const el=(tag,text,cls)=>{
  const e=document.createElement(tag);if(text!==undefined&&text!==null)e.textContent=String(text);
  if(cls)e.className=cls;return e;
};
const button=(label,fn)=>{const b=UI.button(label,fn);b.type="button";return b;};
const select=(items,value,onChange)=>{
  const s=el("select");for(const [id,label] of items){
    const o=el("option",label);o.value=String(id);s.append(o);}
  s.value=String(value);s.onchange=()=>onChange(s.value);return s;
};
function fieldRow(host,label,value){UI.row(host,label,String(value));}
function shell(host,title,context,actions){
  const h=el("div",null,"pt-module-header");
  h.append(el("h3",title),el("small",context,"muted"));
  host.append(h);
  const toolbar=el("div",null,"pt-module-tools");actions(toolbar);host.append(toolbar);
}
function message(host,text){host.append(el("p",text,"pt-notice"));}
export function renderModule({root,instance:ins,workspace:ws,ships,onChanged,onBinding,onRemove,onMockAction}){
  root.classList.add("pt-module-body");root.replaceChildren();
  const resolved=resolveShip(ins,ws,ships),ship=resolved.ship,id=resolved.shipId;
  const context=ins.binding.mode==="none"?"Независимый модуль":
    ins.binding.mode==="fixed"?"Фиксирован: #"+id:"Следует пространству: #"+id;
  shell(root,LABELS[ins.type]+" · "+(ship?ship.name+" #"+ship.id:(id!==null?"#"+id:"без корабля")),context,bar=>{
    if(ins.type!=="guide"){
      const modes=select([["workspace","Следовать пространству"],["fixed","Фиксированный корабль"]],ins.binding.mode,
        mode=>onBinding(ins,mode,mode==="fixed"?(id||ws.shipId):null));
      modes.setAttribute("aria-label","Привязка корабля");bar.append(modes);
      if(ins.binding.mode==="fixed"){
        const shipsOptions=[...ships.values()].map(s=>[s.id,s.name+" #"+s.id]);
        if(id!=null&&!ships.has(id))shipsOptions.unshift([id,"Недоступен #"+id]);
        const fixed=select(shipsOptions,id,shipId=>onBinding(ins,"fixed",Number(shipId)));
        fixed.setAttribute("aria-label","Фиксированный корабль");
        bar.append(fixed);
      }
    }
    bar.append(button("Убрать из пространства",()=>onRemove(ins.id)));
  });
  if(ins.type==="guide"){
    const article=el("div",null,"pt-guide");
    article.append(el("h4","Руководство пилота"),el("p","Переключайте рабочие пространства и выбирайте корабли независимо."));
    article.append(el("p","⌖ в заголовке закрепляет положение окна, а «Фиксированный корабль» закрепляет его данные."));
    article.append(el("p","Все операции здесь являются демонстрацией и не изменяют игру."));
    root.append(article);return;
  }
  if(resolved.status!=="ready"){
    message(root,id===null?"Выберите корабль пространства":"Объект недоступен · корабль #"+id);
    root.append(el("p","Выберите доступный корабль через контекст пространства или привязку окна.","muted"));
    return;
  }
  if(ins.type==="ship"){
    const tabs=select([["overview","Обзор"],["systems","Состояние"]],ins.uiState.tab||"overview",
      value=>{ins.uiState.tab=value;onChanged();renderModule({root,instance:ins,workspace:ws,ships,onChanged,onBinding,onRemove,onMockAction});});
    tabs.setAttribute("aria-label","Раздел корабля");root.append(tabs);
    if(ins.uiState.tab==="systems"){
      fieldRow(root,"Местоположение",ship.position);
      fieldRow(root,"Звёздная система",ship.star);
      fieldRow(root,"Тип корпуса",ship.model);
      fieldRow(root,"Состояние щита",ship.shield+"%");
    }else{
      const art=el("div","◈","pt-ship-art");
      art.append(el("strong",ship.name+" · #"+ship.id));
      root.append(art);
      root.append(UI.bar(ship.fuel,ship.fuelMax,"Топливо"));
      fieldRow(root,"Топливо",ship.fuel+" / "+ship.fuelMax);
      fieldRow(root,"Корпус",ship.health+"%");
      fieldRow(root,"Щит",ship.shield+"%");
      fieldRow(root,"Местоположение",ship.position);
    }
  }else if(ins.type==="cargo"){
    const controls=el("div",null,"pt-filter");
    const category=select([["all","Все ресурсы"],["Руда","Руда"],["Жидкость","Жидкости"],["Органика","Органика"]],
      ins.uiState.category||"all",v=>{ins.uiState.category=v;onChanged();renderModule({root,instance:ins,workspace:ws,ships,onChanged,onBinding,onRemove,onMockAction});});
    category.setAttribute("aria-label","Фильтр трюма");
    const sort=select([["name","По названию"],["quantity","По количеству"]],ins.uiState.sort||"name",
      v=>{ins.uiState.sort=v;onChanged();renderModule({root,instance:ins,workspace:ws,ships,onChanged,onBinding,onRemove,onMockAction});});
    sort.setAttribute("aria-label","Сортировка трюма");controls.append(category,sort);root.append(controls);
    const qty=Object.values(ship.cargo).reduce((a,b)=>a+b,0);
    root.append(UI.bar(qty,ship.capacity,"Заполнение трюма"));
    fieldRow(root,"Всего груза",qty+" / "+ship.capacity);
    const items=Object.entries(ship.cargo).filter(([k])=>ins.uiState.category==="all"||!ins.uiState.category||RESOURCES[k]?.[1]===ins.uiState.category);
    items.sort((a,b)=>ins.uiState.sort==="quantity"?b[1]-a[1]:RESOURCES[a[0]][0].localeCompare(RESOURCES[b[0]][0],"ru"));
    const list=el("div",null,"pt-resources");
    for(const [res,amount] of items)fieldRow(list,RESOURCES[res]?.[0]||res,amount);
    if(!items.length)message(list,"Нет ресурсов по выбранному фильтру");
    root.append(list);
  }else if(ins.type==="mining"){
    const filter=select([["all","Все месторождения"],["ready","Доступные"],["working","В процессе"]],
      ins.uiState.filter||"all",v=>{ins.uiState.filter=v;onChanged();renderModule({root,instance:ins,workspace:ws,ships,onChanged,onBinding,onRemove,onMockAction});});
    filter.setAttribute("aria-label","Фильтр добычи");root.append(filter);
    const sites=ship.mining.filter(x=>ins.uiState.filter==="all"||!ins.uiState.filter||x.status===ins.uiState.filter);
    for(const site of sites){
      const card=el("div",null,"pt-mining-site");
      card.append(el("strong",site.site),el("small",(RESOURCES[site.resource]?.[0]||site.resource)+" · "+site.rate+" / тик · "+(site.status==="ready"?"Доступно":"В процессе")));
      card.append(button("Демо: добыть",()=>onMockAction({shipId:ship.id,siteId:site.id,action:"mine"})));
      root.append(card);
    }
    if(!sites.length)message(root,"Нет месторождений по выбранному фильтру");
  }
}
