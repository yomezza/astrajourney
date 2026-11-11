/* Pilot command terminal POC. No production state, commands or socket access. */
import {Modules,UI} from "/ui/runtime.js";
import {Preferences} from "/core/preferences.js";
import {Session} from "/core/session.js";
import {createFleet} from "./fixtures.js";
import {TYPES,LABELS,MAX_WORKSPACES,seeded,empty,newWorkspace,newInstance,
        duplicateWorkspace,validName,graphemes,validateState,resolveShip,windowId} from "./model.js";
import {renderModule} from "./modules.js";

const make=(tag,label,cls)=>{
  const n=document.createElement(tag);
  if(label!==undefined&&label!==null)n.textContent=String(label);
  if(cls)n.className=cls;
  return n;
};
const action=(label,click,disabled=false)=>{
  const b=UI.button(label,click,disabled);b.type="button";return b;
};
const STORAGE_VERSION=1;
let controller=null, accountVariant="A";
function storageAvailable(){
  try{
    const k="ptdemo-storage-test";
    localStorage.setItem(k,"ok");
    const good=localStorage.getItem(k)==="ok";
    localStorage.removeItem(k);
    return good;
  }catch{return false;}
}
function accountID(){
  const base=Session.data?.state?.user?.id;
  if(!Session.signedIn||!Number.isSafeInteger(Number(base)))return null;
  return String(base)+(accountVariant==="B"?"-sandbox-B":"");
}
class PilotWorkspaceController{
  constructor(account){
    this.account=account;
    this.key=Preferences.key("pilot-terminal",{account,version:STORAGE_VERSION});
    const raw=Preferences.read(this.key,null);
    this.corrupt=raw!==null&&!validateState(raw);
    this.state=validateState(raw)?raw:raw===null?seeded():empty();
    this.ships=createFleet();
    this.catalogOpen=false;this.query="";
    this.silent=false;this.activeWindows=new Map();this.log=[];
    this.storageOK=storageAvailable();
    this.shellId="ptdemo-control-u"+account.replace(/[^a-z0-9-]/gi,"-");
    this.shell=null;this.root=null;
    this.onClose=event=>this.closed(event.detail);
    this.onDestroy=event=>this.destroyed(event.detail);
    this.onPageHide=()=>this.save();
    this.onSignOut=()=>this.dispose();
    window.addEventListener("moduleclose",this.onClose);
    window.addEventListener("moduledestroy",this.onDestroy);
    window.addEventListener("pagehide",this.onPageHide);
    window.addEventListener("signedout",this.onSignOut);
    this.save();
  }
  current(){return this.state.workspaces.find(w=>w.id===this.state.activeWorkspaceId);}
  id(ws,ins){return windowId(this.account,ws,ins);}
  save(){
    Preferences.write(this.key,this.state);
    if(this.storageOK){
      try{
        if(localStorage.getItem(this.key)!==JSON.stringify(this.state))this.storageOK=false;
      }catch{this.storageOK=false;}
    }
    this.storageWarning();
  }
  storageWarning(){
    const banner=this.root?.querySelector(".pt-storage-warning");
    if(banner){banner.hidden=this.storageOK&&!this.corrupt;
      banner.textContent=this.corrupt?"Повреждённые настройки отклонены: создано безопасное пространство.":
        "Сохранение только до закрытия вкладки: localStorage недоступен.";
    }
  }
  notice(message){const e=this.root?.querySelector(".pt-message");if(e){e.textContent=message;e.hidden=false;}}
  open(){
    if(!this.shell){
      const old=Preferences.read("module:"+this.shellId,{});
      if(!Number.isFinite(old.x)){
        Preferences.write("module:"+this.shellId,{x:Math.max(6,(innerWidth-1120)/2),y:69,
          w:Math.min(1120,innerWidth-12),h:Math.min(282,innerHeight-112),pinned:false});
      }
      this.shell=Modules.create(this.shellId,"Командный терминал · проба",1120,282,{private:false});
      this.shell.classList.add("pt-terminal-host");
      this.root=make("div",null,"pt-terminal");
      this.shell.replaceChildren(this.root);
    }
    Modules.open(this.shellId);
    this.renderControl();
    this.mountWorkspace();
  }
  closed(id){
    if(this.silent)return;
    if(id===this.shellId){this.unmountWorkspace();return;}
    const record=this.activeWindows.get(id);
    if(!record)return;
    this.activeWindows.delete(id);
    const ws=this.current();
    const index=ws?.instances.findIndex(x=>x.id===record.ins.id)??-1;
    if(index>=0){ws.instances.splice(index,1);this.save();this.renderControl();}
    Modules.destroy(id);
  }
  destroyed(id){this.activeWindows.delete(id);}
  dispose(){
    this.silent=true;
    this.unmountWorkspace();
    if(this.shellId){Modules.close(this.shellId);Modules.destroy(this.shellId);}
    this.save();
    window.removeEventListener("moduleclose",this.onClose);
    window.removeEventListener("moduledestroy",this.onDestroy);
    window.removeEventListener("pagehide",this.onPageHide);
    window.removeEventListener("signedout",this.onSignOut);
    this.silent=false;this.shell=null;this.root=null;
    if(controller===this)controller=null;
  }
  unmountWorkspace(){
    const wasSilent=this.silent;
    this.silent=true;
    for(const id of [...this.activeWindows.keys()]){
      Modules.close(id);Modules.destroy(id);
    }
    this.activeWindows.clear();
    this.silent=wasSilent;
    this.save();
  }
  switchWorkspace(id){
    if(id===this.state.activeWorkspaceId)return;
    if(!this.state.workspaces.some(x=>x.id===id))return;
    this.unmountWorkspace();
    this.state.activeWorkspaceId=id;this.save();
    this.renderControl();this.mountWorkspace();
  }
  ensureGeometry(ws,ins){
    const id=this.id(ws,ins),data=Preferences.read("module:"+id,{});
    if(Number.isFinite(data.x)&&Number.isFinite(data.w))return;
    const index=TYPES.indexOf(ins.type);
    const width=Math.min(356,innerWidth-12);
    const height=Math.min(ins.type==="guide"?280:330,innerHeight-110);
    const columns=Math.max(1,Math.floor((innerWidth-20)/375));
    const column=index%columns,row=Math.floor(index/columns);
    const x=Math.min(innerWidth-width-6,8+column*375);
    const y=Math.min(innerHeight-height-36,Math.max(64,356+row*60));
    Preferences.write("module:"+id,{x,y,w:width,h:height,pinned:false});
  }
  mountWorkspace(){
    const ws=this.current();
    if(!ws||!Modules.isOpen(this.shellId))return;
    for(const ins of ws.instances){
      const id=this.id(ws,ins);
      if(this.activeWindows.has(id))continue;
      this.ensureGeometry(ws,ins);
      const root=Modules.create(id,LABELS[ins.type]+" · "+ws.name,356,330,{private:false});
      this.activeWindows.set(id,{ws,ins,root});
      this.renderInstance(ws,ins,root);
      Modules.open(id);
    }
  }
  renderInstance(ws,ins,root){
    renderModule({root,instance:ins,workspace:ws,ships:this.ships,
      onChanged:()=>this.save(),
      onBinding:(instance,mode,shipId)=>this.setBinding(instance,mode,shipId),
      onRemove:instanceId=>this.removeInstance(instanceId),
      onMockAction:details=>this.mockAction(details)});
  }
  renderShipBound(ws){
    for(const {ins,root} of this.activeWindows.values())
      if(ins.binding.mode==="workspace")this.renderInstance(ws,ins,root);
  }
  setShip(value){
    const ws=this.current(),id=Number(value);
    if(!this.ships.has(id)){this.notice("Объект недоступен: корабль #"+id);return;}
    ws.shipId=id;this.save();this.renderControl();this.renderShipBound(ws);
  }
  setBinding(ins,mode,id){
    if(ins.type==="guide"||!["workspace","fixed"].includes(mode))return;
    if(mode==="fixed"&&!Number.isSafeInteger(Number(id)))return;
    ins.binding=mode==="fixed"?{mode,shipId:Number(id)}:{mode};
    this.save();
    const item=[...this.activeWindows.values()].find(v=>v.ins.id===ins.id);
    if(item)this.renderInstance(item.ws,ins,item.root);
  }
  addInstance(type){
    const ws=this.current();
    if(!TYPES.includes(type))return;
    const existing=ws.instances.find(x=>x.type===type);
    if(existing){Modules.focus(this.id(ws,existing));this.catalogOpen=false;this.renderControl();return;}
    ws.instances.push(newInstance(type,ws.shipId));
    this.catalogOpen=false;this.save();this.renderControl();this.mountWorkspace();
  }
  removeInstance(id){
    const ws=this.current(),i=ws.instances.findIndex(x=>x.id===id);
    if(i<0)return;
    const ins=ws.instances[i],key=this.id(ws,ins);
    this.silent=true;Modules.close(key);Modules.destroy(key);this.silent=false;
    this.activeWindows.delete(key);
    ws.instances.splice(i,1);Preferences.remove("module:"+key);
    this.save();this.renderControl();
  }
  createWorkspace(){
    if(this.state.workspaces.length>=MAX_WORKSPACES){
      this.notice("Не более 10 рабочих пространств");return;
    }
    const name=prompt("Название пространства (до 32 символов):","Новое пространство");
    if(name===null)return;
    if(!validName(name)){this.notice("Название: от 1 до 32 видимых символов");return;}
    const ws=newWorkspace(name,this.current()?.shipId||41);
    this.state.workspaces.push(ws);this.save();this.switchWorkspace(ws.id);
  }
  rename(){
    const ws=this.current(),name=prompt("Новое название (до 32 символов)",ws.name);
    if(name===null)return;
    if(!validName(name)){this.notice("Название: от 1 до 32 видимых символов");return;}
    ws.name=name.trim().normalize("NFC");this.save();this.renderControl();
  }
  duplicate(){
    if(this.state.workspaces.length>=MAX_WORKSPACES){this.notice("Не более 10 рабочих пространств");return;}
    const ws=this.current(),clone=duplicateWorkspace(ws);
    clone.name=graphemes(clone.name).slice(0,32).join("");
    for(let i=0;i<ws.instances.length;i++){
      const old=this.id(ws,ws.instances[i]),next=this.id(clone,clone.instances[i]);
      const geometry=Preferences.read("module:"+old,{});
      if(Number.isFinite(geometry.x)&&Number.isFinite(geometry.w))
        Preferences.write("module:"+next,geometry);
    }
    this.state.workspaces.splice(this.state.workspaces.indexOf(ws)+1,0,clone);
    this.save();this.switchWorkspace(clone.id);
  }
  removeWorkspace(){
    if(this.state.workspaces.length===1){this.notice("Последнее пространство удалить нельзя");return;}
    const ws=this.current();
    if(!confirm("Удалить пространство «"+ws.name+"»? Игровые данные не затрагиваются."))return;
    this.unmountWorkspace();
    const index=this.state.workspaces.indexOf(ws);
    this.state.workspaces.splice(index,1);
    for(const ins of ws.instances)Preferences.remove("module:"+this.id(ws,ins));
    if(this.state.defaultWorkspaceId===ws.id)
      this.state.defaultWorkspaceId=this.state.workspaces[0].id;
    this.state.activeWorkspaceId=this.state.workspaces[Math.min(index,this.state.workspaces.length-1)].id;
    this.save();this.renderControl();this.mountWorkspace();
  }
  reorder(offset){
    const ws=this.current(),index=this.state.workspaces.indexOf(ws),other=index+offset;
    if(other<0||other>=this.state.workspaces.length)return;
    [this.state.workspaces[index],this.state.workspaces[other]]=
      [this.state.workspaces[other],this.state.workspaces[index]];
    this.save();this.renderControl();
  }
  setDefault(){this.state.defaultWorkspaceId=this.current().id;this.save();this.renderControl();}
  mockAction({shipId,siteId,action:name}){
    const line="DEMO ONLY: "+name+" · корабль #"+shipId+" · месторождение "+siteId+
      " · реальные команды отключены.";
    this.log.unshift(line);this.log.length=Math.min(8,this.log.length);
    this.notice(line);
    this.renderLog();
  }
  renderLog(){
    const host=this.root?.querySelector(".pt-action-log");
    if(host)host.replaceChildren(...this.log.map(item=>make("div",item)));
  }
  toggleShip(){
    if(this.ships.has(82))this.ships.delete(82);
    else this.ships=createFleet();
    for(const {ws,ins,root} of this.activeWindows.values())this.renderInstance(ws,ins,root);
    this.renderControl();
  }
  renderControl(){
    if(!this.root)return;
    const root=this.root,ws=this.current();
    root.replaceChildren();
    const tabs=make("div",null,"pt-workspace-tabs");
    tabs.setAttribute("role","tablist");
    tabs.setAttribute("aria-label","Рабочие пространства");
    for(const space of this.state.workspaces){
      const b=make("button",space.name+(space.id===this.state.defaultWorkspaceId?" ★":""),"pt-tab");
      b.type="button";b.setAttribute("role","tab");
      b.setAttribute("aria-selected",String(space.id===ws.id));
      b.onclick=()=>this.switchWorkspace(space.id);
      b.onkeydown=e=>{
        if(!["ArrowLeft","ArrowRight","Home","End"].includes(e.key))return;
        e.preventDefault();
        const arr=this.state.workspaces,idx=arr.indexOf(space);
        const next=e.key==="Home"?0:e.key==="End"?arr.length-1:
          (idx+(e.key==="ArrowRight"?1:-1)+arr.length)%arr.length;
        this.switchWorkspace(arr[next].id);
        this.root.querySelectorAll(".pt-tab")[next]?.focus();
      };
      tabs.append(b);
    }
    tabs.append(action("+",()=>this.createWorkspace(),this.state.workspaces.length>=MAX_WORKSPACES));
    const panel=make("div",null,"pt-context");
    const heading=make("div");
    heading.append(make("strong",ws.name),
      make("small","Контекст корабля пространства · "+this.state.workspaces.length+"/"+MAX_WORKSPACES,"muted"));
    const context=make("label","Корабль: ");
    const shipSelect=make("select");
    for(const ship of this.ships.values()){
      const o=make("option",ship.name+" #"+ship.id);o.value=String(ship.id);shipSelect.append(o);
    }
    if(ws.shipId!=null&&!this.ships.has(ws.shipId)){
      const o=make("option","Объект недоступен #"+ws.shipId);o.value=String(ws.shipId);shipSelect.prepend(o);
    }
    shipSelect.value=String(ws.shipId);shipSelect.onchange=()=>this.setShip(shipSelect.value);
    context.append(shipSelect);
    panel.append(heading,context);
    const actions=make("div",null,"pt-workspace-actions");
    for(const [name,fn] of [
      ["Все модули",()=>{this.catalogOpen=!this.catalogOpen;this.renderControl();}],
      ["Переименовать",()=>this.rename()],
      ["Дублировать",()=>this.duplicate()],
      ["Удалить",()=>this.removeWorkspace()],
      ["←",()=>this.reorder(-1)],
      ["→",()=>this.reorder(1)],
      ["По умолчанию",()=>this.setDefault()]
    ])actions.append(action(name,fn));
    const storage=make("p",null,"pt-storage-warning");
    const msg=make("p",null,"pt-message");msg.hidden=true;msg.setAttribute("role","status");
    root.append(tabs,panel,actions,storage,msg);
    if(this.catalogOpen){
      const cat=make("div",null,"pt-catalog");
      const search=make("input");search.type="search";search.placeholder="Поиск модуля";search.value=this.query;
      search.setAttribute("aria-label","Поиск модулей");
      const list=make("div",null,"pt-catalog-list");
      const draw=()=>{
        list.replaceChildren();
        for(const type of TYPES.filter(t=>LABELS[t].toLowerCase().includes(this.query.toLowerCase()))){
          const exists=ws.instances.some(i=>i.type===type);
          list.append(action(LABELS[type]+(exists?" · Открыть":" · Добавить"),()=>this.addInstance(type)));
        }
      };
      search.oninput=()=>{this.query=search.value;draw();};
      cat.append(search,list);draw();root.append(cat);
    }
    const extra=make("div",null,"pt-demo-controls");
    extra.append(action("Скрыть / вернуть корабль #82",()=>this.toggleShip()),
      action("Mock аккаунт: "+accountVariant,()=>switchMockAccount()),
      action("Общие уведомления",()=>Modules.open("notifications")));
    root.append(extra);
    const log=make("div",null,"pt-action-log");log.setAttribute("aria-live","polite");root.append(log);
    this.storageWarning();this.renderLog();
  }
}
function switchMockAccount(){
  accountVariant=accountVariant==="A"?"B":"A";
  controller?.dispose();
  openPilotTerminal();
}
export function openPilotTerminal(){
  const id=accountID();
  if(id===null)return;
  if(controller?.account!==id){controller?.dispose();controller=new PilotWorkspaceController(id);}
  controller.open();
}
export function initPilotTerminal(){
  if(document.querySelector("[data-pilot-terminal-open]"))return;
  if(!document.getElementById("pt-demo-css")){
    const link=make("link");link.id="pt-demo-css";link.rel="stylesheet";link.href="/mock/pilot-terminal/terminal.css";
    document.head.append(link);
  }
  const nav=document.querySelector("#sidebar");
  if(!nav)return;
  const b=make("button",null,"pt-sidebar-entry");b.type="button";
  b.dataset.pilotTerminalOpen="true";b.dataset.nav="Командный терминал";
  b.append(make("span","⌘","nav-icon"),make("span","Терминал · проба","nav-label"));
  const anchor=nav.querySelector("[data-uiux-open]")||nav.querySelector('[data-open="guide"]');
  if(anchor)anchor.before(b);else nav.append(b);
  b.onclick=()=>openPilotTerminal();
  window.addEventListener("signedout",()=>{accountVariant="A";});
  if(new URLSearchParams(location.search).get("pilot-terminal")==="1"){
    const onReady=()=>{
      if(!Session.signedIn)return;
      window.removeEventListener("gamedata",onReady);
      openPilotTerminal();
    };
    window.addEventListener("gamedata",onReady);
    onReady();
  }
}
