/* Shared interactive demo for all A/B/C variants. No APIs or external fonts. */
(function(){
"use strict";
const root=document.getElementById("projector-lab");
if(!root)return;
const base=root.dataset.assets||"../";
const langUI={
 ru:{lead:"Три художественных решения. Выбери сценарий, сравни RU/EN и проверь реальную ширину правого слота.",language:"Язык",scenario:"Сценарий",width:"Ширина проектора",focus:"Режим",compare:"Три рядом",select:"Крупный просмотр",all:"Все новости",nav:"НОВОСТИ",click:"Нажатие зарегистрировано; игровое окно новостей не подключено.",none:"Сигнал отсутствует",emptyCategory:"НЕТ СОБЫТИЙ",a:"Голографическая типографика",b:"Информационный терминал",c:"Объёмная сводка",descA:"Световая типографика и тонкая разметка; самая лёгкая проекция.",descB:"Полупрозрачный информационный слой, читаемый при компактной ширине.",descC:"Микроголограмма события встроена в текстовую иерархию."},
 en:{lead:"Three experimental treatments. Switch content cases, RU/EN and half-column sizes.",language:"Language",scenario:"Sample",width:"Projector width",focus:"View",compare:"Side by side",select:"Focused view",all:"All news",nav:"NEWS",click:"Click registered; the game news window is not connected.",none:"No signal",emptyCategory:"NO EVENTS",a:"Holographic typography",b:"Information terminal",c:"Volumetric briefing",descA:"Light-made letters and minimal calibration accents.",descB:"Transparent information layer optimized for compact widths.",descC:"A tiny holographic object anchors the story."}
};
const stories={
 ru:{
 short:[{kind:"ИССЛЕДОВАНИЯ",title:"Новый мир",desc:"Обнаружена пригодная для изучения планета.",time:"12 минут назад",icon:"star"},{kind:"ЭКОНОМИКА",title:"Рынок растёт",desc:"Станции закупают больше ресурсов.",time:"40 минут назад",icon:"market"}],
 normal:[{kind:"ИССЛЕДОВАНИЯ",title:"Открыта новая звёздная система",desc:"Академия Эдема подтверждает координаты ранее неизвестной системы.",time:"15 минут назад",icon:"star"},{kind:"ЭКОНОМИКА",title:"Рынок реагирует на новые поставки",desc:"Изменились объёмы ресурсов на ключевых торговых станциях.",time:"42 минуты назад",icon:"market"}],
 long:[{kind:"ГАЛАКТИКА",title:"Исследователи обнаружили необычную систему за пределами известных гиперкоридоров",desc:"Экспедиция уточняет навигационные координаты и состав планет.",time:"1 час назад",icon:"star"},{kind:"ЭКОНОМИКА",title:"Стоимость редких материалов изменилась после открытия дополнительного маршрута доставки",desc:"Перевозчики оценивают движение цен на удалённых рынках.",time:"3 часа назад",icon:"market"}],
 verbose:[{kind:"ИССЛЕДОВАНИЯ",title:"Новый сигнал",desc:"Многолетние наблюдения выявили перспективные объекты. Команда станции приступила к дополнительному сканированию, чтобы уточнить их спектральный класс и положение.",time:"1 час назад",icon:"star"},{kind:"ГАЛАКТИКА",title:"Новая экспедиция",desc:"Пилоты и исследовательские группы направляются к дальним системам. Доступные сведения будут дополнены результатами сканирования новых звёзд, планет и ресурсов.",time:"Вчера",icon:"planet"}],
 nodesc:[{kind:"ИССЛЕДОВАНИЯ",title:"Сканирование завершено",desc:"",time:"5 минут назад",icon:"planet"},{kind:"ЭКОНОМИКА",title:"Поставки возобновлены",desc:"",time:"10 минут назад",icon:"market"}],
 missing:[null,{kind:"ЭКОНОМИКА",title:"Торговый маршрут открыт",desc:"На станции появился новый контракт на доставку.",time:"20 минут назад",icon:"market"}]
 },
 en:{
 short:[{kind:"EXPLORATION",title:"New world",desc:"A new planet is ready to explore.",time:"12 min ago",icon:"star"},{kind:"ECONOMY",title:"Prices rise",desc:"Stations demand more materials.",time:"40 min ago",icon:"market"}],
 normal:[{kind:"EXPLORATION",title:"A new star system discovered",desc:"Eden Academy confirms the location of an uncharted system.",time:"15 min ago",icon:"star"},{kind:"ECONOMY",title:"Markets react to incoming cargo",desc:"Resource volumes changed at major trading stations.",time:"42 min ago",icon:"market"}],
 long:[{kind:"GALAXY",title:"Explorers discover an unusual stellar system beyond mapped hyperspace routes",desc:"The expedition is identifying objects and charting navigation coordinates.",time:"1 hour ago",icon:"star"},{kind:"ECONOMY",title:"Rare commodity prices fluctuate following a breakthrough in long-range cargo logistics",desc:"Freight operators reassess distant market prices.",time:"3 hours ago",icon:"market"}],
 verbose:[{kind:"EXPLORATION",title:"Unusual signal",desc:"Extended observations have revealed several objects of interest, and station researchers have begun additional scans to assess their spectral classes and orbital positions.",time:"1 hour ago",icon:"star"},{kind:"GALAXY",title:"New expedition",desc:"Pilots and exploration teams are preparing journeys to distant systems. Their findings will be shared as new stars, planets and resources are discovered.",time:"Yesterday",icon:"planet"}],
 nodesc:[{kind:"EXPLORATION",title:"Scan complete",desc:"",time:"5 min ago",icon:"planet"},{kind:"ECONOMY",title:"Deliveries restored",desc:"",time:"10 min ago",icon:"market"}],
 missing:[null,{kind:"ECONOMY",title:"Cargo route open",desc:"A station lists a fresh delivery contract.",time:"20 min ago",icon:"market"}]
 }
};
const iconPaths={
 star:'<circle cx="12" cy="12" r="5"/><path d="M12 1.5v4m0 13v4m-10.5-10.5h4m13 0h4M4.5 4.5l3 3m9 9 3 3M4.5 19.5l3-3m9-9 3-3"/>',
 planet:'<ellipse cx="12" cy="12" rx="7" ry="7.2"/><path d="M2 17.5c3-5 16-10 20-9.5M3 17.8c4 1.8 12-1 18-8.4"/>',
 market:'<path d="M3 19.5h18M5 16V11m4 5V6m4 10V9m4 7V3m4 13V8"/><path d="M4 8.5 10 5l5 2 5-4"/>',
 offline:'<circle cx="12" cy="12" r="8"/><path d="M6 18 18 6"/>'
};
const imgFiles=["news-projection-beams.svg","news-panel-left.svg","news-panel-right.svg","news-all-button.svg","news-projector-base.svg"];
const types=["a","b","c"], names={a:"A",b:"B",c:"C"};
let language="ru",caseId="normal",maxWidth=520,mode="all",focus="b";
const single=root.dataset.single||"";
const variants=single?[single]:types;
let elements=[];
function node(tag,cls,content){const el=document.createElement(tag);if(cls)el.className=cls;if(content!==undefined)el.textContent=content;return el;}
function init(){
 const intro=node("p","lead");intro.id="copy-intro";root.append(intro);
 const bar=node("div","lab-tools");bar.id="lab-tools";root.append(bar);
 const languagePicker=node("select");languagePicker.id="language-picker";languagePicker.setAttribute("aria-label","Language");
 [["ru","RU"],["en","EN"]].forEach(pair=>{const opt=node("option","",pair[1]);opt.value=pair[0];languagePicker.append(opt)});
 const casePicker=node("select");casePicker.id="case-picker";casePicker.setAttribute("aria-label","News scenario");
 [["short","Короткий / Short"],["normal","Обычный / Regular"],["long","Длинный / Long title"],["verbose","Длинное описание / Long copy"],["nodesc","Без описания / No description"],["missing","Нет новости / Missing"]].forEach(pair=>{const opt=node("option","",pair[1]);opt.value=pair[0];casePicker.append(opt)});
 const widthPicker=node("select");widthPicker.id="width-picker";widthPicker.setAttribute("aria-label","Projector width");
 [420,520,680,900].forEach(w=>{const opt=node("option","",w+" px");opt.value=w;widthPicker.append(opt)});
 function labelled(picker,key){const label=node("label");label.dataset.label=key;const span=node("span");label.append(span,picker);bar.append(label);}
 labelled(languagePicker,"language");labelled(casePicker,"scenario");labelled(widthPicker,"width");
 if(!single){
  const picker=node("div","picker");picker.id="mode-picker";
  const allBtn=node("button","", "ABC");allBtn.type="button";allBtn.dataset.mode="all";picker.append(allBtn);
  for(const type of types){const b=node("button","",names[type]);b.dataset.mode="focus";b.dataset.focus=type;b.type="button";picker.append(b)}
  bar.append(picker);
 }
 const grid=node("section","variant-grid"+(single?" solo":""));grid.id="variant-grid";root.append(grid);
 for(const type of variants){
   const card=node("section","variant-card variant-"+type);card.dataset.variant=type;
   const head=node("header");const title=node("h2");title.dataset.title=type;const cap=node("span","glyph","NEWS // "+names[type]);head.append(title,cap);
   const rail=node("div","stage-rail");const stage=node("div","stage");
   stage.setAttribute("aria-label","Holographic news projection concept "+names[type]);
   for(let i=0;i<imgFiles.length;i++){
     const image=document.createElement("img");image.src=base+imgFiles[i];image.alt="";image.draggable=false;
     image.className="layer "+["beams","panel left-image","panel right-image","controller","base"][i];image.loading="eager";
     stage.append(image);
   }
   const tracks=[];
   for(const side of ["left","right"]){
     const track=node("div","float-track "+side);
     const panel=node("article","copy-surface");panel.setAttribute("aria-live","off");
     const header=node("div","story-head");const icon=node("span","story-icon");icon.setAttribute("aria-hidden","true");const kind=node("span","story-kind");header.append(icon,kind);
     const title=node("h3","story-heading"),desc=node("p","story-desc"),time=node("time","story-time");panel.append(header,title,desc,time);track.append(panel);
     stage.append(track);tracks.push({track,panel,kind,icon,title,desc,time,side});
   }
   const button=node("button","all-news");button.type="button";const label=node("span");button.append(label,node("small","", "UPLINK"));stage.append(button);
   const description=node("p","mode-caption");const status=node("div","live-status");status.setAttribute("role","status");
   rail.append(stage);card.append(head,rail,description,status);grid.append(card);
   button.addEventListener("click",()=>{status.textContent=langUI[language].click;button.classList.remove("pressed");void button.offsetWidth;button.classList.add("pressed");window.setTimeout(()=>button.classList.remove("pressed"),180)});
   elements.push({type,card,stage,tracks,button,label,description,title,status});
 }
 languagePicker.addEventListener("change",()=>{language=languagePicker.value;render()});
 casePicker.addEventListener("change",()=>{caseId=casePicker.value;render()});
 widthPicker.addEventListener("change",()=>{maxWidth=Number(widthPicker.value);render()});
 if(!single)bar.addEventListener("click",e=>{const btn=e.target.closest("button[data-mode]");if(!btn)return;mode=btn.dataset.mode;focus=btn.dataset.focus||"b";render()});
 languagePicker.value=language;casePicker.value=caseId;widthPicker.value=maxWidth;
 render();
}
function glyph(kind){return '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.35" stroke-linecap="round" stroke-linejoin="round" focusable="false" aria-hidden="true">'+(iconPaths[kind]||iconPaths.offline)+"</svg>";}
function render(){
 document.documentElement.lang=language;
 const l=langUI[language];root.querySelector("#copy-intro").textContent=l.lead;
 for(const label of root.querySelectorAll("[data-label]"))label.querySelector("span").textContent=l[label.dataset.label];
 const grid=root.querySelector("#variant-grid");grid.classList.toggle("focused",mode==="focus"&&!single);
 root.querySelectorAll("[data-mode]").forEach(b=>b.setAttribute("aria-pressed",String(mode===b.dataset.mode&&(mode==="all"||focus===b.dataset.focus))));
 const pair=stories[language][caseId];
 for(const item of elements){
  const {type,tracks,button,label,title,description,stage,status,card}=item;
  card.classList.toggle("selected",type===focus);
  title.textContent=names[type]+" — "+l[type];
  description.textContent=l["desc"+names[type]];
  status.textContent="";
  stage.style.setProperty("--target-width",maxWidth+"px");
  label.textContent=l.all==="Все новости"?"Все\nновости":"All\nnews";
  label.style.whiteSpace="pre-line";
  button.setAttribute("aria-label",l.all);
  for(let i=0;i<tracks.length;i++){
   const {track,panel,kind,icon,title:heading,desc,time}=tracks[i],entry=pair[i];
   const empty=!entry;
   track.classList.toggle("is-empty",empty);
   track.classList.toggle("no-description",!entry||!entry.desc);
   track.classList.toggle("is-long",!!entry&&entry.title.length>48);
   kind.textContent=empty?l.emptyCategory:entry.kind;
   icon.innerHTML=glyph(empty?"offline":entry.icon);
   heading.textContent=empty?l.none:entry.title;
   heading.title=empty?"":entry.title;
   desc.textContent=empty?"":entry.desc;
   time.textContent=empty?"—":entry.time;
   panel.setAttribute("aria-label",empty?l.none:entry.kind+": "+entry.title+". "+entry.desc);
  }
 }
}
init();
})();
