import { world } from "../../core/world-store.js";
import { Session } from "../../core/session.js";
import { I18n } from "../../core/i18n.js";
import { node } from "../../ui/dom.js";
import { UI } from "../../ui/runtime.js";
import { createCartography } from "../maps/cartography.js";

const tr = value => I18n.t(value);
let source, connections, artwork;

function neighbors(start) {
  if (source !== world.data) {
    source = world.data;
    connections = new Map();
    for (const {from,to} of source.lanes || []) {
      for (const [a,b] of [[from,to],[to,from]]) {
        if (!connections.has(a)) connections.set(a,[]);
        connections.get(a).push(b);
      }
    }
  }
  const ids = new Set([start]);
  for (const id of ids) {
    for (const neighbor of connections.get(id) || []) {
      const star = world.stars.get(neighbor);
      if (star && !star.deleted) ids.add(neighbor);
      if (ids.size === 17) return ids;
    }
  }
  return ids;
}

export function mapReading(module, ship, context, change, run) {
  const root = node("section",undefined,"matrix-map");
  const current = world.stars.get(ship?.star);
  if (!world.data || !current || current.deleted) {
    root.append(node("p",tr("Карта доступна после загрузки системы"),"matrix-empty"));
    return root;
  }
  const ids = neighbors(current.id);
  const stars = [...ids].map(id => world.stars.get(id));
  const requested = world.stars.get(module.system_id);
  const selected = requested && !requested.deleted ? requested : current;
  if (!ids.has(selected.id)) stars.push(selected);
  const known = new Set((Session.data.workbench?.known || []).map(({star}) => star));
  const canvas = node("canvas");
  canvas.width = 840;
  canvas.height = 360;
  canvas.setAttribute("role","img");
  canvas.setAttribute("aria-label",tr("Галактическая карта"));
  const ctx = canvas.getContext("2d");
  const left = Math.min(...stars.map(p => p.x)), right = Math.max(...stars.map(p => p.x));
  const top = Math.min(...stars.map(p => p.y)), bottom = Math.max(...stars.map(p => p.y));
  const scale = Math.min(690 / Math.max(1,right-left),270 / Math.max(1,bottom-top));
  const points = stars.map(star => ({
    star,x:420+(star.x-(left+right)/2)*scale,y:180+(star.y-(top+bottom)/2)*scale,
  }));
  const byID = new Map(points.map(p => [p.star.id,p]));
  artwork ||= createCartography();
  artwork.background(ctx,840,360,{x:0,y:0,zoom:1},"galaxy");
  ctx.strokeStyle = "#557483";
  ctx.beginPath();
  for (const point of points) for (const id of connections.get(point.star.id) || []) {
    const other = byID.get(id);
    if (!other || id < point.star.id) continue;
    ctx.moveTo(point.x,point.y);
    ctx.lineTo(other.x,other.y);
  }
  ctx.stroke();
  for (const point of points) {
    const {x,y,star} = point;
    artwork.star(ctx,x,y,3,star.class,known.has(star.id) || star === current);
    if (star === selected || star === current) {
      ctx.strokeStyle = star === selected ? "#efad58" : "#78c7d9";
      ctx.beginPath();
      ctx.arc(x,y,10,0,2*Math.PI);
      ctx.stroke();
      artwork.label(ctx,star.name,x+11,y-7,ctx.strokeStyle);
    }
  }
  canvas.onclick = event => {
    const box = canvas.getBoundingClientRect();
    const x = (event.clientX-box.left)*840/box.width;
    const y = (event.clientY-box.top)*360/box.height;
    let best = 25, target;
    for (const point of points) {
      const distance = Math.hypot(point.x-x,point.y-y);
      if (distance < best) {
        best = distance;
        target = point.star.id;
      }
    }
    if (target) change(target);
  };
  const panel = node("div",undefined,"matrix-map-tools");
  const label = node("label",tr("Выбранная система"));
  const field = node("select");
  field.dataset.studioFocus = "map-system";
  for (const star of stars) {
    const option = node("option",star.name);
    option.value = star.id;
    field.append(option);
  }
  field.value = selected.id;
  field.onchange = () => change(Number(field.value));
  label.append(field);
  panel.append(label,node("small",tr(known.has(selected.id) ? "Система исследована" : "Неизвестная система")));
  const search = node("input");
  search.type = "search";
  search.placeholder = tr("Поиск системы");
  search.setAttribute("aria-label",search.placeholder);
  const matches = node("div",undefined,"matrix-map-matches");
  search.oninput = () => {
    matches.replaceChildren();
    const query = search.value.trim().toLocaleLowerCase(I18n.language);
    if (!query) return;
    let count = 0;
    for (const star of world.stars.values()) {
      if (star.deleted || !star.name.toLocaleLowerCase(I18n.language).includes(query)) continue;
      const choice = UI.button(star.name,() => change(star.id));
      matches.append(choice);
      if (++count === 8) break;
    }
    if (!count) matches.append(node("p",tr("Системы не найдены"),"matrix-empty"));
  };
  panel.append(search,matches);
  if (context && !ship.destination && !ship.local_target) {
    const action = selected === current ? "scan" : "travel";
    panel.append(UI.button(action === "scan" ? "Сканировать" : "Перелёт", () =>
      UI.confirm(tr(action === "scan" ? "Сканировать" : "Перелёт"),
        `${ship.name} · #${ship.id} → ${selected.name}`,
        () => run(context,{action,to:selected.id})), action === "scan" && !!context.surveyed));
  }
  root.append(canvas,panel);
  return root;
}
