import { initI18n } from "../../../../web/core/i18n.js";
import { anchor, createModule, limit, resolve, restore, save } from "./model.js";
import { data } from "./fixtures.js";
import { registry } from "./registry.js";
import { button, field, node, select, tr } from "./ui.js";

await initI18n();
const $ = id => document.getElementById(id);
const layout = restore();
const windows = $("windows");
const tiles = $("tiles");
const catalog = $("catalog");
const workspace = () => layout.workspaces.find(space => space.id === layout.active);
let editing = false;
let z = 3;

function persist() {
  if (!save(layout)) $("storage-status").textContent = tr("Раскладка доступна только до перезагрузки");
}

function renderSettings() {
  const space = workspace();
  const settings = $("settings");
  settings.hidden = !editing;
  settings.replaceChildren();
  if (!editing) return;
  const name = node("input");
  name.value = space.name;
  name.maxLength = 128;
  const order = layout.workspaces.indexOf(space);
  const rename = button("Сохранить название", () => {
    const text = name.value.trim().normalize("NFC");
    if (!text || [...new Intl.Segmenter("ru").segment(text)].length > 32) {
      name.reportValidity();
      return;
    }
    space.name = text;
    render();
  });
  name.oninput = () => name.setCustomValidity(!name.value.trim() ||
    [...new Intl.Segmenter("ru").segment(name.value.trim())].length > 32 ? tr("От 1 до 32 символов") : "");
  const copy = button("Дублировать", () => {
    const duplicate = structuredClone(space);
    duplicate.id = crypto.randomUUID();
    for (const module of duplicate.modules) module.id = crypto.randomUUID();
    layout.workspaces.push(duplicate);
    layout.active = duplicate.id;
    render();
  });
  copy.disabled = layout.workspaces.length >= limit.workspaces;
  const remove = button("Удалить пространство", () => {
    if (!window.confirm(tr("Удалить раскладку этого пространства?"))) return;
    layout.workspaces.splice(order, 1);
    if (layout.start === space.id) layout.start = layout.workspaces[0].id;
    layout.active = layout.start;
    render();
  });
  remove.disabled = layout.workspaces.length < 2;
  settings.append(field("Название пространства", name), rename, copy);
  for (const [delta, label] of [[-1, "Влево"], [1, "Вправо"]]) {
    const other = order + delta;
    const move = button(label, () => {
      [layout.workspaces[order], layout.workspaces[other]] = [layout.workspaces[other], space];
      render();
    });
    move.disabled = other < 0 || other >= layout.workspaces.length;
    settings.append(move);
  }
  const start = button("Назначить стартовым", () => { layout.start = space.id; render(); });
  start.disabled = layout.start === space.id;
  settings.append(start, remove);
}

function render() {
  const space = workspace();
  const tabs = $("workspaces");
  tabs.replaceChildren();
  for (const item of layout.workspaces) {
    const tab = button(item.name, () => {
      layout.active = item.id;
      render();
      tabs.querySelector('[aria-current="page"]').focus();
    });
    if (item.id === space.id) tab.setAttribute("aria-current", "page");
    tabs.append(tab);
  }
  const add = button("Новое пространство", () => {
    const id = crypto.randomUUID();
    layout.workspaces.push({ id, name: "Новое пространство", ship: null, modules: [] });
    layout.active = id;
    render();
  });
  add.disabled = layout.workspaces.length >= limit.workspaces;
  tabs.append(add);
  const heading = $("heading");
  const tools = node("div", null, "controls");
  const picker = select([["", "Без корабля"], ...Object.entries(data.ships).map(([id, ship]) => [id, ship.name])],
    space.ship, id => {
      space.ship = id || null;
      for (const module of space.modules) {
        if (module.type === "navigation" && module.binding.mode === "workspace")
          delete module.settings.points;
      }
      renderTiles();
      persist();
    });
  picker.id = "workspace-ship";
  const edit = button(editing ? "Готово" : "Настроить", () => { editing = !editing; render(); });
  edit.id = "edit-layout";
  tools.append(field("Корабль пространства", picker), button("Добавить плитку", () => openCatalog("tile")), edit);
  const title = node("div");
  title.append(node("h1", space.name), node("small", "", "muted"));
  title.lastChild.id = "storage-status";
  heading.replaceChildren(title, tools);
  renderSettings();
  renderTiles();
  persist();
}

function renderTiles() {
  const space = workspace();
  tiles.replaceChildren(...space.modules.map((descriptor, index) => renderModule(descriptor, { space, index })));
  if (!space.modules.length) tiles.append(node("p", "Добавьте первую плитку из каталога."));
}

