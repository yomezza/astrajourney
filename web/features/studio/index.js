import { api } from "../../core/api.js";
import { I18n } from "../../core/i18n.js";
import { Preferences } from "../../core/preferences.js";
import { RequestScope, Session } from "../../core/session.js";
import { node } from "../../ui/dom.js";
import { lineIcon } from "../../ui/icons.js";
import { Modules, UI } from "../../ui/runtime.js";
import { labels, readings, shipLabel } from "./views.js";
import { activeOperations, dashboard, dashboardReading, marketReading } from "./dashboard.js";
import { Game } from "../runtime.js";
import { loadWorld, world } from "../../core/world-store.js";

const tr = (text) => I18n.t(text);
const instanceID = (module) => `studio-${module.id}`;
const activeWorkspace = (layout) => layout.workspaces.find(({ id }) => id === layout.active);
const ownedShips = () => Session.data.fleet?.ships || [];
const types = {...labels, ...Object.fromEntries(Object.entries(dashboard).map(([type,[label]]) => [type,label]))};
const actionsFor = {fleet:"ship",cargo:"cargo",mining:"research"};
const actionTitles = {
  ship:"Открыть корабль", cargo:"Открыть трюм", research:"Открыть добычу",
  tasks:"Открыть планировщик", galaxy:"Открыть карту", market:"Открыть рынок",
};
const starter = ["overview", "operations", "map", "market", "fleet"];
const presets = [
  ["Командование", starter],
  ["Добыча", ["fleet", "mining", "cargo", "operations"]],
  ["Торговля", ["market", "cargo", "overview"]],
];
const appearance = {
  overview: ["ship", "Парк кораблей", "Флот"],
  operations: ["tasks", "Бортовые операции", "Активные задачи"],
  map: ["galaxy", "Навигация", "Маршрут"],
  market: ["market", "Торговля", "Рыночная сводка"],
  fleet: ["ship", "Управление флотом", "Корабль"],
  mining: ["research", "Ресурсы системы", "Добыча"],
  cargo: ["cargo", "Ресурсы", "Трюм"],
};
function newModule(type) {
  return {id:id(), type, binding:dashboard[type] ? "none" : "workspace", ship_id:0,
    view:"card", ...(["overview", "market", "mining"].includes(type) ? {wide:true} : {})};
}

// getRandomValues is also available on installations served over plain HTTP.
function id() {
  const bytes = crypto.getRandomValues(new Uint8Array(16));
  bytes[6] = (bytes[6] & 15) | 64;
  bytes[8] = (bytes[8] & 63) | 128;
  const hex = Array.from(bytes, (b) => b.toString(16).padStart(2, "0")).join("");
  return `${hex.slice(0,8)}-${hex.slice(8,12)}-${hex.slice(12,16)}-${hex.slice(16,20)}-${hex.slice(20)}`;
}

function button(text, action, disabled = false) {
  const control = node("button", tr(text), "terminal-action");
  control.type = "button";
  control.disabled = disabled;
  control.onclick = action;
  return control;
}

function select(label, options, value, change) {
  const control = node("select");
  control.setAttribute("aria-label",tr(label));
  for (const [key, text] of options) {
    const option = node("option", text);
    option.value = key;
    control.append(option);
  }
  control.value = value;
  control.onchange = () => change(control.value);
  const field = node("label", tr(label));
  field.append(control);
  return field;
}

function shipOptions(current) {
  const options = [[0, tr("Активный корабль")]];
  for (const ship of ownedShips()) options.push([ship.id, shipLabel(ship)]);
  if (current && !ownedShips().some(({ id }) => id === current))
    options.push([current, `${tr("Корабль недоступен")} · #${current}`]);
  return options;
}

