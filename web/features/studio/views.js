import { node } from "../../ui/dom.js";
import { I18n } from "../../core/i18n.js";

export const labels = Object.freeze({
  fleet: "Корабль",
  cargo: "Трюм",
  mining: "Добыча",
});

const tr = (value) => I18n.t(value);

export function shipLabel(ship) {
  return `${ship.name || tr(ship.spec?.name)} · #${ship.id}`;
}

function rows(values) {
  const list = node("dl", undefined, "studio-readings");
  for (const [name, value] of values)
    list.append(node("dt", tr(name)), node("dd", String(value ?? "—")));
  return list;
}

export function readings(module, ship, shipID) {
  if (!ship)
    return node("p", shipID ? `${tr("Корабль недоступен")} · #${shipID}` : tr("Выберите корабль для просмотра"));
  if (module.type === "fleet")
    return rows([
      ["Класс", tr(ship.spec?.name)],
      ["Топливо", `${ship.fuel} / ${ship.spec?.fuel ?? "—"}`],
      ["Система", ship.star],
      ["Пункт назначения", ship.destination],
    ]);
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
