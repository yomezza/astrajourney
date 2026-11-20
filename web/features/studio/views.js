import { node } from "../../ui/dom.js";
import { I18n } from "../../core/i18n.js";
import { assetURL } from "../../core/assets.js";
import { Format } from "../../core/format.js";
import { world } from "../../core/world-store.js";
import { Session } from "../../core/session.js";

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

function gauge(value, capacity, label) {
  const field = node("span",undefined,"matrix-gauge");
  field.append(node("span",Format.number(value) + " / " + Format.number(capacity)));
  if (capacity > 0 && Number.isFinite(value)) {
    const meter = node("meter");
    meter.max = capacity;
    meter.value = value;
    meter.setAttribute("aria-label",tr(label));
    field.append(meter);
  }
  return field;
}

function rows(values) {
  const list = node("dl", undefined, "studio-readings");
  for (const [name, value] of values) {
    const reading = node("dd");
    reading.append(value instanceof Node ? value : String(value ?? "—"));
    list.append(node("dt",tr(name)),reading);
  }
  return list;
}

export function readings(module, ship, shipID) {
  if (!ship)
    return node("p", shipID ? `${tr("Корабль недоступен")} · #${shipID}` : tr("Выберите корабль для просмотра"));
  if (module.type === "fleet") {
    const summary = node("div", undefined, "matrix-ship-summary");
    if (ship.spec?.model) {
      const image = node("img");
      image.alt = "";
      image.src = assetURL("/" + ship.spec.model);
      image.loading = "lazy";
      image.decoding = "async";
      summary.append(image);
    }
    const loaded = Object.values(ship.cargo || {}).reduce((sum,quantity) => sum + quantity,0);
    summary.append(rows([
      ["Класс", tr(ship.spec?.name)],
      ["Топливо", gauge(ship.fuel,ship.spec?.fuel,"Топливо")],
      ["Трюм", gauge(loaded,ship.spec?.cargo,"Трюм")],
      ["Система", systemName(ship.star)],
      ["Пункт назначения", systemName(ship.destination)],
    ]));
    return summary;
  }
  if (module.type === "mining")
    return rows([
      ["Экстрактор", tr(ship.extractor)],
      ["Минимальная добыча", ship.mining?.min],
      ["Максимальная добыча", ship.mining?.max],
      ["Текущая добыча", ship.mining?.current],
    ]);
  const cargo = Object.entries(ship.cargo || {}).filter(([, quantity]) => quantity > 0);
  cargo.sort(
    (a, b) => (module.filter === "quantity" ? b[1] - a[1] : 0) ||
      tr(a[0]).localeCompare(tr(b[0]), I18n.language)
  );
  return cargo.length ? rows(cargo) : node("p", tr("Трюм пуст"));
}
