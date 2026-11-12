/*
 * Research system lineup: SVG assets and objects from the game's existing
 * /api/system?id=N DTO. Adapted from the approved v13 demo for production
 * string site IDs, window lifecycle and the shared localization catalogue.
 */
const MODELS = Object.freeze({
  "Ледяная":"object-icy.svg", "Каменистая":"object-rocky.svg",
  "Садовый мир":"object-garden.svg", "Газовый гигант":"object-gas.svg",
  "Пустынная":"object-desert.svg", "Лавовая":"object-lava.svg",
  "Вулканическая":"object-lava.svg",
  "Орбитальная станция":"object-station.svg",
  "Пояс астероидов":"object-asteroids.svg",
  "Ледяное поле":"object-ice-field.svg",
  "Чёрная дыра":"object-black-hole.svg"
});
const STARS = new Set(["O","B","A","F","G","K","M"]);
const FALLBACK = "object-unknown.svg";
let tooltipSerial = 0;

export function assetFor(object, star) {
  if (object?.kind === "Звезда")
    return STARS.has(String(star?.class||"").toUpperCase())
      ? "object-star-"+String(star.class).toLowerCase()+".svg"
      : FALLBACK;
  return MODELS[object?.kind] || FALLBACK;
}

const make = (tag, cls, text) => {
  const node=document.createElement(tag);
  if (cls) node.className=cls;
  if (text!==undefined && text!==null) node.textContent=String(text);
  return node;
};
const tr = (lang, ru, en) => lang==="en" ? en : ru;

function valid(s){
  // Production site IDs are strings (planet-1, station, asteroids), not star IDs.
  return s && typeof s==="object" &&
    ((typeof s.id==="string" && s.id.trim()!=="") || Number.isInteger(s.id)) &&
    typeof s.name==="string" && s.name.trim()!=="";
}
function orderSites(items) {
  return items.map((site,index)=>({site,index}))
    .sort((a,b)=>{
      const ao=a.site?.orbit,bo=b.site?.orbit;
      if(ao!==null&&ao!==undefined&&bo!==null&&bo!==undefined&&
         Number.isFinite(Number(ao))&&Number.isFinite(Number(bo)))
        return Number(ao)-Number(bo)||a.index-b.index;
      return a.index-b.index;
    }).map(entry=>entry.site);
}
function modelItems(data){
  const star=data?.star;
  if(!valid(star)||!Number.isInteger(Number(star.id))||Number(star.id)<1)throw Error("Invalid system star");
  const sites=Array.isArray(data?.sites) ? data.sites.filter(valid) : [];
  // The upstream API masks undiscovered sites. Never synthesize extras.
  return [{id:star.id,kind:"Звезда",name:star.name,star:true,details:star},
          ...orderSites(sites).map(site=>({...site,star:false}))];
}
function createTooltip(){
  const tip=make("aside","research-system-tooltip");
  tip.setAttribute("role","tooltip");
  tip.id="research-system-tip-"+(++tooltipSerial);
  tip.hidden=true;
  document.body.append(tip);
  return tip;
}
function fillTooltip(tip,model,data,lang,resolveResource,translateKind){
  tip.replaceChildren();
  const star=data.star;
  const heading=make("strong","research-system-tooltip-title",model.name);
  tip.append(heading,make("span","research-system-tooltip-kind",
    model.star ? tr(lang,"Звезда","Star") : translateKind(model.kind)));
  const rows=[];
  const row=(label,value)=>{
    if(value===undefined||value===null||value===""||!Number.isFinite(Number(value))&&typeof value==="number")return;
    rows.push([label,String(value)]);
  };
  if(model.star){
    row(tr(lang,"Спектральный класс","Spectral class"),star.class);
    row(tr(lang,"Масса, M☉","Mass, M☉"),star.mass);
    row(tr(lang,"Светимость, L☉","Luminosity, L☉"),star.luminosity);
  }else{
    row(tr(lang,"ID объекта","Object ID"),model.id);
    row(tr(lang,"Тип","Type"),translateKind(model.kind));
    const d=model.details;
    if(d&&typeof d==="object"){
      row(tr(lang,"Масса, M⊕","Mass, M⊕"),d.mass);
      row(tr(lang,"Гравитация, g","Gravity, g"),d.gravity);
      row(tr(lang,"Температура, K","Temperature, K"),d.temperature);
      row(tr(lang,"Расстояние, AU","Distance, AU"),d.au);
      row(tr(lang,"Атмосфера","Atmosphere"),d.atmosphere);
    }
    if(Array.isArray(model.resources) && model.resources.length)
      row(tr(lang,"Ресурсы","Resources"),
        model.resources.map(id=>resolveResource(id,lang)).join(", "));
    if(model.quality!==undefined)row(tr(lang,"Качество","Quality"),model.quality);
    if(model.concentration!==undefined)row(tr(lang,"Концентрация","Concentration"),model.concentration+"%");
    if(typeof data.surveyed==="boolean")
      row(tr(lang,"Исследование","Survey"),data.surveyed?
        tr(lang,"Завершено","Completed"):tr(lang,"Сведения частичные","Partial information"));
  }
  for(const [label,value] of rows){
    const line=make("div","research-system-tooltip-row");
    line.append(make("span",null,label),make("b",null,value));
    tip.append(line);
  }
}
function placeTooltip(tip,anchor){
  if(!anchor?.isConnected||tip.hidden)return;
  const r=anchor.getBoundingClientRect();
  const w=tip.offsetWidth,h=tip.offsetHeight;
  const margin=8;
  const vw=document.documentElement.clientWidth,vh=window.innerHeight;
  const x=Math.max(margin,Math.min(r.left+r.width/2-w/2,vw-w-margin));
  const below=r.bottom+9;
  const y=below+h<=vh-margin?below:Math.max(margin,r.top-h-9);
  tip.style.left=x+"px";tip.style.top=y+"px";
}

