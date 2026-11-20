import { Session } from "../../core/session.js";
import { I18n } from "../../core/i18n.js";
import { Format } from "../../core/format.js";
import { node } from "../../ui/dom.js";
import { createTable } from "../../ui/table.js";
import { resourceName } from "../inventory/resource-label.js";
import { Game } from "../runtime.js";
import { UI } from "../../ui/runtime.js";
import { gauge, systemName } from "./views.js";

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

export function fleetReading(ships, focus, chooseShip) {
  const { mining = [], research = [] } = Session.data.workbench || {};
  return table(["Корабль","Система","Состояние",["Топливо","table-number"],"Задача"], ships.map(ship => {
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
    const identity = node("div",undefined,"matrix-fleet-identity");
    identity.append(control,node("small",`#${ship.id} · ${tr(ship.spec?.name)}`));
    const miningJob = mining.find(job => job.ship_id === ship.id);
    const scan = research.find(job => job.ship === ship.id);
    const task = miningJob ? tr("Добыча") + " · " + resourceName(miningJob.resource).textContent :
      scan ? tr(scan.paused ? "На паузе" : "Сканирование") :
      ship.destination ? systemName(ship.destination) :
      ship.local_target ? tr("Манёвр") : tr("Ожидает задания");
    return [identity,systemName(ship.star),activity,gauge(ship.fuel,ship.spec?.fuel,"Топливо"),task];
  }),"Кораблей нет","Флот");
}

export function operations(ships, workbench, focus, context, run) {
  const byID = new Map(ships.map(ship => [ship.id,ship]));
  const jobs = [
    ...(workbench.mining || []).map(job => ({
      ship:job.ship_id, name:job.ship_name, title:"Добыча", tone:"working",
      detail:`${tr("К следующей фазе")} · ${Format.quantity(job.quantity)} × ${resourceName(job.resource).textContent}`,
    })),
    ...(workbench.research || []).map(job => ({
      ship:job.ship, title:"Сканирование", tone:job.paused ? "paused" : "working",
      progress:job.scanned, total:job.total, paused:job.paused,
      detail:`${Format.number(job.scanned)} / ${Format.number(job.total)}${job.paused ? " · " + tr("На паузе") : ""}`,
    })),
    ...ships.filter(ship => ship.destination || ship.local_target).map(ship => ({
      ship:ship.id, title:ship.destination ? "Перелёт" : "Манёвр", tone:"working",
      detail:ship.destination ? systemName(ship.destination) : systemName(ship.star),
    })),
  ];
  jobs.sort((left,right) => Number(right.ship === focus?.id) - Number(left.ship === focus?.id));
  const feed = node("div",undefined,"matrix-operation-feed");
  if (!jobs.length) feed.append(node("p",tr("У флота нет активных операций"),"matrix-empty"));
  for (const job of jobs.slice(0,8)) {
    const card = node("article",undefined,"matrix-operation");
    const owner = byID.get(job.ship);
    const heading = node("div",undefined,"matrix-operation-head");
    heading.append(status(job.title,job.tone),node("small",(job.name || nameOf(owner)) + " · #" + job.ship));
    card.append(heading,node("p",job.detail));
    if (job.total > 0) {
      const progress = node("progress");
      progress.max = job.total;
      progress.value = job.progress;
      progress.setAttribute("aria-label",tr("Ход операции"));
      card.append(progress);
      if (owner) card.append(UI.button(job.paused ? "Продолжить" : "Приостановить", () =>
        Game.command({action:job.paused ? "resume_research" : "pause_research",ship:job.ship})));
    } else if (context?.job && job.title === "Добыча" && context.ship.id === job.ship) {
      card.append(UI.button("Остановить добычу", () => run(context,{action:"cancel_mine"})));
    }
    feed.append(card);
  }
  return feed;
}

export function marketReading(context, source, onSource, editor, onOffer, run) {
  const { workbench } = Session.data;
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
  } else if (context?.docked) {
    const ship = context.ship;
    const port = workbench?.stations?.find(p => p.star === ship.star && p.object_id === ship.object_id);
    const offers = context.offers || [];
    view.append(node("p",`${port?.name || systemName(ship.star)} · #${ship.id}`,"matrix-source-label"));
    const list = node("div",undefined,"matrix-offer-list");
    const prices = node("div",undefined,"matrix-offer-row matrix-offer-heading");
    for (const label of ["Ресурс","Купить","Продать"]) prices.append(node("small",tr(label)));
    list.append(prices);
    for (const offer of offers) {
      const line = node("div",undefined,"matrix-offer-row");
      const material = node("span",undefined,"matrix-offer-material");
      material.append(resourceName(offer.resource),node("small",tr("В наличии") + " " + Format.quantity(offer.stock)));
      const button = UI.button("",() => onOffer(offer.resource));
      button.append(material);
      button.setAttribute("aria-pressed",String(editor.resource === offer.resource));
      button.dataset.studioFocus = `offer-${offer.resource}`;
      line.append(button,node("span",Format.credits(offer.buy)),node("span",Format.credits(offer.sell)));
      list.append(line);
    }
    if (offers.length) view.append(list);
    else view.append(node("p",tr("У станции пока нет предложений"),"matrix-empty"));
    const selected = offers.find(offer => offer.resource === editor.resource);
    if (selected) {
      const editorPanel = node("div",undefined,"matrix-offer-editor");
      const quantityLabel = node("label",tr("Количество"));
      const quantity = node("input");
      quantity.type = "number";
      quantity.min = 1;
      quantity.max = context.capacity || ship.spec?.cargo;
      quantity.value = editor.quantity || "1";
      quantity.dataset.studioFocus = "market-quantity";
      quantity.oninput = () => { editor.quantity = quantity.value; };
      quantityLabel.append(quantity);
      editorPanel.append(quantityLabel);
      for (const [action,label] of [["buy","Купить в трюм"],["sell","Продать из трюма"]]) {
        const control = UI.button(label, () => {
          if (!quantity.reportValidity() || !quantity.value) return;
          const count = Number(quantity.value);
          const price = selected[action];
          UI.confirm(tr(label),`${resourceName(selected.resource).textContent} × ${count} · ${Format.credits(count*price)} · ${ship.name} #${ship.id}`,
            () => run(context,{action,resource:selected.resource,quantity:count,expected_price:price}));
        });
        editorPanel.append(control);
      }
      view.append(editorPanel);
    }
  } else {
    view.append(node("p",tr("Для локальной торговли пристыкуйтесь к порту."),"matrix-empty"));
  }
  return view;
}
