import { api } from "../../core/api.js";
import { I18n } from "../../core/i18n.js";
import { Session } from "../../core/session.js";
import { refresh } from "../../core/snapshot-store.js";
import { uuid } from "../../core/uuid.js";
import { node } from "../../ui/dom.js";
import { notify } from "../../ui/feedback.js";
import { UI } from "../../ui/runtime.js";
import { resourceName } from "../inventory/resource-label.js";
import { shipLabel } from "./views.js";

const tr = key => I18n.t(key);

function choice(title, options, changed) {
  const label = node("label",tr(title));
  const select = node("select");
  select.setAttribute("aria-label",tr(title));
  label.append(select);
  for (const [id,caption] of options) {
    const item = node("option",caption);
    item.value = id;
    select.append(item);
  }
  select.onchange = () => changed(select.value);
  return {label,select};
}

let account, accountEpoch, pending, busy = false;
const key = id => "astra:pending-station-transfer:" + id;
const changed = () => window.dispatchEvent(new Event("stationtransferchange"));

function restore() {
  const owner = Session.signedIn ? Session.data.state?.user?.id : null;
  if (owner === account && accountEpoch === Session.epoch) return;
  account = owner;
  accountEpoch = Session.epoch;
  pending = null;
  busy = false;
  if (!owner) return;
  try { pending = JSON.parse(sessionStorage.getItem(key(owner))); } catch { /* storage is optional */ }
}

function remember(command) {
  pending = command;
  try {
    if (command) sessionStorage.setItem(key(account),JSON.stringify(command));
    else sessionStorage.removeItem(key(account));
  } catch { /* keep the request in memory */ }
}

async function send() {
  restore();
  if (!pending || busy) return;
  const epoch = Session.epoch;
  const current = () => epoch === Session.epoch && Session.signedIn;
  busy = true;
  changed();
  try {
    const result = await api("logistics",pending,AbortSignal.timeout(12000));
    if (!current()) return;
    if (result.mode !== "direct") throw new Error("Unexpected transfer response");
    remember(null);
    notify(tr("Действие выполнено"));
    await refresh(true);
  } catch (error) {
    if (!current()) return;
    if (error.code === "TRANSFER_REJECTED") {
      remember(null);
      notify(error.message,"error");
      await refresh(true);
    } else {
      notify(tr("Результат переноса пока неизвестен. Повтор сохраняет тот же запрос."),"error");
    }
  } finally {
    if (current()) {
      busy = false;
      changed();
    }
  }
}

export function transferState() {
  restore();
  if (!pending) return null;
  const panel = node("section",undefined,"matrix-operation");
  const status = node("p",tr(busy ? "Проверяем перенос…" : "Результат переноса пока неизвестен. Повтор сохраняет тот же запрос."));
  status.setAttribute("role","status");
  const source = pending.from.ship ? "#" + pending.from.ship : tr("Личный склад");
  const target = pending.to.ship ? "#" + pending.to.ship : tr("Личный склад");
  const items = pending.items || [{resource:pending.resource,quantity:pending.quantity}];
  panel.append(status,node("p",source + " → " + target),
    node("p",items.map(item => resourceName(item.resource).textContent + " × " + item.quantity).join(" · ")),
    UI.button("Повторить тот же запрос",send,busy));
  return panel;
}

export function confirmTransfer(title, content, compose) {
  restore();
  if (!account || pending || busy) return;
  UI.confirm(tr(title),content,async () => {
    restore();
    if (!account || pending || busy) return false;
    const body = compose();
    if (!body) return false;
    remember({...body,mode:"direct",request:uuid()});
    await send();
    return true;
  });
}

export function openStationTransfer(ship, station, stock) {
  const candidates = (Session.data.fleet?.ships || []).filter(item =>
    item.id !== ship.id && item.star === ship.star && item.object_id === ship.object_id &&
    item.position_mode === "docked" && !item.destination && !item.local_target);
  const cargo = item => ({kind:"ship",ship:item.id});
  const warehouse = {kind:"personal",star:ship.star,station:ship.object_id};
  const panel = node("div",undefined,"matrix-panel-stack");
  panel.append(node("p",station.name));
  const materials = node("div",undefined,"matrix-panel-stack");
  let destination, quantityFields = [];
  const selected = choice("Источник",[
    ["ship",shipLabel(ship)],["warehouse",tr("Личный склад")],
  ],render);
  panel.append(selected.label,materials);

  function render() {
    const fromShip = selected.select.value === "ship";
    materials.replaceChildren();
    const options = fromShip ?
      [["warehouse",tr("Личный склад")],...candidates.map(item => [item.id,shipLabel(item)])] :
      [[ship.id,shipLabel(ship)]];
    destination = choice("Назначение",options,() => {});
    materials.append(destination.label);
    quantityFields = [];
    const source = fromShip ? ship.cargo : stock;
    for (const [id,count] of Object.entries(source || {}).filter(([,n]) => n > 0)) {
      const label = node("label");
      const quantity = node("input");
      quantity.type = "number";
      quantity.min = 0;
      quantity.max = Math.min(count,100000);
      quantity.step = 1;
      quantity.value = "0";
      const name = resourceName(id);
      quantity.setAttribute("aria-label",name.textContent);
      label.append(name,quantity);
      materials.append(label);
      quantityFields.push([id,quantity]);
    }
    if (!quantityFields.length) materials.append(node("p",tr("В источнике нет ресурсов"),"matrix-empty"));
  }
  render();
  confirmTransfer("Переместить ресурсы",panel,() => {
    if (!quantityFields.every(([,field]) => field.reportValidity())) return;
    const items = quantityFields.map(([resource,field]) => ({resource,quantity:Number(field.value)}))
      .filter(item => item.quantity > 0);
    if (!items.length) {
      notify(tr("Укажите количество ресурсов"),"error");
      return;
    }
    const from = selected.select.value === "ship" ? cargo(ship) : warehouse;
    const value = destination.select.value;
    const to = value === "warehouse" ? warehouse : {kind:"ship",ship:Number(value)};
    return {from,to,ship:ship.id,items};
  });
}
