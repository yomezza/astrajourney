import { confirmTransfer, openStationTransfer, transferState } from "./transfers.js";
import { Format } from "../../core/format.js";
import { I18n } from "../../core/i18n.js";
import { Session } from "../../core/session.js";
import { node } from "../../ui/dom.js";
import { UI } from "../../ui/runtime.js";
import { resourceName } from "../inventory/resource-label.js";
import { rows, systemName } from "./views.js";

const tr = key => I18n.t(key);
const empty = text => node("p",tr(text),"matrix-empty");

function command(data, action, caption, detail, run) {
  return UI.button(caption, () => UI.confirm(tr(caption),
    [data.ship.name, "#" + data.ship.id, detail].filter(Boolean).join(" · "),
    () => run(data, action)));
}

export function miningReading(data, run) {
  if (!data) return empty("Выберите корабль для просмотра");
  const {ship, job, sites = []} = data;
  const panel = node("div",undefined,"matrix-panel-stack");
  if (job) {
    panel.append(rows([
      ["Ресурс",resourceName(job.resource)],["Количество",Format.quantity(job.quantity)],
    ]),command(data,{action:"cancel_mine"},"Остановить добычу","",run));
    return panel;
  }
  if (ship.destination || ship.local_target) return empty("Корабль в пути");
  const deposits = sites.filter(site => site.resources?.length).slice(0,12);
  for (const site of deposits) {
    const deposit = node("details",undefined,"matrix-deposit");
    const near = ship.object_id === site.id;
    deposit.dataset.studioDetails = site.id;
    deposit.append(node("summary",site.name));
    if (!near) deposit.append(command(data,{action:"move",target:site.id,mode:"approach"},"Сблизиться",site.name,run));
    for (const resource of site.resources) {
      const row = node("div",undefined,"matrix-deposit-line");
      row.append(resourceName(resource));
      if (near) row.append(command(data,{action:"mine",planet:site.planet,resource},"Добывать",site.name,run));
      deposit.append(row);
    }
    panel.append(deposit);
  }
  if (!deposits.length) {
    panel.append(empty("Исследуйте объекты системы для автоматической добычи"));
    if (!data.surveyed) panel.append(command(data,{action:"scan"},"Сканировать",data.system.name,run));
  }
  return panel;
}

export function shipActions(data, run) {
  const actions = node("div",undefined,"studio-controls");
  if (!data || data.ship.destination || data.ship.local_target) return actions;
  if (!data.docked) {
    for (const site of (data.sites || []).filter(site => site.kind === "Орбитальная станция").slice(0,3))
      actions.append(command(data,{action:"move",target:site.id,mode:"docked"},"Стыковаться",site.name,run));
  }
  if (data.ship.fuel < data.ship.spec?.fuel && data.ship.cargo?.agni > 0)
    actions.append(command(data,{action:"refuel"},"Заправить","",run));
  return actions;
}

export function logisticsReading(ship, workbench) {
  const panel = node("div",undefined,"matrix-panel-stack");
  const recovery = transferState();
  if (recovery) panel.append(recovery);
  if (!ship) {
    panel.append(empty("Выберите корабль для просмотра"));
    return panel;
  }
  if (ship.position_mode !== "docked" || ship.local_target || ship.destination) {
    panel.append(empty("Перенос на текущей станции"));
    return panel;
  }
  const station = (workbench.stations || []).find(port => port.star === ship.star && port.object_id === ship.object_id);
  if (!station) {
    panel.append(empty("Корабль недоступен"));
    return panel;
  }
  const depot = (workbench.warehouses || []).find(port => port.star === ship.star && port.station === ship.object_id);
  const stock = depot?.cargo || {};
  panel.append(node("p",station.name + " · #" + ship.id,"matrix-source-label"),
    UI.button("Переместить ресурсы",() => openStationTransfer(ship,station,stock),!!recovery));
  const materials = Object.keys({...stock,...ship.cargo}).filter(key => stock[key] > 0 || ship.cargo?.[key] > 0);
  if (!materials.length) panel.append(empty("Трюм и склад пусты"));
  for (const resource of materials) {
    const row = node("div",undefined,"matrix-deposit-line");
    const loaded = ship.cargo?.[resource] || 0, stored = stock[resource] || 0;
    row.append(resourceName(resource),node("small",Format.number(loaded) + " / " + Format.number(stored)));
    const craft = {kind:"ship",ship:ship.id};
    const warehouse = {kind:"personal",star:ship.star,station:ship.object_id};
    for (const [source,target,max,caption] of [
      [craft,warehouse,loaded,"На склад"],[warehouse,craft,stored,"В трюм"],
    ]) {
      if (!max) continue;
      const button = UI.button(caption, () => {
        const amount = node("input");
        amount.type = "number";
        amount.min = 1;
        amount.max = Math.min(max,100000);
        amount.step = 1;
        amount.value = "1";
        amount.required = true;
        const label = node("label",tr("Количество"));
        label.append(amount);
        const content = node("div");
        content.append(node("p",ship.name + " · #" + ship.id + " · " + station.name),resourceName(resource),label);
        confirmTransfer(caption,content,() => {
          if (!amount.reportValidity()) return;
          return {from:source,to:target,ship:ship.id,resource,quantity:Number(amount.value)};
        });
      },!!recovery);
      row.append(button);
    }
    panel.append(row);
  }
  return panel;
}

export function accountReading(type, workbench, profile, ship, data, run) {
  const panel = node("div",undefined,"matrix-panel-stack");
  switch (type) {
    case "studies":
      panel.append(rows([["Исследованные системы", (workbench.known || []).length],
        ["Система",systemName(ship?.star)]]));
      if (data && !data.surveyed && !ship.destination && !ship.local_target)
        panel.append(command(data,{action:"scan"},"Сканировать",data.system.name,run));
      break;
    case "finance":
      panel.append(rows([["БАЛАНС",Format.credits(profile?.credits ?? Session.data.expedition?.credits ?? 0)]]));
      for (const item of (workbench.ledger || []).slice(0,4))
        panel.append(rows([[item.reason,Format.credits(item.amount)]]));
      break;
    case "corporations":
      panel.append(rows([["Корпорация",profile?.company || tr("Без компании")],
        ["Участники",(workbench.members || []).length],["Проекты",(workbench.projects || []).length]]));
      break;
    case "profile":
      panel.append(rows([["Пилот",profile?.username],["Стаж игрока",profile?.level]]));
      break;
    case "colonies":
      panel.append(empty("Управление планетами и поселениями"));
      break;
    case "guide":
      panel.append(empty("Навигация и флот"),empty("Торговля и ресурсы"),empty("Развитие"));
      break;
    case "tasks":
      panel.append(empty("Очередь и выполнение"),rows([["Корабль",ship?.name]]));
      break;
  }
  return panel;
}
