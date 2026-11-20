import { Session } from "../../core/session.js";
import { world } from "../../core/world-store.js";
import { I18n } from "../../core/i18n.js";
import { node } from "../../ui/dom.js";
import { resourceName } from "../inventory/resource-label.js";

export const dashboard = Object.freeze({
  overview: ["Флот", "ship", "⌁"],
  operations: ["Активные операции", "tasks", "▤"],
  map: ["Галактическая карта", "galaxy", "✧"],
  market: ["Рыночные предложения", "market", "◇"],
});
const tr = (text) => I18n.t(text);
const n = (value) => new Intl.NumberFormat(I18n.language).format(value);

function table(columns, rows) {
  if (!rows.length) return node("p", tr("Нет данных"), "matrix-empty");
  const result = node("table");
  const caption = node("thead"), heading = node("tr");
  for (const title of columns) {
    const cell = node("th",tr(title));
    cell.scope = "col";
    heading.append(cell);
  }
  caption.append(heading);
  const body = node("tbody");
  for (const values of rows) {
    const row = node("tr");
    for (const value of values) {
      const cell = node("td");
      cell.append(value instanceof Node ? value : String(value ?? "—"));
      row.append(cell);
    }
    body.append(row);
  }
  result.append(caption,body);
  const scroll = node("div",undefined,"matrix-table");
  scroll.append(result);
  return scroll;
}

function map(ship) {
  const root = node("div", undefined, "matrix-map");
  const data = world.data;
  if (!data) {
    root.append(node("p", tr("Карта доступна в игровом окне"), "matrix-empty"));
    return root;
  }
  const focus = world.stars.get(ship?.star);
  const stars = (data.stars || []).filter(s => !s.deleted && (!focus || focus.galaxy === s.galaxy));
  if (!stars.length) return node("p",tr("Нет данных"),"matrix-empty");
  const canvas = node("canvas");
  canvas.width = 840;
  canvas.height = 290;
  canvas.setAttribute("role","img");
  canvas.setAttribute("aria-label",tr("Галактическая карта"));
  const ctx = canvas.getContext("2d");
  let left = Infinity, right = -Infinity, top = Infinity, bottom = -Infinity;
  for (const {x,y} of stars) {
    left = Math.min(left,x); right = Math.max(right,x);
    top = Math.min(top,y); bottom = Math.max(bottom,y);
  }
  const scale = Math.min(800 / Math.max(1,right-left),250 / Math.max(1,bottom-top));
  const project = ({x,y}) => [420 + (x-(left+right)/2)*scale,145 + (y-(top+bottom)/2)*scale];
  const displayed = new Set(stars.map(({id}) => id));
  ctx.strokeStyle = "#263c4a";
  ctx.beginPath();
  for (const lane of data.lanes || []) {
    if (!displayed.has(lane.from) || !displayed.has(lane.to)) continue;
    ctx.moveTo(...project(world.stars.get(lane.from)));
    ctx.lineTo(...project(world.stars.get(lane.to)));
  }
  ctx.stroke();
  ctx.fillStyle = "#8cabbb";
  for (const star of stars) {
    const [x,y] = project(star);
    ctx.fillRect(x,y,2,2);
  }
  if (focus && displayed.has(focus.id)) {
    const [x,y] = project(focus);
    ctx.strokeStyle = "#efad58";
    ctx.beginPath();
    ctx.arc(x,y,6,0,2*Math.PI);
    ctx.stroke();
  }
  root.append(canvas);
  if (focus) root.append(node("p",focus.name,"matrix-map-caption"));
  return root;
}

export function dashboardReading(type, ship) {
  const { fleet, workbench } = Session.data;
  if (type === "map") return map(ship);
  if (type === "overview") return table(["Корабль","Система","Состояние","Топливо"],
    (fleet?.ships || []).map(v => [
      v.name || tr(v.spec?.name),
      world.stars.get(v.star)?.name || `#${v.star}`,
      tr(v.destination ? "В пути" : v.position_mode === "docked" ? "Состыкован" : "Готов"),
      `${v.fuel} / ${v.spec?.fuel ?? "—"}`,
    ]));
  if (type === "operations") return table(["Операция","Система","Корабль"],
    [
      ...(workbench?.mining || []).map(job => [tr("Добыча"), world.stars.get(job.star)?.name || `#${job.star}`,job.ship_name]),
      ...(workbench?.research || []).map(job => [tr("Исследования"),world.stars.get(job.star)?.name || `#${job.star}`,(fleet?.ships || []).find(v=>v.id===job.ship_id)?.name]),
    ].slice(0,8));
  const orders = workbench?.orders || [];
  if (orders.length) return table(["Ресурс","Количество","Цена"],orders
    .slice(0,8).map(order => [resourceName(order.resource),n(order.quantity),`${n(order.price)} CR`]));
  const expedition = Session.data.expedition;
  // The snapshot's station offers apply to its active ship only.
  if (ship?.id === expedition?.ship?.id) return table(["Ресурс","Количество","Цена"],
    (expedition?.offers || []).slice(0,8).map(offer => [resourceName(offer.resource),n(offer.stock),`${n(offer.buy)} CR`]));
  return node("p",tr("Нет данных"),"matrix-empty");
}
