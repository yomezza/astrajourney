import { blocked, button, field, node, select, table } from "./ui.js";

function tabs(options, current, change) {
  const bar = node("div", null, "controls");
  for (const [value, label] of options) {
    const tab = button(label, () => change(value));
    tab.setAttribute("aria-pressed", String(value === current));
    bar.append(tab);
  }
  return bar;
}

function meter(value, max, label) {
  const el = node("meter");
  el.max = max;
  el.value = value;
  el.setAttribute("aria-label", label);
  return field(`${label} · ${value} / ${max}`, el);
}

function shipView({ ship }, settings, change) {
  const body = node("div");
  const tab = settings.tab ?? "overview";
  body.append(tabs([["overview", "Обзор"], ["spec", "Характеристики"], ["equipment", "Оснащение"]],
    tab, value => change({ tab: value })));
  if (tab === "spec") {
    body.append(table(["Параметр", "Базовый", "Фактический"], [
      ["Бак", ship.base.tank, ship.tank], ["Трюм", ship.base.hold, ship.hold],
    ]));
  } else if (tab === "equipment") {
    const layout = node("div", null, "equipment");
    const model = node("img", null, "ship-diagram");
    model.src = "../../../../assets/game/space/ships/pilgrim.svg";
    model.alt = "";
    layout.append(model, table(["Оборудование", "Состояние"], ship.equipment.map(item => [item, "Установлено"])));
    body.append(layout, blocked(["Установить", "Снять"]));
  } else {
    body.append(node("p", `${ship.model} · ${ship.system} · ${ship.dock ? "Состыкован" : "В системе"}`),
      meter(ship.fuel, ship.tank, "Топливо"),
      meter(ship.cargo.reduce((total, item) => total + item[2], 0), ship.hold, "Трюм"),
      blocked(["Заправить", "Переименовать"]));
  }
  return body;
}

function inventoryView(context, settings, change, purpose) {
  const body = node("div");
  const input = context.station ? context.station[purpose] : context.ship.cargo;
  if (!input) return node("p", "Хранилище недоступно");
  const categories = [["", "Все"], ...Array.from(new Set(input.map(row => row[1])), name => [name, name])];
  const filter = node("div", null, "controls");
  const search = node("input");
  search.type = "search";
  search.value = settings.query ?? "";
  search.onchange = () => change({ query: search.value });
  filter.append(field("Поиск", search), field("Категория", select(categories, settings.category,
    category => change({ category }))), field("Сортировка", select([["name", "По названию"], ["quantity", "По количеству"]],
    settings.sort ?? "name", sort => change({ sort }))));
  body.append(filter, tabs([["table", "Таблица"], ["grid", "Сетка"]], settings.view ?? "table",
    view => change({ view })));
  if (!context.station) {
    const used = input.reduce((total, row) => total + row[2], 0);
    body.append(meter(used, context.ship.hold, "Занято"), node("p", `Свободно: ${context.ship.hold - used}`));
  }
  const visible = input.filter(([name, category]) => (!settings.category || settings.category === category) &&
    name.toLocaleLowerCase("ru").includes((settings.query ?? "").toLocaleLowerCase("ru")));
  visible.sort((a, b) => settings.sort === "quantity" ? b[2] - a[2] : a[0].localeCompare(b[0], "ru"));
  if (!visible.length) body.append(node("p", "Нет подходящих позиций"));
  else if (settings.view === "grid") {
    const grid = node("ul", null, "inventory-grid");
    for (const [name, category, count] of visible) {
      const item = node("li");
      item.append(node("strong", name), node("small", category), node("span", String(count)));
      grid.append(item);
    }
    body.append(grid);
  } else body.append(table(["Ресурс", "Категория", "Количество"], visible));
  body.append(blocked(["Переместить"]));
  return body;
}

function pathBetween(from, to, lanes) {
  const queue = [[from]];
  const seen = new Set([from]);
  for (const path of queue) {
    const current = path.at(-1);
    if (current === to) return path;
    for (const lane of lanes.filter(pair => pair.includes(current))) {
      const next = lane.find(system => system !== current);
      if (seen.has(next)) continue;
      seen.add(next);
      queue.push([...path, next]);
    }
  }
  return null;
}

function routeDiagram(points) {
  const diagram = node("ol", null, "route");
  diagram.setAttribute("aria-label", "Схема маршрута");
  for (const point of points) diagram.append(node("li", point));
  return diagram;
}

function navigationView({ ship }, settings, change, purpose, data) {
  const body = node("div");
  const interstellar = purpose === "interstellar";
  const points = settings.points ?? [];
  const options = interstellar ? data.systems : data.local[ship.system] ?? [];
  const search = node("input");
  search.type = "search";
  search.value = settings.search ?? "";
  search.onchange = () => change({ search: search.value });
  const list = options.filter(name => name.toLocaleLowerCase("ru").includes((settings.search ?? "").toLocaleLowerCase("ru")));
  const destination = select([["", "Выберите цель"], ...list.map(name => [name, name])], "", value => {
    if (!value || points.at(-1) === value) return;
    change({ points: interstellar ? [...points, value].slice(0, 20) : [value] });
  });
  body.append(field("Поиск целей", search), field("Назначение", destination));
  if (!list.length) body.append(node("p", "Подходящих целей нет"));
  if (interstellar && points.length >= 20) body.append(node("p", "Достигнут предел в 20 точек"));
  const route = [ship.system];
  for (const point of points) {
    const path = interstellar ? pathBetween(route.at(-1), point, data.lanes) : [ship.system, point];
    if (!path) {
      body.append(node("p", "Между системами нет доступного пути"));
      break;
    }
    route.push(...path.slice(1));
  }
  body.append(routeDiagram(route));
  if (points.length) {
    const editor = node("ol", null, "waypoints");
    points.forEach((point, index) => {
      const row = node("li", point);
      for (const [offset, label] of [[-1, "Выше"], [1, "Ниже"]]) {
        const move = button(label, () => {
          const next = [...points];
          [next[index], next[index + offset]] = [next[index + offset], next[index]];
          change({ points: next });
        });
        move.disabled = index + offset < 0 || index + offset >= points.length;
        row.append(move);
      }
      row.append(button("Убрать", () => change({ points: points.filter((_, i) => i !== index) })));
      editor.append(row);
    });
    body.append(editor);
  }
  body.append(blocked(interstellar ? ["Начать перелёт", "Отменить остаток маршрута", "Портал", "Спасение"] :
    ["Стыковка", "Отстыковка", "Орбита", "Посадка"]));
  return body;
}

export const registry = new Map([
  ["ship", { id: "M001", name: "Корабль", purposes: [["inspection", "Собственный корабль"]], render: shipView }],
  ["inventory", { id: "M002", name: "Инвентарь", purposes: [
    ["cargo", "Трюм корабля"], ["personal", "Персональный склад"], ["corporate", "Корпоративный склад"],
  ], render: inventoryView }],
  ["navigation", { id: "M003", name: "Навигация", purposes: [
    ["interstellar", "Межзвёздная навигация"], ["local", "Локальная навигация"],
  ], render: navigationView }],
]);