function renderModule(descriptor, placement) {
  const definition = registry.get(descriptor.type);
  const panel = node("article", null, "module");
  panel.dataset.instance = descriptor.id;
  panel.classList.toggle("wide", descriptor.wide);
  const header = node("div", null, "module-bar");
  const body = node("div", null, "module-body");
  const title = node("h2");
  const contextLabel = node("small", null, "context-label");
  const identity = node("div");
  identity.append(title, contextLabel);
  const tools = node("div", null, "controls");
  header.append(identity, tools);
  panel.append(header, body);

  function paint() {
    const context = resolve(descriptor.binding, placement.space ?? {}, data);
    const purpose = definition.purposes.find(([id]) => id === descriptor.purpose)[1];
    const target = context.station?.name ?? context.ship?.name ?? context.missing;
    const mode = { workspace: "Корабль пространства", fixed: "Фиксированный корабль",
      derived: "Станция стыковки", station: "Фиксированная станция" }[descriptor.binding.mode];
    title.textContent = tr(purpose);
    contextLabel.textContent = `${tr(mode)} · ${tr(target)}`;
    const change = patch => {
      const focused = document.activeElement;
      const tag = focused.tagName;
      const index = [...body.querySelectorAll(tag)].indexOf(focused);
      Object.assign(descriptor.settings, patch);
      paint();
      body.querySelectorAll(tag)[index]?.focus({ preventScroll: true });
      if (placement.space) persist();
    };
    body.replaceChildren(context.missing ? node("p", context.missing) :
      definition.render(context, descriptor.settings, change, descriptor.purpose, data));
  }

  if (placement.close) {
    tools.append(button("Закрыть", placement.close));
    header.classList.add("drag-handle");
  } else if (editing) {
    for (const [delta, label] of [[-1, "Раньше"], [1, "Позже"]]) {
      const next = placement.index + delta;
      const move = button(label, () => {
        const list = placement.space.modules;
        [list[placement.index], list[next]] = [list[next], descriptor];
        renderTiles();
        persist();
        tiles.querySelector(`[data-instance="${descriptor.id}"] button`)?.focus();
      });
      move.disabled = next < 0 || next >= placement.space.modules.length;
      tools.append(move);
    }
    const size = button(descriptor.wide ? "Сузить" : "Расширить", () => {
      descriptor.wide = !descriptor.wide;
      panel.classList.toggle("wide", descriptor.wide);
      size.textContent = tr(descriptor.wide ? "Сузить" : "Расширить");
      persist();
    });
    tools.append(size, button("Убрать", () => {
      placement.space.modules.splice(placement.index, 1);
      renderTiles();
      persist();
      $("edit-layout").focus();
    }));
  }
  paint();
  return panel;
}

function openWindow(descriptor) {
  const shell = node("section", null, "floating");
  shell.setAttribute("aria-label", tr("Плавающий инструмент"));
  const panel = renderModule(descriptor, { close: () => {
    shell.remove();
    $("tools").focus();
  } });
  shell.append(panel);
  windows.append(shell);
  const offset = (windows.children.length - 1) % 8 * 22;
  shell.style.left = `${Math.max(52, Math.min(500 + offset, innerWidth - 490))}px`;
  shell.style.top = `${85 + offset}px`;
  shell.style.zIndex = ++z;
  shell.onpointerdown = () => { shell.style.zIndex = ++z; };
  const handle = panel.querySelector(".drag-handle");
  handle.onpointerdown = event => {
    if (event.target.closest("button") || innerWidth < 700 || event.button !== 0) return;
    handle.setPointerCapture(event.pointerId);
    const rect = shell.getBoundingClientRect();
    const dx = event.clientX - rect.left;
    const dy = event.clientY - rect.top;
    handle.onpointermove = move => {
      shell.style.left = `${Math.max(0, Math.min(innerWidth - rect.width, move.clientX - dx))}px`;
      shell.style.top = `${Math.max(46, Math.min(innerHeight - 60, move.clientY - dy))}px`;
    };
    handle.onlostpointercapture = () => { handle.onpointermove = null; };
  };
  panel.querySelector("button").focus();
}

function openCatalog(initial) {
  const source = workspace();
  const root = $("catalog-body");
  const search = node("input");
  search.type = "search";
  const list = node("div", null, "catalog-list");
  let mode = initial;

  function update() {
    list.replaceChildren();
    const query = search.value.trim().toLocaleLowerCase("ru");
    for (const [type, family] of registry) {
      const group = node("section", null, "catalog-family");
      group.append(node("h3", `${family.id} · ${tr(family.name)}`));
      for (const [purpose, label] of family.purposes) {
        if (!`${family.name} ${label}`.toLocaleLowerCase("ru").includes(query)) continue;
        const warehouse = type === "inventory" && purpose !== "cargo";
        const targets = [["workspace", warehouse ? "Станция стыковки корабля" : "Корабль пространства"],
          ...Object.entries(warehouse ? data.stations : data.ships).map(([id, object]) => [id, object.name])];
        const row = node("div", null, "catalog-row");
        const target = select(targets, "workspace", () => {});
        const add = button(mode === "tile" ? "Добавить" : "Открыть", () => {
          const binding = target.value === "workspace" ? { mode: warehouse ? "derived" : "workspace" } :
            { mode: warehouse ? "station" : "fixed", id: target.value };
          if (mode === "tile") {
            if (source.modules.length >= limit.modules) return;
            source.modules.push(createModule(type, purpose, binding));
            renderTiles();
            persist();
            catalog.close();
          } else {
            catalog.close();
            openWindow(createModule(type, purpose, anchor(binding, source)));
          }
        });
        add.setAttribute("aria-label", tr(label) + " · " + add.textContent);
        add.disabled = mode === "tile" && source.modules.length >= limit.modules;
        row.append(node("strong", label), field("Привязка", target), add);
        group.append(row);
      }
      if (group.children.length > 1) list.append(group);
    }
    if (!list.children.length) list.append(node("p", "Нет совпадений"));
  }

  search.oninput = update;
  root.replaceChildren(field("Размещение", select([["tile", "Добавить в пространство"], ["window", "Открыть плавающее окно"]],
    mode, value => { mode = value; update(); })), field("Поиск модулей", search), list);
  update();
  catalog.showModal();
  search.focus();
}

$("tools").onclick = () => openCatalog("window");
$("fleet").onclick = () => $("workspace-ship").focus();
$("close-catalog").onclick = () => catalog.close();
render();
