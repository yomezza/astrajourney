import { Session } from "../../core/session.js";
import { world } from "../../core/world-store.js";
import { I18n } from "../../core/i18n.js";
import { Format } from "../../core/format.js";
import { node } from "../../ui/dom.js";
import { createTable } from "../../ui/table.js";
import { resourceName } from "../inventory/resource-label.js";
import { station } from "../selectors.js";
import { systemName } from "./views.js";

export const dashboard = Object.freeze({
  overview: ["Флот", "ship"],
  operations: ["Активные операции", "tasks"],
  map: ["Галактическая карта", "galaxy"],
  market: ["Рыночные предложения", "market"],
});
const tr = (text) => I18n.t(text);
const nameOf = ship => ship?.name || tr(ship?.spec?.name) || "—";

function table(columns, rows, empty, label) {
  if (!rows.length) return node("p", tr(empty), "matrix-empty");
  return createTable(columns, rows, {className:"matrix-table",label});
}

function status(title, tone) {
  const badge = node("span",tr(title),"matrix-status");
  badge.dataset.tone = tone;
  return badge;
}

function fleetReading(ships, focus, chooseShip) {
  return table(["Корабль","Система","Состояние",["Топливо","table-number"]], ships.map(ship => {
    const control = node("button",nameOf(ship),"matrix-context-link");
    control.type = "button";
    control.dataset.studioFocus = `ship-${ship.id}`;
    control.dataset.shipContext = ship.id;
    control.title = tr("Выбрать корабль для просмотра");
    control.setAttribute("aria-pressed",String(ship.id === focus?.id));
    control.onclick = () => chooseShip(ship.id);
    const activity = ship.destination ? status("В пути","working") :
      ship.local_target ? status("Манёвр","working") :
      ship.position_mode === "docked" ? status("Состыкован","ready") : status("На позиции","quiet");
    const fuel = Format.number(ship.fuel) + " / " + Format.number(ship.spec?.fuel);
    return [control,systemName(ship.star),activity,fuel];
  }),"Кораблей нет","Флот");
}

function operations(ships, workbench, focus) {
  const byID = new Map(ships.map(ship => [ship.id,ship]));
  const jobs = [
    ...(workbench.mining || []).map(job => ({
      ship:job.ship_id, name:job.ship_name, title:"Добыча", tone:"working",
      detail:`${tr("К следующей фазе")} · ${Format.quantity(job.quantity)} × ${resourceName(job.resource).textContent}`,
    })),
    ...(workbench.research || []).map(job => ({
      ship:job.ship, title:"Сканирование", tone:job.paused ? "paused" : "working",
      detail:`${Format.number(job.scanned)} / ${Format.number(job.total)}${job.paused ? " · " + tr("На паузе") : ""}`,
    })),
    ...ships.filter(ship => ship.destination || ship.local_target).map(ship => ({
      ship:ship.id, title:ship.destination ? "Перелёт" : "Манёвр", tone:"working",
      detail:ship.destination ? systemName(ship.destination) : systemName(ship.star),
    })),
  ];
  jobs.sort((left,right) => Number(right.ship === focus?.id) - Number(left.ship === focus?.id));
  return table(["Операция","Корабль","Ход операции"],jobs.slice(0,8).map(job => [
    status(job.title,job.tone),job.name || nameOf(byID.get(job.ship)),job.detail,
  ]),"У флота нет активных операций","Активные операции");
}

export function marketReading(ship, source, onSource) {
  const { expedition, workbench } = Session.data;
  // Expedition offers belong to the active ship's dock, even when Studio views another ship.
  const local = expedition?.docked && ship?.id === expedition.ship?.id;
  const view = node("div",undefined,"matrix-market");
  const buttons = node("div",undefined,"matrix-source-switch");
  buttons.setAttribute("role","group");
  buttons.setAttribute("aria-label",tr("Источник предложений"));
  for (const [key,title] of [["local","Станция"],["orders","Предложения игроков"]]) {
    const button = node("button",tr(title));
    button.type = "button";
    button.setAttribute("aria-pressed",String(key === source));
    button.dataset.studioFocus = key;
    button.onclick = () => onSource(key);
    buttons.append(button);
  }
  view.append(buttons);
  if (source === "orders") {
    view.append(table(["Ресурс","Система",["Количество","table-number"],["Цена","table-number"]],
      (workbench?.orders || []).slice(0,8).map(order => [
        resourceName(order.resource),systemName(order.star),Format.quantity(order.quantity),Format.credits(order.price),
      ]),"Игроки пока не выставили предложений","Предложения игроков"));
  } else if (local) {
    const port = station();
    if (port?.name) view.append(node("p",port.name,"matrix-source-label"));
    view.append(table(["Ресурс",["В наличии","table-number"],["Купить","table-number"],["Продать","table-number"]],
      (expedition.offers || []).slice(0,8).map(offer => [
        resourceName(offer.resource),Format.quantity(offer.stock),Format.credits(offer.buy),Format.credits(offer.sell),
      ]),"У станции пока нет предложений","Локальный рынок"));
  } else {
    view.append(node("p",tr("Локальные цены доступны для активного корабля у станции"),"matrix-empty"));
  }
  return view;
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

export function dashboardReading(type, ship, chooseShip) {
  const ships = Session.data.fleet?.ships || [];
  if (type === "map") return map(ship);
  if (type === "overview") return fleetReading(ships,ship,chooseShip);
  return operations(ships,Session.data.workbench || {},ship);
}
