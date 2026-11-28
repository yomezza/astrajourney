import { Session } from "../../core/session.js";
import { I18n } from "../../core/i18n.js";
import { Format } from "../../core/format.js";
import { node } from "../../ui/dom.js";
import { resourceName } from "../inventory/resource-label.js";
import { station } from "../selectors.js";
import { gauge, rows, systemName } from "./views.js";

export const dashboard = Object.freeze({
  overview: ["Флот", "ship"],
  operations: ["Активные операции", "tasks"],
  map: ["Маршрут", "galaxy"],
  market: ["Рыночные предложения", "market"],
});
const tr = (text) => I18n.t(text);
const nameOf = ship => ship?.name || tr(ship?.spec?.name) || "—";

function table(columns, rows, empty) {
  if (!rows.length) return node("p", tr(empty), "matrix-empty");
  const result = node("table");
  const caption = node("thead"), heading = node("tr");
  for (const column of columns) {
    const [title, alignment] = Array.isArray(column) ? column : [column];
    const cell = node("th",tr(title),alignment);
    cell.scope = "col";
    heading.append(cell);
  }
  caption.append(heading);
  const body = node("tbody");
  for (const values of rows) {
    const row = node("tr");
    for (const [index,value] of values.entries()) {
      const cell = node("td",undefined,heading.children[index].className);
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

function status(title, tone) {
  const badge = node("span",tr(title),"matrix-status");
  badge.dataset.tone = tone;
  return badge;
}

function fleetReading(ships, focus, chooseShip) {
  const tasks = activeOperations(ships, Session.data.workbench || {});
  return table(["Корабль","Система","Состояние",["Топливо","matrix-numeric"],"Задача"], ships.map(ship => {
    const control = node("button",nameOf(ship),"matrix-context-link");
    control.type = "button";
    control.dataset.studioFocus = `ship-${ship.id}`;
    control.dataset.shipContext = ship.id;
    control.title = tr("Выбрать корабль для просмотра");
    control.setAttribute("aria-pressed",String(ship.id === focus?.id));
    control.onclick = () => chooseShip(ship.id);
    const identity = node("span", undefined, "matrix-fleet-identity");
    identity.append(control, node("small", `#${ship.id} · ${tr(ship.spec?.name)}`));
    const activity = ship.destination ? status("В пути","working") :
      ship.local_target ? status("Манёвр","working") :
      ship.position_mode === "docked" ? status("Состыкован","ready") : status("На позиции","quiet");
    const task = tasks.find(job => job.ship === ship.id);
    return [identity, systemName(ship.star), activity,
      gauge(ship.fuel, ship.spec?.fuel, "Топливо"), task ? tr(task.title) : "—"];
  }),"Кораблей нет");
}

export function activeOperations(ships, workbench) {
  return [
    ...(workbench.mining || []).map(job => ({
      ship:job.ship_id, name:job.ship_name, title:"Добыча", tone:"working",
      detail:`${tr("К следующей фазе")} · ${Format.quantity(job.quantity)} × ${resourceName(job.resource).textContent}`,
    })),
    ...(workbench.research || []).map(job => ({
      ship:job.ship, title:"Сканирование", tone:job.paused ? "paused" : "working",
      detail:`${Format.number(job.scanned)} / ${Format.number(job.total)}${job.paused ? " · " + tr("На паузе") : ""}`,
      progress: [job.scanned, job.total],
    })),
    ...ships.filter(ship => ship.destination || ship.local_target).map(ship => ({
      ship:ship.id, title:ship.destination ? "Перелёт" : "Манёвр", tone:"working",
      detail:ship.destination ? systemName(ship.destination) : systemName(ship.star),
    })),
  ];
}

function operations(ships, workbench, focus) {
  const byID = new Map(ships.map(ship => [ship.id,ship]));
  const jobs = activeOperations(ships, workbench);
  jobs.sort((left,right) => Number(right.ship === focus?.id) - Number(left.ship === focus?.id));
  return table(["Операция","Корабль","Ход операции"], jobs.map(job => {
    const detail = node("span", job.detail, "matrix-operation-detail");
    if (job.progress?.[1] > 0) {
      const meter = node("meter");
      meter.max = job.progress[1];
      meter.value = job.progress[0];
      meter.className = "matrix-progress";
      meter.setAttribute("aria-label", tr("Ход операции"));
      detail.append(meter);
    }
    return [status(job.title, job.tone), job.name || nameOf(byID.get(job.ship)), detail];
  }), "У флота нет активных операций");
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
    view.append(table(["Ресурс","Система",["Количество","matrix-numeric"],["Цена","matrix-numeric"]],
      (workbench?.orders || []).slice(0,8).map(order => [
        resourceName(order.resource),systemName(order.star),Format.quantity(order.quantity),Format.credits(order.price),
      ]),"Игроки пока не выставили предложений"));
  } else if (local) {
    const port = station();
    if (port?.name) view.append(node("p",port.name,"matrix-source-label"));
    view.append(table(["Ресурс",["В наличии","matrix-numeric"],["Купить","matrix-numeric"],["Продать","matrix-numeric"]],
      (expedition.offers || []).slice(0,8).map(offer => [
        resourceName(offer.resource),Format.quantity(offer.stock),Format.credits(offer.buy),Format.credits(offer.sell),
      ]),"У станции пока нет предложений"));
  } else {
    view.append(node("p",tr("Локальные цены доступны для активного корабля у станции"),"matrix-empty"));
  }
  return view;
}

function route(ship) {
  if (!ship) return node("p", tr("Выберите корабль для просмотра"), "matrix-empty");
  const summary = node("div", undefined, "matrix-module-summary");
  const fields = [["Текущая система", systemName(ship.star)]];
  if (ship.destination) fields.push(["Пункт назначения", systemName(ship.destination)]);
  summary.append(rows(fields), gauge(ship.fuel, ship.spec?.fuel, "Топливо"));
  return summary;
}

export function dashboardReading(type, ship, chooseShip) {
  const ships = Session.data.fleet?.ships || [];
  if (type === "map") return route(ship);
  if (type === "overview") return fleetReading(ships,ship,chooseShip);
  return operations(ships,Session.data.workbench || {},ship);
}