function legacyGeometry(type) {
  const key = { ...actionsFor, ...Object.fromEntries(Object.entries(dashboard).map(([key,entry]) => [key,entry[1]])) }[type];
  const value = Preferences.read("module:" + key, {});
  const {x, y, w, h, pinned} = value;
  if (![x,y,w,h].every(Number.isFinite) || w < 160 || h < 160 ||
      x < 0 || y < 0 || Math.max(x,y,w,h) > 8192) return undefined;
  return {x: Math.round(x), y: Math.round(y), w: Math.round(w), h: Math.round(h),
    pinned: !!pinned, minimized: false};
}

export function initStudio() {
  const host = document.getElementById("studio");
  if (!host) return;
  const shortcut = document.getElementById("studio-shortcut");
  const overview = document.querySelector(".matrix-overview");
  const headerContext = document.getElementById("matrix-header-context");
  const stripShip = overview.querySelector("[data-studio-ship]");
  const stripOperations = overview.querySelector("[data-studio-operations]");
  shortcut.onclick = () => {
    host.tabIndex = -1;
    host.classList.add("matrix-focus");
    (controls?.querySelector('.studio-tabs [aria-current="true"]')?.getClientRects().length
      ? controls.querySelector('.studio-tabs [aria-current="true"]')
      : controls?.querySelector(".studio-picker select") || host).focus();
    host.scrollIntoView({block:"nearest"});
  };
  host.addEventListener("focusout", event => {
    if (!host.contains(event.relatedTarget)) host.classList.remove("matrix-focus");
  });
  const requests = new RequestScope();
  const mounted = new Map(), cards = new Map();
  const marketSources = new WeakMap();
  let grid, mapRequest, dragID;
  let layout, revision, timer, request, writing, loading, dirty, failed, conflict, pendingWrite;
  let controls, status, actions, shipSignature;
  const workspace = () => activeWorkspace(layout);
  const activeShipID = () => Session.data.expedition?.ship?.id || 0;
  const focusShipID = () => workspace().ship_id || activeShipID();
  const moduleShipID = module => module.binding === "fixed" ? module.ship_id :
    module.binding === "workspace" ? focusShipID() : 0;

  function refreshStrip() {
    const shipID = focusShipID();
    const ship = ownedShips().find(({id}) => id === shipID);
    const context = ship ? shipLabel(ship) : shipID ? `${tr("Корабль недоступен")} · #${shipID}` : "—";
    stripShip.textContent = context;
    headerContext.lastElementChild.textContent = context;
    headerContext.hidden = false;
    stripOperations.textContent = activeOperations(ownedShips(), Session.data.workbench || {}).length;
  }

  function moduleAction(module) {
    const target = dashboard[module.type]?.[1] || actionsFor[module.type];
    const action = button(actionTitles[target], () => {
      const marketSource = marketSources.get(module) || "local";
      if (target === "market" && marketSource === "orders") return Game.openMarket(marketSource);
      if (target === "galaxy") {
        const opening = Modules.open(target);
        const shipID = focusShipID();
        if (ownedShips().some(({id}) => id === shipID))
          window.dispatchEvent(new CustomEvent("findship", {detail: shipID}));
        else window.dispatchEvent(new Event("showgalaxy"));
        return opening;
      }
      if (target === "tasks") return Modules.open(target);

      const shipID = dashboard[module.type] ? focusShipID() : moduleShipID(module);
      const ship = ownedShips().find(({id}) => id === shipID);
      if (!ship) return;
      const open = () => target === "market" ? Game.openMarket(marketSource) : Modules.open(target);
      if (ship.id === activeShipID()) return open();
      const current = request;
      const source = workspace();
      UI.confirm(tr("Сменить активный корабль"), `${tr("Сделать активным и открыть окно")}: ${shipLabel(ship)}?`, async () => {
        if (!current?.current() || workspace() !== source || !source.modules.includes(module) ||
          (dashboard[module.type] ? focusShipID() : moduleShipID(module)) !== ship.id ||
          !ownedShips().some(({id}) => id === ship.id)) return false;
        if (ship.id !== activeShipID() && !await Game.command({action: "select_ship", ship: ship.id})) return false;
        if (!current.current() || workspace() !== source || !source.modules.includes(module)) return true;
        if (ship.id !== activeShipID()) return false;
        open();
        return true;
      });
    });
    action.classList.add("matrix-action");
    action.prepend(lineIcon("right"));
    return action;
  }

  function report(text = "") {
    const following = failed || conflict ? actions : null;
    if (status.nextSibling !== following) host.insertBefore(status, following);
    status.textContent = tr(text);
    actions.replaceChildren();
    if (failed && !conflict)
      actions.append(button("Повторить", () => layout ? flush() : load()));
    if (conflict)
      actions.append(button("Загрузить сохранённую раскладку", () => load()));
  }

  function frame() {
    host.before(overview);
    grid = node("div", undefined, "matrix-grid");
    controls = node("div");
    status = node("p");
    status.setAttribute("role","status");
    actions = node("div", undefined, "studio-controls matrix-recovery");
    host.replaceChildren(controls, actions, grid, status);
    host.hidden = false;
    shortcut.hidden = false;
  }

  function changed() {
    dirty = true;
    if (conflict) return;
    report("Есть несохранённые изменения");
    clearTimeout(timer);
    timer = setTimeout(flush, 250);
  }

  async function flush() {
    if (!layout || !dirty || writing || conflict || !request?.current()) return;
    const scope = request;
    pendingWrite ||= {revision, layout:structuredClone(layout)};
    const attempted = pendingWrite;
    writing = true;
    dirty = false;
    failed = false;
    report("Сохраняем раскладку…");
    try {
      const result = await api("workspaces", attempted, scope.signal);
      if (!scope.current()) return;
      revision = result.revision;
      pendingWrite = null;
      dirty = JSON.stringify(layout) !== JSON.stringify(attempted.layout);
      report(dirty ? "Есть несохранённые изменения" : "Раскладка сохранена");
    } catch (error) {
      if (!scope.current()) return;
      dirty = failed = true;
      if (error.status >= 400 && error.status < 500) pendingWrite = null;
      conflict = error.status === 409;
      report(error.message);
    } finally {
      if (scope.current()) {
        writing = false;
        if (dirty && !failed) flush();
      }
    }
  }

  function saveGeometry(module, geometry) {
    if (geometry.w < 160 || geometry.h < 160 || Math.max(geometry.x,geometry.y,geometry.w,geometry.h) > 8192) return;
    const next = {...geometry, minimized: !!module.geometry?.minimized};
    if (module.geometry && Object.entries(next).every(([key,value]) => module.geometry[key] === value)) return;
    module.geometry = next;
    changed();
  }

  function replaceReadings(entry, output) {
    if (entry.data.firstElementChild?.isEqualNode(output)) return;
    const scrollSelector = ".matrix-table, .matrix-resource-list";
    const oldScroll = entry.data.querySelector(scrollSelector);
    const top = oldScroll?.scrollTop || 0, left = oldScroll?.scrollLeft || 0;
    const active = document.activeElement;
    const focus = entry.data.contains(active) && active.dataset.studioFocus;
    entry.data.replaceChildren(output);
    entry.data.querySelector(scrollSelector)?.scrollTo(left,top);
    if (focus) entry.data.querySelector(`[data-studio-focus="${CSS.escape(focus)}"]`)?.focus({preventScroll:true});
  }

  function paintWindow(module, entry, rebuildControls = false) {
    const shipID = moduleShipID(module);
    const ship = ownedShips().find(({id}) => id === shipID);
    if (dashboard[module.type]) {
      const focus = ownedShips().find(({id}) => id === focusShipID());
      entry.action.disabled = module.type === "overview" && !focus;
      if (module.type === "market") {
        const source = marketSources.get(module) || "local";
        entry.action.disabled = source === "local" && !focus;
        replaceReadings(entry, marketReading(focus,source,key => {
          if (key === source) return;
          marketSources.set(module,key);
          for (const view of [cards.get(instanceID(module)),mounted.get(instanceID(module))])
            if (view) paintWindow(module,view);
        }));
        return;
      }
      replaceReadings(entry,dashboardReading(module.type, focus, selectShip));
      return;
    }
    const output = readings(module, ship, shipID);
    entry.action.disabled = !ship;
    entry.title.textContent = ship ? shipLabel(ship) : tr(labels[module.type]);
    if (rebuildControls || entry.shipSignature !== shipSignature) {
      const binding = select("Привязка корабля", [
        ["workspace",tr("Корабль пространства")], ["fixed",tr("Закрепить корабль")], ["none",tr("Без привязки")],
      ], module.binding, (value) => {
        if (value === "fixed" && !ship && ownedShips().length !== 1) {
          paintWindow(module, entry, true);
          return;
        }
        module.binding = value;
        module.ship_id = value === "fixed" ? (ship || ownedShips()[0]).id : 0;
        paintWindow(module, entry, true);
        entry.tools.querySelector("select")?.focus();
        changed();
      });
      binding.querySelector('option[value="fixed"]').disabled =
        !ship && ownedShips().length !== 1 && module.binding !== "fixed";
      entry.tools.replaceChildren(binding);
      if (module.binding === "fixed") {
        const field = select("Закреплённый корабль", shipOptions(module.ship_id), module.ship_id, (value) => {
          module.ship_id = Number(value);
          paintWindow(module, entry);
          changed();
        });
        field.querySelector('option[value="0"]').disabled = true;
        entry.tools.append(field);
      }
      if (module.type === "cargo") entry.tools.append(select("Сортировка трюма", [
        ["name",tr("По названию")], ["quantity",tr("По количеству")],
      ], module.filter || "name", (value) => {
        module.filter = value;
        paintWindow(module, entry);
        changed();
      }));
      entry.shipSignature = shipSignature;
    }
    replaceReadings(entry,output);
  }

  function mount(module) {
    const key = instanceID(module);
    const offset = (mounted.size % 6) * 26;
    const root = Modules.create(key, types[module.type], 430, 330, {
      minContentWidth: 175,
      layout: {
        read: () => module.geometry || {x: 44 + offset, y: 260 + offset},
        save: (geometry) => saveGeometry(module, geometry),
        minimize: (value) => {
          const geometry = module.geometry || {x: 8,y: 85,w: 430,h: 330,pinned:false};
          module.geometry = { ...geometry, minimized: value };
          changed();
        },
      },
    });
    const view = node("section", undefined, "studio-view");
    const tools = node("div", undefined, "studio-controls"), data = node("div");
    const title = node("h3", tr(appearance[module.type][2]));
    const action = moduleAction(module);
    view.append(title, data, action, tools);
    root.append(view);
    const entry = {title, tools, data, action};
    mounted.set(key,entry);
    Modules.header(key).closest("dialog").classList.add("studio-window");
    paintWindow(module,entry,true);
    Modules.open(key);
  }

  function showWindows() {
    for (const key of mounted.keys()) Modules.destroy(key);
    mounted.clear();
    if (!document.body.classList.contains("planet-mode"))
      for (const module of workspace().modules) if (module.view === "window") mount(module);
  }

  function requestMap() {
    if (world.data || mapRequest || !workspace().modules.some(item => item.type === "map")) return;
    const scope = request;
    mapRequest = loadWorld().then(() => {
      if (scope.current()) refreshShips();
    }).catch(() => {}).finally(() => {
      if (scope.current()) mapRequest = null;
    });
  }

  function drawCards(focus) {
    const w = workspace();
    grid.replaceChildren();
    cards.clear();
    const reorder = (module, index, target) => {
      if (target === index) return;
      w.modules.splice(index,1);
      w.modules.splice(target,0,module);
      drawCards(module.id);
      changed();
    };
    for (const [index,module] of w.modules.entries()) {
      const card = node("article",undefined,"matrix-card");
      card.classList.toggle("matrix-wide", !!module.wide);
      card.dataset.module = module.type;
      const head = node("div",undefined,"matrix-card-bar");
      const [icon, group, name] = appearance[module.type];
      const label = node("h2");
      label.append(lineIcon(icon), document.createTextNode(tr(types[module.type])));
      label.draggable = grid.classList.contains("matrix-editing");
      label.tabIndex = -1;
      label.ondragstart = event => {
        dragID = module.id;
        event.dataTransfer.setData("text/plain",module.id);
        event.dataTransfer.effectAllowed = "move";
      };
      label.ondragend = () => { dragID = null; };
      card.ondragover = event => { if (dragID && dragID !== module.id) event.preventDefault(); };
      card.ondrop = event => {
        const from = w.modules.findIndex(({id}) => id === dragID);
        if (from < 0 || from === index) return;
        event.preventDefault();
        const moving = w.modules[from];
        dragID = null;
        reorder(moving,from,index);
      };
      const tool = (icon, caption, run, disabled = false) => {
        const b = button("",run,disabled);
        b.append(lineIcon(icon));
        b.title = tr(caption);
        b.setAttribute("aria-label",tr(caption));
        return b;
      };
      const tools = node("div",undefined,"matrix-card-actions");
      const float = tool("maximize","Открыть отдельное окно",() => {
        if (mounted.has(instanceID(module))) return Modules.focus(instanceID(module));
        module.view = "window";
        module.geometry ||= legacyGeometry(module.type);
        mount(module);
        changed();
      });
      const resize = tool("wide","Широкая карточка",() => {
        if (module.wide) delete module.wide;
        else module.wide = true;
        card.classList.toggle("matrix-wide", !!module.wide);
        resize.setAttribute("aria-pressed", String(!!module.wide));
        changed();
      });
      resize.setAttribute("aria-pressed", String(!!module.wide));
      const editing = node("div",undefined,"matrix-edit-actions");
      editing.append(
        tool("left","Переместить карточку влево",() => reorder(module,index,index-1),index === 0),
        tool("right","Переместить карточку вправо",() => reorder(module,index,index+1),index === w.modules.length-1),
        resize,
        tool("close","Убрать карточку",() => {
          w.modules.splice(index,1);
          mounted.delete(instanceID(module));
          Modules.destroy(instanceID(module));
          drawCards(w.modules[index]?.id || w.modules[index-1]?.id);
          if (!w.modules.length) controls.querySelector(".matrix-add")?.focus();
          changed();
        })
      );
      tools.append(editing,float);
      head.append(label,tools);
      const body = node("div",undefined,"matrix-card-body");
      const data = node("div");
      const settings = node("details",undefined,"matrix-card-settings");
      const binding = node("div",undefined,"studio-controls");
      if (!dashboard[module.type]) settings.append(node("summary",tr("Настройки карточки")),binding);
      const caption = node("div", undefined, "matrix-card-caption");
      const title = node("h3", tr(name), "matrix-context-title");
      caption.append(node("p", tr(group), "matrix-eyebrow"), title);
      const action = moduleAction(module);
      body.append(data, action, settings);
      card.append(head, caption, body);
      grid.append(card);
      const entry = {title, tools:binding, data, action};
      cards.set(instanceID(module),entry);
      paintWindow(module,entry,true);
      if (module.id === focus) label.focus();
    }
    if (w.modules.length < 16) {
      const add = button("Добавить модуль", () => openCatalog(controls.querySelector(".matrix-catalog")));
      add.prepend(lineIcon("plus"));
      add.className = "matrix-add-card";
      add.setAttribute("aria-label", tr("Добавить модуль в пространство"));
      grid.append(add);
    }
    requestMap();
  }

  function openCatalog(catalog) {
    if (!catalog) return;
    catalog.open = true;
    catalog.querySelector("input").focus();
    catalog.scrollIntoView({block:"nearest"});
  }

  function switchTo(value, selector = ".studio-picker select") {
    layout.active = value;
    render(true, selector);
    changed();
  }

  function selectShip(value) {
    const w = workspace();
    w.ship_id = Number(value);
    const control = controls.querySelector(".matrix-workspace-heading label:not(.studio-picker) select");
    if (control) control.value = value;
    refreshShips(true);
    changed();
  }

  function render(switchWindows = false, focusSelector) {
    if (switchWindows) showWindows();
    const w = workspace(), index = layout.workspaces.indexOf(w);
    const toolbar = node("div", undefined, "studio-controls");
    const picker = select("Рабочее пространство", layout.workspaces.map(({id,name}) => [id,name]), w.id, switchTo);
    picker.classList.add("studio-picker");
    const tabs = node("nav", undefined, "studio-tabs");
    tabs.setAttribute("aria-label", tr("Рабочие пространства"));
    for (const item of layout.workspaces) {
      const tab = button(item.name, () => switchTo(item.id, '.studio-tabs [aria-current="true"]'));
      tab.textContent = item.name;
      tab.prepend(lineIcon("studio"));
      tab.setAttribute("aria-current", String(item.id === w.id));
      tabs.append(tab);
    }
    const shipPicker = select("Корабль пространства", shipOptions(w.ship_id), w.ship_id, selectShip);
    shipPicker.firstChild.replaceWith(node("span", tr("Корабль")));
    toolbar.append(picker, shipPicker);

    const manage = node("details",undefined,"matrix-manage");
    manage.open = controls.querySelector(".matrix-manage")?.open || false;
    manage.append(node("summary",tr("Настроить пространство")));
    manage.ontoggle = () => {
      if (!manage.isConnected) return;
      grid.classList.toggle("matrix-editing",manage.open);
      for (const title of grid.querySelectorAll(".matrix-card-bar h2")) title.draggable = manage.open;
    };
    const edit = node("form", undefined, "studio-controls");
    const name = node("input");
    name.value = w.name;
    name.required = true;
    name.maxLength = 256;
    const field = node("label",tr("Название пространства"));
    field.append(name);
    const rename = button("Переименовать", null);
    rename.type = "submit";
    edit.onsubmit = (event) => {
      event.preventDefault();
      const value = name.value.trim().normalize("NFC");
      const count = [...new Intl.Segmenter(I18n.language, {granularity:"grapheme"}).segment(value)].length;
      if (!count || count > 32) {
        report("Название должно содержать от 1 до 32 символов");
        name.focus();
        return;
      }
      w.name = value;
      render(false,"input");
      changed();
    };
    const create = (duplicate) => {
      const next = duplicate ? structuredClone(w) :
        {name: tr("Новое пространство"), ship_id:0, modules:[], customized:true};
      next.id = id();
      for (const module of next.modules) module.id = id();
      layout.workspaces.push(next);
      layout.active = next.id;
      manage.open = true;
      render(true,"input");
      changed();
    };
    const move = (delta) => {
      [layout.workspaces[index],layout.workspaces[index+delta]] =
        [layout.workspaces[index+delta],layout.workspaces[index]];
      render(false, ".matrix-edit-toggle");
      changed();
    };
    const remove = () => {
      const current = request;
      UI.confirm(tr("Удалить пространство"), tr("Удалить рабочее пространство?"), () => {
        if (!current.current() || workspace() !== w || layout.workspaces.length < 2) return false;
        layout.workspaces = layout.workspaces.filter(({id}) => id !== w.id);
        if (layout.default === w.id) layout.default = layout.workspaces[0].id;
        layout.active = layout.default;
        render(true, '.studio-tabs [aria-current="true"]');
        changed();
        return true;
      });
    };
    edit.append(field,rename,
      button("Создать пространство", () => create(false),layout.workspaces.length >= 10),
      button("Дублировать", () => create(true),layout.workspaces.length >= 10),
      button("Переместить влево", () => move(-1),index === 0),
      button("Переместить вправо", () => move(1),index === layout.workspaces.length-1),
      button("Назначить стартовым", () => {
        layout.default = w.id;
        render(false, ".matrix-edit-toggle");
        changed();
      }, layout.default === w.id),
      button("Открыть стартовое", () => {
        layout.active = layout.default;
        render(true,"select");
        changed();
      },layout.default === w.id),
      button("Удалить пространство",remove,layout.workspaces.length < 2)
    );
    manage.append(edit);

    const catalog = node("details",undefined,"matrix-catalog");
    catalog.open = controls.querySelectorAll("details")[1]?.open || false;
    catalog.append(node("summary",tr("Добавить модуль")));
    const search = node("input");
    search.type = "search";
    search.setAttribute("aria-label",tr("Поиск модулей"));
    const modules = node("div", undefined, "studio-controls");
    const filter = () => {
      modules.replaceChildren();
      for (const [type,label] of Object.entries(types)) {
        if (!tr(label).toLocaleLowerCase().includes(search.value.trim().toLocaleLowerCase())) continue;
        modules.append(button(label,() => {
          const module = newModule(type);
          w.modules.push(module);
          drawCards(module.id);
          filter();
          changed();
        },w.modules.length >= 16));
      }
    };
    search.oninput = filter;
    filter();
    catalog.append(search,modules);
    const heading = node("div",undefined,"matrix-workspace-heading");
    const title = node("h1",w.name,"matrix-workspace-title");
    const identity = node("div", undefined, "matrix-workspace-identity");
    const headingLine = node("div", undefined, "matrix-heading-line");
    headingLine.append(title, node("span", "MATRIX × STUDIO", "matrix-wordmark"));
    identity.append(node("p", tr("Командный терминал"), "matrix-eyebrow"), headingLine);
    const open = button("Добавить модуль",() => openCatalog(catalog),w.modules.length >= 16);
    open.classList.add("matrix-add");
    open.prepend(lineIcon("plus"));
    toolbar.append(open);
    heading.append(identity,toolbar);
    const tabline = node("div",undefined,"matrix-tabline");
    const plus = button("",() => create(false),layout.workspaces.length >= 10);
    plus.append(lineIcon("plus"));
    plus.classList.add("matrix-new-workspace");
    plus.setAttribute("aria-label", tr("Новое пространство"));
    const editToggle = button("Настроить пространство", () => {
      manage.open = !manage.open;
      if (manage.open) name.focus();
    });
    editToggle.prepend(lineIcon("settings"));
    editToggle.classList.add("matrix-edit-toggle");
    editToggle.setAttribute("aria-expanded", String(manage.open));
    manage.addEventListener("toggle", () => editToggle.setAttribute("aria-expanded", String(manage.open)));
    const layoutTools = node("div", undefined, "matrix-layout-tools");
    const copy = button("Копия", () => create(true), layout.workspaces.length >= 10);
    copy.prepend(lineIcon("studio"));
    const removeCurrent = button("Удалить", remove, layout.workspaces.length < 2);
    removeCurrent.prepend(lineIcon("close"));
    layoutTools.append(copy, editToggle, removeCurrent);
    tabline.append(tabs, plus, layoutTools);
    controls.replaceChildren(tabline,heading,overview,manage,catalog);
    refreshStrip();
    grid.classList.toggle("matrix-editing",manage.open);
    drawCards();
    if (focusSelector) {
      const candidate = controls.querySelector(focusSelector);
      (candidate?.getClientRects().length ? candidate :
        controls.querySelector(".studio-picker select"))?.focus();
    }
  }

  function refreshShips(force = false) {
    if (!layout) return;
    refreshStrip();
    const signature = JSON.stringify(ownedShips().map(ship => [ship.id,shipLabel(ship)]));
    const update = force || signature !== shipSignature;
    shipSignature = signature;
    if (update) {
      // Keep the editor draft and current keyboard focus during background snapshots.
      const old = controls.querySelector(".matrix-workspace-heading label:not(.studio-picker) select");
      const updateOptions = () => {
        if (!old?.isConnected || !layout) return;
        const field = select("Корабль пространства",shipOptions(workspace().ship_id),workspace().ship_id,() => {});
        old.replaceChildren(...field.querySelector("select").options);
        old.value = workspace().ship_id;
      };
      if (old && document.activeElement === old) old.addEventListener("blur",updateOptions,{once:true});
      else updateOptions();
    }
    for (const module of workspace().modules) {
      const entry = mounted.get(instanceID(module));
      if (entry) paintWindow(module,entry,force);
      const card = cards.get(instanceID(module));
      if (card) paintWindow(module,card,force);
    }
  }

  async function load() {
    if (loading) return;
    const scope = requests.begin();
    request = scope;
    loading = true;
    failed = false;
    frame();
    report("Загружаем рабочие пространства…");
    try {
      const result = await api("workspaces", undefined, scope.signal);
      if (!scope.current()) return;
      layout = result.layout;
      const first = layout.workspaces[0];
      if (layout.workspaces.length === 1 && first.name === "Астра" && !first.customized && !first.modules.length) {
        first.name = tr(presets[0][0]);
        first.modules = presets[0][1].map(newModule);
        for (const [name, modules] of presets.slice(1))
          layout.workspaces.push({id:id(), name:tr(name), ship_id:0, modules:modules.map(newModule)});
      }
      for (const item of layout.workspaces) {
        if (!item.customized && !item.modules.length) item.modules = starter.map(newModule);
        item.customized = true;
      }
      revision = result.revision;
      dirty = conflict = false;
      pendingWrite = null;
      shipSignature = JSON.stringify(ownedShips().map(ship => [ship.id,shipLabel(ship)]));
      render(true);
      report();
    } catch (error) {
      if (!scope.current()) return;
      failed = true;
      report(error.message);
    } finally {
      if (scope.current()) loading = false;
    }
  }

  window.addEventListener("gamedata", () => {
    if (!Session.signedIn) return;
    if (!layout && !loading && !failed) load();
    else refreshShips();
  });
  window.addEventListener("moduleclose", ({detail}) => {
    if (!Session.signedIn || !layout || !mounted.delete(detail)) return;
    const w = workspace();
    const module = w.modules.find((item) => instanceID(item) === detail);
    if (!module) return;
    module.view = "card";
    Modules.destroy(detail);
    changed();
  });
  window.addEventListener("planetmodechange", ({detail}) => {
    if (!detail && layout && !mounted.size) showWindows();
  });
  window.addEventListener("languagechange", () => {
    if (!layout) return;
    render();
    refreshShips(true);
  });
  window.addEventListener("universechange", () => refreshShips(true));
  window.addEventListener("keydown", event => {
    if (!layout || event.defaultPrevented || document.body.classList.contains("planet-mode") ||
      event.target.closest?.("input, textarea, select, [contenteditable], dialog")) return;
    if (event.key === "/") {
      event.preventDefault();
      openCatalog(controls.querySelector(".matrix-catalog"));
    }
    if (event.shiftKey && ["ArrowLeft","ArrowRight"].includes(event.key)) {
      const current = layout.workspaces.indexOf(workspace());
      const next = layout.workspaces[current + (event.key === "ArrowLeft" ? -1 : 1)];
      if (next) {
        event.preventDefault();
        switchTo(next.id, '.studio-tabs [aria-current="true"]');
      }
    }
  });
  window.addEventListener("signedout", () => {
    requests.invalidate();
    clearTimeout(timer);
    layout = pendingWrite = null;
    for (const key of mounted.keys()) Modules.destroy(key);
    mounted.clear();
    cards.clear();
    mapRequest = dragID = null;
    writing = loading = dirty = failed = conflict = false;
    host.hidden = shortcut.hidden = headerContext.hidden = true;
    headerContext.lastElementChild.textContent = "";
    stripShip.textContent = stripOperations.textContent = "—";
    host.before(overview);
    host.replaceChildren();
  });
}
