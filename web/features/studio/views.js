import { node } from "../../ui/dom.js";
import { I18n } from "../../core/i18n.js";
import { assetURL } from "../../core/assets.js";
import { Format } from "../../core/format.js";
import { usedCargo } from "../../core/ship-capacity.js";
import { world } from "../../core/world-store.js";
import { Session } from "../../core/session.js";
import { resourceName } from "../inventory/resource-label.js";

export const labels = Object.freeze({
  fleet: "Корабль",
  cargo: "Трюм",
  mining: "Добыча",
});

const tr = (value) => I18n.t(value);

export function shipLabel(ship) {
  return `${ship.name || tr(ship.spec?.name)} · #${ship.id}`;
}

export function systemName(id) {
  if (!id) return "—";
  const current = Session.data.expedition?.system;
  return world.stars.get(id)?.name || (current?.id === id ? current.name : `#${id}`);
}

export function gauge(value, capacity, label) {
  const field = node("div", undefined, "matrix-gauge");
  const caption = node("div", undefined, "matrix-gauge-caption");
  caption.append(node("span", tr(label)), node("strong", `${Format.number(value)} / ${Format.number(capacity)}`));
  field.append(caption);
  if (capacity > 0 && Number.isFinite(value)) {
    const meter = node("meter");
    meter.max = capacity;
    meter.value = value;
    meter.setAttribute("aria-label", tr(label));
    field.append(meter);
  }
  return field;
}

export function rows(values) {
  const list = node("dl", undefined, "studio-readings");
  for (const [name, value] of values) {
    const label = node("dt");
    label.append(name instanceof Node ? name : tr(name));
    const reading = node("dd");
    reading.append(value instanceof Node ? value : String(value ?? "—"));
    const pair = node("div");
    pair.append(label, reading);
    list.append(pair);
  }
  return list;
}

export function readings(module, ship, shipID) {
  if (!ship)
    return node("p", shipID ? `${tr("Корабль недоступен")} · #${shipID}` : tr("Выберите корабль для просмотра"), "matrix-empty");
  if (module.type === "fleet") {
    const summary = node("div", undefined, "matrix-ship-summary");
    if (ship.spec?.model) {
      const projection = node("div", undefined, "matrix-ship-projection");
      const image = node("img");
      image.alt = "";
      image.src = assetURL("/" + ship.spec.model);
      image.loading = "lazy";
      image.decoding = "async";
      projection.append(image);
      summary.append(projection);
    }
    const telemetry = node("div", undefined, "matrix-ship-telemetry");
    telemetry.append(
      node("strong", ship.name || tr(ship.spec?.name), "matrix-ship-name"),
      node("p", [tr(ship.spec?.role || ship.spec?.name), systemName(ship.star)].filter(Boolean).join(" · "), "matrix-source-label"),
      gauge(ship.fuel, ship.spec?.fuel, "Топливо"),
      gauge(usedCargo(ship), ship.spec?.cargo, "Трюм"),
    );
    if (ship.destination) telemetry.append(rows([["Пункт назначения", systemName(ship.destination)]]));
    summary.append(telemetry);
    return summary;
  }
  if (module.type === "mining") {
    const jobs = (Session.data.workbench?.mining || []).filter(job => job.ship_id === ship.id);
    const summary = node("div", undefined, "matrix-module-summary");
    const extractor = ship.extractor === "universal" ? "Универсальный" : ship.extractor;
    summary.append(rows([["Экстрактор", tr(extractor)], ["Текущая добыча", Format.number(ship.mining?.current)]]));
    if (jobs.length) summary.append(rows(jobs.map(job => [resourceName(job.resource), Format.quantity(job.quantity)])));
    else summary.append(node("p", tr("Нет активной добычи"), "matrix-empty"));
    return summary;
  }
  const cargo = Object.entries(ship.cargo || {})
    .filter(([, quantity]) => quantity > 0)
    .map(([resource, quantity]) => ({name: resourceName(resource), quantity}));
  cargo.sort((a, b) => (module.filter === "quantity" ? b.quantity - a.quantity : 0) ||
    a.name.textContent.localeCompare(b.name.textContent, I18n.language));
  const summary = node("div", undefined, "matrix-module-summary");
  summary.append(gauge(usedCargo(ship), ship.spec?.cargo, "Заполнено"));
  if (cargo.length) {
    const resources = node("div", undefined, "matrix-resource-list");
    resources.append(rows(cargo.map(({name, quantity}) => [name, Format.quantity(quantity)])));
    summary.append(resources);
  } else if (!usedCargo(ship)) summary.append(node("p", tr("Трюм пуст"), "matrix-empty"));
  return summary;
}
