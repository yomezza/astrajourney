/* Shared colony catalogue: presentation only, no colony mutations or fabricated sites. */
const views = new WeakMap();
const TYPES = Object.freeze({
  "Каменистая":"object-rocky.svg","Садовый мир":"object-garden.svg",
  "Пустынная":"object-desert.svg","Ледяная":"object-icy.svg",
  "Лавовая":"object-lava.svg","Вулканическая":"object-lava.svg",
  "Газовый гигант":"object-gas.svg"
});
const STATUS = {funding:["Финансирование","Funding"],building:["Строительство","Building"],
  complete:["Колонизация завершена","Completed"],cancelled:["Отменён","Cancelled"],
  expired:["Просрочен","Expired"]};
const LABELS = {
  "Колонии и проекты":"Colonies and projects","Мои колонии":"My colonies",
  "Выбранная колония":"Selected colony","Планета":"Planet","Система":"System",
  "Тип планеты":"Planet type","Владелец":"Owner","Тип владения":"Ownership",
  "Личная":"Personal","Корпоративная":"Corporate","Сектор":"Sector",
  "Статус":"Status","Прогресс":"Progress","Открыть проект":"Open project",
  "Поверхность планеты":"Planet surface","Продолжить высадку":"Resume landing",
  "Получить возврат":"Claim refund","Нет доступных колоний или проектов колонизации":"No colonies or colonization projects available",
  "Ничего не найдено":"No matching colonies","Поиск колонии, планеты или системы":"Search colonies, planets or systems",
  "Сведения о планете недоступны":"Planet information unavailable",
  "Загрузка сведений о планете…":"Loading planet information…",
  "Доступные проекты и колонии":"Available projects and colonies",
  "Возвраты":"Refunds"
};
export function mountColonyCatalog(root, data, options) {
  let state=views.get(root);
  if(!state){
    state={selected:null,query:"",cache:new Map(),serial:0,request:0,scroll:0};
    views.set(root,state);
  }
  const lang=options.language?.()==="en"?"en":"ru";
  const tr=(s)=>lang==="en"?(LABELS[s]||s):s;
  const status=(s)=>STATUS[s]?(STATUS[s][lang==="en"?1:0]):String(s||"");
  const node=(tag,cls,value)=>{
    const e=document.createElement(tag);if(cls)e.className=cls;
    if(value!==undefined&&value!==null)e.textContent=String(value);
    return e;
  };
  const makeAction=(label,fn)=>{const b=node("button","terminal-action colony-catalog-action",tr(label));b.type="button";b.addEventListener("click",fn);return b;};
  const projects=Array.isArray(data?.projects)?data.projects:[];
  if(!projects.some(p=>String(p.id)===String(state.selected)))
    state.selected=projects.length?projects[0].id:null;
  state.request++;
  const oldSearch=root.querySelector(".colony-catalog-search");
  const restoreFocus=document.activeElement===oldSearch;
  const cursor=restoreFocus?oldSearch.selectionStart:null;
  const oldList=root.querySelector(".colony-catalog-entries");
  if(oldList)state.scroll=oldList.scrollTop;
  root.classList.add("colony-catalog-root");
  root.replaceChildren();
  const header=node("div","colony-catalog-heading");
  const heading=node("div");
  heading.append(node("h3",null,tr("Колонии и проекты")),node("p","muted",tr("Доступные проекты и колонии")));
  header.append(heading,makeAction("Продолжить высадку",options.onVisit));
  const layout=node("div","colony-catalog-layout"),
    detail=node("section","colony-catalog-detail"),
    library=node("section","colony-catalog-library");
  const libraryHeading=node("h3",null,tr("Мои колонии"));
  const search=node("input","colony-catalog-search");
  search.type="search";search.value=state.query;
  search.placeholder=tr("Поиск колонии, планеты или системы");
  search.setAttribute("aria-label",search.placeholder);
  const entries=node("div","colony-catalog-entries");
  library.append(libraryHeading,search,entries);
  layout.append(detail,library);
  root.append(header,layout);
  const refunds=Array.isArray(data?.refunds)?data.refunds:[];
  if(refunds.length){
    const section=node("section","colony-catalog-refunds");
    section.append(node("h3",null,tr("Возвраты")));
    for(const refund of refunds){
      section.append(makeAction("Получить возврат",()=>options.onRefund(refund)));
      const b=section.lastChild;
      b.textContent=tr("Получить возврат")+" · "+options.resourceName(refund.resource)+" × "+refund.amount;
    }
    root.append(section);
  }
  const planetData=p=>state.cache.get(String(p.star)+":"+String(p.planet));
  const starName=p=>options.starName?.(p.star)||"";
  function row(host,label,value){
    if(value===undefined||value===null||String(value).trim()==="")return;
    const item=node("div","colony-catalog-row");
    item.append(node("span",null,tr(label)),node("strong",null,value));host.append(item);
  }
  function renderDetail(){
    detail.replaceChildren(node("p","colony-catalog-eyebrow",tr("Выбранная колония")));
    const p=projects.find(x=>String(x.id)===String(state.selected));
    if(!p){detail.append(node("p","muted",tr("Нет доступных колоний или проектов колонизации")));return;}
    const info=planetData(p);
    const visual=node("div","colony-catalog-visual");
    const img=node("img","colony-catalog-planet");
    img.alt=info?.site?.name||p.name||tr("Планета");
    const kind=info?.site?.kind;
    img.src=options.assetBase+(TYPES[kind]||"object-unknown.svg");
    img.onerror=()=>{if(!img.dataset.fallback){img.dataset.fallback="1";img.src=options.assetBase+"object-unknown.svg";}};
    visual.append(img);
    if(info?.site?.name)visual.append(node("span","colony-catalog-planet-name",info.site.name));
    detail.append(visual,node("h2",null,p.name||info?.site?.name||tr("Планета")));
    const badge=node("span","colony-catalog-status",status(p.status));badge.dataset.status=p.status||"";
    detail.append(badge);
    const planet=node("div","colony-catalog-data"),colony=node("div","colony-catalog-data");
    planet.append(node("h3",null,tr("Планета")));
    row(planet,"Планета",info?.site?.name||p.name);
    row(planet,"Система",info?.star?.name||starName(p));
    row(planet,"Тип планеты",kind&&(lang==="en"?(options.translateKind?.(kind)||kind):kind));
    if(!info)planet.append(node("small","muted",tr("Загрузка сведений о планете…")));
    else if(!info.site)planet.append(node("small","muted",tr("Сведения о планете недоступны")));
    colony.append(node("h3",null,tr("Колонии и проекты")));
    row(colony,"Владелец",p.owner);
    row(colony,"Тип владения",p.owner_company!=null?tr("Корпоративная"):p.owner_user!=null?tr("Личная"):null);
    row(colony,"Сектор",p.sector);
    row(colony,"Статус",status(p.status));
    if(Number.isFinite(Number(p.progress))&&Number.isFinite(Number(p.build_ticks))&&Number(p.build_ticks)>0&&p.status==="building")
      row(colony,"Прогресс",p.progress+" / "+p.build_ticks);
    detail.append(planet,colony);
    const actions=node("div","colony-catalog-actions");
    actions.append(makeAction("Открыть проект",()=>options.onProject(p.id)),
      makeAction("Поверхность планеты",()=>options.onSurface(p.star,p.planet)));
    detail.append(actions);
    if(!info&&p.star!=null&&p.planet!=null)resolve(p);
  }
  async function resolve(p){
    const key=String(p.star)+":"+String(p.planet);
    if(state.cache.has(key)||state.pending?.has(key))return;
    if(!state.pending)state.pending=new Set();
    state.pending.add(key);
    const serial=state.serial;
    try{
      const system=await options.loadSystem(p.star);
      if(state.serial!==serial)return;
      const site=system?.sites?.find(s=>Number(s.planet)===Number(p.planet) &&
        TYPES[s.kind] && (s.kind!=="Орбитальная станция"));
      state.cache.set(key,{site:site||null,star:system?.star||null});
    }catch(error){
      if(state.serial!==serial)return;
      state.cache.set(key,{site:null,star:null});
      options.onLoadError?.(error);
    }finally{
      state.pending.delete(key);
      if(state.serial===serial&&root.isConnected&&String(state.selected)===String(p.id))renderDetail();
    }
  }
  function renderEntries(){
    const oldScroll=entries.scrollTop;
    entries.replaceChildren();
    const q=state.query.trim().toLocaleLowerCase();
    const filtered=projects.filter(p=>[p.name,p.owner,starName(p),planetData(p)?.site?.name]
      .some(v=>String(v||"").toLocaleLowerCase().includes(q)));
    for(const p of filtered){
      const item=node("button","colony-catalog-entry");
      item.type="button";item.dataset.projectId=String(p.id);
      item.setAttribute("aria-pressed",String(p.id)===String(state.selected)?"true":"false");
      const top=node("span","colony-catalog-entry-top");
      top.append(node("strong",null,p.name||tr("Планета")),node("span","colony-catalog-entry-status",status(p.status)));
      const secondary=[starName(p),p.sector!=null?tr("Сектор")+" "+p.sector:""].filter(Boolean).join(" · ");
      item.append(top,node("small",null,secondary),node("small","muted",p.owner||""));
      item.addEventListener("click",()=>{
        state.selected=p.id;
        for(const e of entries.children)e.setAttribute("aria-pressed",String(e.dataset.projectId)===String(p.id)?"true":"false");
        renderDetail();
      });
      entries.append(item);
    }
    if(!filtered.length)entries.append(node("p","muted",projects.length?tr("Ничего не найдено"):tr("Нет доступных колоний или проектов колонизации")));
    entries.scrollTop=oldScroll;
  }
  search.addEventListener("input",()=>{state.query=search.value;renderEntries();});
  renderEntries();entries.scrollTop=state.scroll||0;renderDetail();
  if(restoreFocus){
    search.focus({preventScroll:true});
    if(cursor!==null)search.setSelectionRange(cursor,cursor);
  }
  return {select:id=>{state.selected=id;renderEntries();renderDetail();}};
}
export function clearColonyCatalog(root){const s=views.get(root);if(s){s.serial++;views.delete(root);}}