export function mountSystemLineup({
  host,starId,loadSystem,onBack,assetBase="/",fallbackAsset="/object-unknown.svg",
  resolveResource=(id)=>id,language=()=>document.documentElement.lang,
  onMap=null,translateKind=(kind)=>kind
}){
  if(!host)throw Error("System view host is missing");
  if(!Number.isInteger(Number(starId))||Number(starId)<1)throw Error("Invalid star ID");
  if(typeof loadSystem!=="function"||typeof onBack!=="function")throw Error("System view callbacks are missing");
  const controller=new AbortController(), signal=controller.signal;
  const previous=[...host.children];
  const hiddenValues=previous.map(node=>node.hidden);
  previous.forEach(node=>node.hidden=true);
  const view=make("section","research-system-view");
  view.dataset.star=String(starId);
  // A local heading must not inherit the game's fixed global <header> rules.
  const top=make("div","research-system-top");
  const back=make("button","terminal-action research-system-back","← Назад");
  back.type="button";
  const hgroup=make("div","research-system-top-heading");
  const name=make("h3",null,"…");
  const caption=make("p","research-system-caption");
  hgroup.append(name,caption);
  top.append(back,hgroup);
  const hint=make("p","research-system-hint");
  const viewport=make("div","research-system-scroll");
  viewport.tabIndex=0;
  viewport.setAttribute("role","region");
  viewport.setAttribute("aria-label","Звёздная система");
  const lineup=make("div","research-system-lineup");
  viewport.append(lineup);
  const status=make("p","research-system-status");
  status.setAttribute("role","status");
  view.append(top,hint,viewport,status);
  host.append(view);
  const tooltip=createTooltip();
  let active=null,preview=null,closed=false,current=null;
  const lang=()=>language()==="en"?"en":"ru";
  const hideTooltip=()=>{
    if(active){
      active.removeAttribute("aria-describedby");
      active.setAttribute("aria-expanded","false");
    }
    active=null;tooltip.hidden=true;tooltip.replaceChildren();
  };
  function close(){
    if(closed)return;
    closed=true;controller.abort();
    hideTooltip();tooltip.remove();view.remove();
    previous.forEach((node,i)=>{if(node.isConnected)node.hidden=hiddenValues[i]});
    onBack(Number(starId));
  }
  function showTooltip(button,model){
    if(closed||!current)return;
    if(active&&active!==button){
      active.removeAttribute("aria-describedby");
      active.setAttribute("aria-expanded","false");
    }
    active=button;
    button.setAttribute("aria-describedby",tooltip.id);
    button.setAttribute("aria-expanded","true");
    fillTooltip(tooltip,model,current,lang(),resolveResource,translateKind);
    tooltip.hidden=false;
    placeTooltip(tooltip,button);
  }
  function render(){
    if(closed)return;
    const l=lang();
    back.textContent=tr(l,"← Назад","← Back");
    hint.textContent=tr(l,
      "Последовательность объектов системы · прокрутите вправо для просмотра",
      "System objects · scroll horizontally for more");
    if(preview?.star){
      name.textContent=preview.star.name;
      const fields=[tr(l,"Звезда","Star")];
      if(preview.star.class)fields.push(tr(l,"класс","class")+" "+preview.star.class);
      if(typeof preview.surveyed==="boolean")
        fields.push(preview.surveyed?tr(l,"исследована","surveyed"):tr(l,"данные ограничены","limited information"));
      caption.textContent=fields.join(" · ");
    }
    viewport.setAttribute("aria-label",tr(l,"Состав звёздной системы","Stellar system objects"));
    lineup.replaceChildren();
    if(!preview)return;
    const models=modelItems(preview);
    const fragment=document.createDocumentFragment();
    models.forEach((model,i)=>{
      const card=make("div","research-system-object");
      card.dataset.objectId=String(model.id);
      card.dataset.kind=model.kind;
      const button=make("button","research-system-object-trigger");
      button.type="button";
      button.setAttribute("aria-label",model.name+": "+translateKind(model.kind||""));
      button.setAttribute("aria-haspopup","true");
      button.setAttribute("aria-expanded","false");
       const frame=make("span","research-system-art");
      const art=make("img");
      const chosen=assetFor(model,preview.star);
      art.src=assetBase+chosen;
      art.alt="";
      art.decoding="async";
      art.loading=i<4?"eager":"lazy";
      art.draggable=false;
      // Missing files and unrecognized kinds always fall back to a neutral SVG.
      art.addEventListener("error",()=>{
        if(!art.dataset.fallback){art.dataset.fallback="1";art.src=fallbackAsset;}
      },{signal});
      frame.append(art);
      button.append(frame,make("span","research-system-object-name",model.name));
      if(!model.star)button.append(make("small","research-system-object-kind",translateKind(model.kind||"")));
      card.append(button);
      button.addEventListener("pointerenter",e=>{if(e.pointerType!=="touch")showTooltip(button,model)},{signal});
      button.addEventListener("focus",()=>showTooltip(button,model),{signal});
      button.addEventListener("click",e=>{
        e.stopPropagation();
        // Focus often fires just before click, especially on touch devices;
        // a first tap must open the tooltip, not accidentally toggle it shut.
        showTooltip(button,model);
      },{signal});
      fragment.append(card);
    });
    lineup.append(fragment);
    status.textContent=models.length>1
      ?tr(l,"Объектов: ","Objects: ")+models.length
      :tr(l,"Других открытых объектов нет. Сведения об остальных могут быть недоступны.",
            "No other revealed objects. Information about further objects may be unavailable.");
  }
  back.addEventListener("click",close,{signal});
  const onKey=e=>{if(e.key==="Escape"&&!tooltip.hidden){hideTooltip();e.stopPropagation();}};
  const outside=e=>{if(tooltip.hidden)return;
    if(!view.contains(e.target)&&!tooltip.contains(e.target))hideTooltip();
    else if(!e.target.closest?.(".research-system-object-trigger"))hideTooltip();
  };
  const onScroll=()=>hideTooltip();
  const onResize=()=>hideTooltip();
  const onLanguage=()=>{hideTooltip();render()};
  document.addEventListener("keydown",onKey,{capture:true,signal});
  document.addEventListener("pointerdown",outside,{signal});
  document.addEventListener("scroll",onScroll,{capture:true,passive:true,signal});
  window.addEventListener("resize",onResize,{signal});
  window.addEventListener("languagechange",onLanguage,{signal});
  window.addEventListener("signedout",close,{signal});
  const onModuleClose=e=>{
    if(e.detail==="studies"||e.detail==="studies-v13-mock")close();
  };
  window.addEventListener("moduleclose",onModuleClose,{signal});
  const promise=Promise.resolve().then(()=>loadSystem(Number(starId),signal));
  status.textContent=tr(lang(),"Загружаем сведения системы…","Loading system data…");
  promise.then(data=>{
    if(closed||signal.aborted)return;
    preview=data;current=data;render();
  }).catch(error=>{
    if(closed||signal.aborted)return;
    status.textContent=tr(lang(),"Данные системы недоступны: ","System data unavailable: ")+error.message;
    name.textContent=String(starId);
  });
  return {close,get element(){return view;},get data(){return current;}};
}
