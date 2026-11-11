// Pure, validated workspace state. No network calls, Session mutations or DOM.
export const TYPES = ["ship","cargo","mining","guide"];
export const LABELS = {ship:"Корабль",cargo:"Трюм",mining:"Добыча",guide:"Справка"};
export const MAX_WORKSPACES = 10;
export const VERSION = 1;
export const uuid = () => crypto.randomUUID();
export function graphemes(text){
  const s=String(text).normalize("NFC");
  return typeof Intl.Segmenter==="function"
    ? [...new Intl.Segmenter("ru",{granularity:"grapheme"}).segment(s)].map(x=>x.segment)
    : Array.from(s);
}
export function validName(value){
  return typeof value==="string"&&!!value.trim()&&graphemes(value.trim()).length<=32;
}
export function newInstance(type,shipId=null){
  if(!TYPES.includes(type))throw Error("Unknown module type");
  const binding=type==="guide"?{mode:"none"}:
    type==="ship"&&shipId!=null?{mode:"fixed",shipId}:{mode:"workspace"};
  return {id:uuid(),type,binding,uiState:{}};
}
export function newWorkspace(name,shipId,types=[]){
  if(!validName(name))throw Error("Название: от 1 до 32 видимых символов");
  return {id:uuid(),name:name.trim().normalize("NFC"),shipId,
    instances:types.map(type=>newInstance(type,type==="ship"?shipId:null))};
}
export function seeded(){
  const a=newWorkspace("Добыча — 1",41,TYPES);
  const b=newWorkspace("Добыча — 2",82,TYPES);
  return {version:VERSION,activeWorkspaceId:a.id,defaultWorkspaceId:a.id,workspaces:[a,b]};
}
export function empty(){const ws=newWorkspace("Моё пространство",41);return {
  version:VERSION,activeWorkspaceId:ws.id,defaultWorkspaceId:ws.id,workspaces:[ws]};}
const isID=x=>typeof x==="string"&&/^[a-z0-9-]{10,80}$/i.test(x);
const shipID=x=>x===null||x===41||x===82;
export function validateState(value){
  if(!value||value.version!==VERSION||!Array.isArray(value.workspaces)||
    value.workspaces.length<1||value.workspaces.length>MAX_WORKSPACES)return false;
  const seen=new Set();
  for(const ws of value.workspaces){
    if(!isID(ws.id)||seen.has(ws.id)||!validName(ws.name)||!shipID(ws.shipId)||
      !Array.isArray(ws.instances)||ws.instances.length>4)return false;
    seen.add(ws.id);
    const types=new Set();
    for(const ins of ws.instances){
      if(!isID(ins.id)||seen.has(ins.id)||!TYPES.includes(ins.type)||types.has(ins.type)||
        !ins.binding||!["workspace","fixed","none"].includes(ins.binding.mode)||
        (ins.type==="guide")!==(ins.binding.mode==="none")||
        (ins.binding.mode==="fixed"&&!shipID(ins.binding.shipId))||
        !ins.uiState||typeof ins.uiState!=="object"||Array.isArray(ins.uiState))
        return false;
      const allowed=ins.type==="cargo"?["category","sort"]:ins.type==="mining"?["filter"]:ins.type==="ship"?["tab"]:[];
      if(Object.keys(ins.uiState).some(k=>!allowed.includes(k)))return false;
      if(Object.values(ins.uiState).some(v=>typeof v!=="string"||v.length>40))return false;
      seen.add(ins.id);types.add(ins.type);
    }
  }
  return seen.has(value.activeWorkspaceId)&&seen.has(value.defaultWorkspaceId);
}
export function resolveShip(ins,ws,ships){
  if(ins.binding.mode==="none")return {ship:null,status:"unbound",shipId:null};
  const shipId=ins.binding.mode==="fixed"?ins.binding.shipId:ws.shipId;
  if(shipId===null)return {ship:null,status:"choose_ship",shipId};
  const ship=ships.get(shipId);
  return {ship:ship||null,status:ship?"ready":"unavailable",shipId};
}
export function windowId(accountId,ws,ins){
  return "ptdemo-u"+String(accountId).replace(/[^a-z0-9-]/gi,"-")+"-w"+ws.id+"-i"+ins.id;
}
export function duplicateWorkspace(ws){
  const copy=newWorkspace(graphemes(ws.name+" · копия").slice(0,32).join(""),ws.shipId);
  copy.instances=ws.instances.map(ins=>({...structuredClone(ins),id:uuid()}));
  return copy;
}
export function nextWorkspace(state,id){
  return state.workspaces.find(x=>x.id===id)||null;
}
