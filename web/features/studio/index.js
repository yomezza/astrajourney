import { api } from "../../core/api.js";
import { I18n } from "../../core/i18n.js";
import { Preferences } from "../../core/preferences.js";
import { RequestScope, Session } from "../../core/session.js";
import { node } from "../../ui/dom.js";
import { notify } from "../../ui/feedback.js";
import { lineIcon } from "../../ui/icons.js";
import { Modules, UI } from "../../ui/runtime.js";
import { readings, shipLabel } from "./views.js";
import { fleetReading, marketReading, operations } from "./dashboard.js";
import { createStudioContext } from "./context.js";
import { mapReading } from "./map.js";
import { accountReading, logisticsReading, miningReading, shipActions } from "./panels.js";
import { registry } from "./registry.js";
import { initStudioComms } from "./communications.js";
import { uuid as id } from "../../core/uuid.js";
import { Game, Scheduler } from "../runtime.js";
import { openModule } from "../shell/module-routes.js";
import { loadWorld, world } from "../../core/world-store.js";

const tr = (text) => I18n.t(text);
const instanceID = (module) => `studio-${module.id}`;
const activeWorkspace = (layout) => layout.workspaces.find(({ id }) => id === layout.active);
const ownedShips = () => Session.data.fleet?.ships || [];
const starter = ["overview","operations","map","market","fleet"];
const presets = [
  ["Командование", starter],
  ["Добыча", ["operations","mining","cargo","fleet","map"]],
  ["Торговля", ["market","logistics","cargo","overview","map"]],
];
function newModule(type) {
  const {binding = "none",wide} = registry[type];
  return {id:id(), type, binding, ship_id:0, view:"card", ...(wide ? {wide} : {})};
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
  const key = {fleet:"ship",mining:"research",operations:"tasks",map:"galaxy"}[type] ||
    registry[type].target || type;
  const value = Preferences.read("module:" + key, {});
  const {x, y, w, h, pinned} = value;
  if (![x,y,w,h].every(Number.isFinite) || w < 160 || h < 160 ||
      x < 0 || y < 0 || Math.max(x,y,w,h) > 8192) return undefined;
  return {x: Math.round(x), y: Math.round(y), w: Math.round(w), h: Math.round(h),
    pinned: !!pinned, minimized: false};
}

function sessionLayoutEqual(left,right) {
  const standard = layout => layout.workspaces.map(workspace => [
    workspace.id,workspace.name,workspace.ship_id,!!workspace.customized,workspace.modules.map(module => [
      module.id,module.type,module.binding,module.ship_id,module.view || "card",!!module.wide,
      module.tab || "",module.system_id || 0,module.filter || "",
      module.geometry ? ["x","y","w","h","pinned","minimized"].map(key => module.geometry[key]) : null,
    ]),
  ]);
  return left.active === right.active && left.default === right.default &&
    JSON.stringify(standard(left)) === JSON.stringify(standard(right));
}

export function initStudio() {
  const host = document.getElementById("studio");
  if (!host) return;
  const shortcut = document.getElementById("studio-shortcut");
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
  const marketEditors = new WeakMap();
  const context = createStudioContext(() => refreshShips());
  const telemetry = document.querySelector("#game .overview-stats");
  const focusMetric = node("strong");
  const pilot = document.querySelector("#game .matrix-pilot");
  const contextShortcut = button("",() => {
    controls.querySelector(".matrix-workspace-heading label:not(.studio-picker) select")?.focus();
  });
  contextShortcut.id = "matrix-ship-shortcut";
  contextShortcut.hidden = true;
  document.querySelector(".header-actions").prepend(contextShortcut);
  const sidebar = document.getElementById("sidebar");
  const navItems = new Map();
  const shipLink = sidebar.querySelector('[data-open="ship"]');
  const navItem = (type, name, icon, run) => {
    const item = button("",run);
    item.className = "matrix-studio-nav";
    item.hidden = true;
    item.dataset.nav = name;
    item.title = tr(name);
    item.setAttribute("aria-label",tr(name));
    const iconSlot = node("span",undefined,"nav-icon");
    iconSlot.append(lineIcon(icon));
    const label = node("span",tr(name),"nav-label");
    label.dataset.i18n = name;
    item.append(iconSlot,label);
    navItems.set(type,item);
    return item;
  };
  shipLink.before(navItem("overview","Флот","ship",() => openType("overview")));
  shipLink.after(navItem("cargo","Трюм","cargo",() => openType("cargo")));
  sidebar.querySelector('[data-open="chat"]').after(navItem("notifications","Уведомления","notifications",
    () => openModule("notifications")));
  let comms;
  let grid, mapRequest, dragID;
  let layout, revision, timer, request, writing, loading, dirty, failed, conflict, pendingWrite;
  let controls, status, actions, shipSignature;
  const workspace = () => activeWorkspace(layout);
  const draftKey = () => "astra:studio-draft:" + Session.data.state.user.id;
  const draft = {
    read() {
      try { return JSON.parse(sessionStorage.getItem(draftKey())); } catch { return null; }
    },
    remember() {
      try { sessionStorage.setItem(draftKey(),JSON.stringify({revision,layout,pendingWrite})); } catch { /* storage can be disabled */ }
    },
    clear() {
      try { sessionStorage.removeItem(draftKey()); } catch { /* storage can be disabled */ }
    },
  };

  function report(text = "") {
    status.textContent = tr(text);
    actions.replaceChildren();
    if (failed && !conflict)
      actions.append(button("Повторить", () => layout ? flush() : load()));
    if (conflict)
      actions.append(button("Загрузить сохранённую раскладку", () => {
        draft.clear();
        load();
      }));
  }

  function frame() {
    grid = node("div", undefined, "matrix-grid");
    controls = node("div");
    status = node("p");
    status.setAttribute("role","status");
    actions = node("div", undefined, "studio-controls matrix-recovery");
    if (telemetry && !focusMetric.isConnected) {
      const metric = node("article",undefined,"panel matrix-ship-metric");
      const caption = node("small",tr("Корабль пространства"));
      caption.dataset.i18n = "Корабль пространства";
      metric.append(caption,focusMetric);
      telemetry.append(metric);
    }
    host.replaceChildren(controls, status, actions, grid);
    if (telemetry) controls.after(telemetry);
    document.querySelector("#profile-menu .menu")?.prepend(pilot);
    contextShortcut.hidden = false;
    for (const entry of navItems.values()) entry.hidden = false;
    if (!comms) comms = initStudioComms();
    host.hidden = false;
    shortcut.hidden = false;
  }

  function changed() {
    dirty = true;
    draft.remember();
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
    draft.remember();
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
      if (dirty) draft.remember();
      else draft.clear();
      report(dirty ? "Есть несохранённые изменения" : "Раскладка сохранена");
    } catch (error) {
      if (!scope.current()) return;
      dirty = failed = true;
      if (error.status >= 400 && error.status < 500) pendingWrite = null;
      draft.remember();
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

  function replaceReadings(entry, output, force = false) {
    const rendered = entry.data.firstElementChild;
    const screen = output.querySelector("[data-studio-model]");
    const previous = entry.data.querySelector("[data-studio-model]");
    if (!force && rendered) {
      const comparable = previous ? rendered.cloneNode(true) : rendered;
      if (previous) comparable.querySelector("[data-studio-model]").replaceChildren();
      if (comparable.isEqualNode(output)) return;
    }
    if (screen) {
      const reusable = previous?.dataset.studioModel === screen.dataset.studioModel;
      if (reusable) {
        screen.replaceWith(previous);
        previous.dataset.studioMoving = screen.dataset.studioMoving;
      }
      UI.updateHologram(reusable ? previous : screen,screen.dataset.studioModel,
        screen.getAttribute("aria-label"),screen.dataset.studioMoving === "true");
    }
    const oldScroll = entry.data.querySelector(".matrix-table");
    const top = oldScroll?.scrollTop || 0, left = oldScroll?.scrollLeft || 0;
    const folds = new Set([...entry.data.querySelectorAll("details[data-studio-details][open]")]
      .map(item => item.dataset.studioDetails));
    const active = document.activeElement;
    const focus = entry.data.contains(active) && active.dataset.studioFocus;
    entry.data.replaceChildren(output);
    for (const item of entry.data.querySelectorAll("details[data-studio-details]"))
      item.open = folds.has(item.dataset.studioDetails);
    const scroll = entry.data.querySelector(".matrix-table");
    scroll?.scrollTo(left,top);
    if (active === oldScroll) scroll?.focus({preventScroll:true});
    if (focus) entry.data.querySelector(`[data-studio-focus="${CSS.escape(focus)}"]`)?.focus({preventScroll:true});
  }

  function contextualShip(module) {
    const focusID = workspace().ship_id || Session.data.expedition?.ship?.id || 0;
    const shipID = module.binding === "fixed" ? module.ship_id :
      registry[module.type].binding && module.binding === "none" ? 0 : focusID;
    return {shipID, ship:ownedShips().find(({id}) => id === shipID)};
  }

  function repaint(module, rebuild = false) {
    for (const entry of [cards.get(instanceID(module)),mounted.get(instanceID(module))])
      if (entry) paintWindow(module,entry,rebuild);
  }

  function paintWindow(module, entry, rebuildControls = false) {
    const {shipID,ship} = contextualShip(module);
    const descriptor = registry[module.type];
    const current = descriptor.expedition && ship ? context.get(shipID) : {};
    const data = current.data?.ship.star === ship?.star && current.data?.ship.object_id === ship?.object_id ?
      current.data : undefined;
    const workbench = Session.data.workbench || {};
    const name = entry.content.firstElementChild;
    name.textContent = descriptor.binding ? (ship ? shipLabel(ship) : tr(descriptor.label)) : "";
    let output;
    switch (module.type) {
      case "overview":
        output = fleetReading(ownedShips(),ship,selectShip);
        break;
      case "operations":
        output = operations(ownedShips(),workbench,ship,data,context.run);
        break;
      case "market": {
        const editor = marketEditors.get(module) || {};
        marketEditors.set(module,editor);
        output = marketReading(data,module.tab || "local",key => {
          if (key === (module.tab || "local")) return;
          module.tab = key;
          changed();
          repaint(module);
        },editor,resource => {
          editor.resource = editor.resource === resource ? null : resource;
          repaint(module);
        },context.run);
        break;
      }
      case "map": {
        const key = [ship?.id,ship?.star,ship?.destination,ship?.local_target,data?.ship.object_id,
          data?.surveyed,module.system_id,I18n.language,workbench.known?.length].join("|");
        if (entry.mapSource === world.data && entry.mapFocus === key && !rebuildControls) return;
        entry.mapFocus = key;
        entry.mapSource = world.data;
        output = mapReading(module,ship,data,selected => {
          module.system_id = selected;
          changed();
          repaint(module);
        },context.run);
        break;
      }
      case "fleet":
      case "mining":
      case "cargo":
        output = node("div",undefined,"matrix-panel-stack");
        output.append(readings(module,ship,shipID));
        if (module.type === "fleet") output.append(shipActions(data,context.run));
        if (module.type === "mining" && ship) output.append(miningReading(data,context.run));
        break;
      case "logistics":
        output = logisticsReading(ship,workbench);
        break;
      case "tasks":
        if (entry.planner && ship && Scheduler.render) {
          const key = [shipID,I18n.language,workbench.time].join("|");
          if (entry.plannerKey !== key) {
            entry.plannerKey = key;
            Scheduler.render(entry.data,{shipID,namespace:instanceID(module),filter:module.filter,
              onFilter: value => { module.filter = value; changed(); }})
              .catch(error => {
                if (entry.data.isConnected) entry.data.replaceChildren(node("p",error.message,"matrix-empty"));
              });
          }
          return;
        }
        output = accountReading(module.type,workbench,Session.data.profile,ship,data,context.run);
        break;
      default:
        output = accountReading(module.type,workbench,Session.data.profile,ship,data,context.run);
    }
    if (!data && current.pending) output.append(node("p",tr("Загружаем данные корабля…"),"matrix-empty"));
    else if (current.error) output.append(node("p",current.error,"matrix-empty"));
    if (!descriptor.binding) {
      replaceReadings(entry,output,module.type === "map");
      return;
    }
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
        repaint(module,true);
        entry.tools.querySelector("select")?.focus();
        changed();
      });
      binding.querySelector('option[value="fixed"]').disabled =
        !ship && ownedShips().length !== 1 && module.binding !== "fixed";
      entry.tools.replaceChildren(binding);
      if (module.binding === "fixed") {
        const field = select("Закреплённый корабль", shipOptions(module.ship_id), module.ship_id, (value) => {
          module.ship_id = Number(value);
          repaint(module,true);
          entry.tools.querySelectorAll("select")[1]?.focus();
          changed();
        });
        field.querySelector('option[value="0"]').disabled = true;
        entry.tools.append(field);
      }
      if (module.type === "cargo") entry.tools.append(select("Сортировка трюма", [
        ["name",tr("По названию")], ["quantity",tr("По количеству")],
      ], module.filter || "name", (value) => {
        module.filter = value;
        repaint(module,true);
        entry.tools.querySelector("label:last-child select")?.focus();
        changed();
      }));
      entry.shipSignature = shipSignature;
    }
    replaceReadings(entry,output);
  }

  function mount(module) {
    const key = instanceID(module);
    const offset = (mounted.size % 6) * 24;
    function placement() {
      const stage = host.getBoundingClientRect();
      const left = stage.width ? stage.left : 12;
      const available = stage.width || innerWidth - left - 12;
      const mapWidth = Math.min(710,Math.max(380,Math.floor(available * .54)));
      const shipWidth = Math.min(420,Math.max(330,Math.floor(available * .42)));
      const [w,h] = {tasks:[810,630],map:[mapWidth,450],fleet:[shipWidth,340],market:[520,480]}[module.type] || [430,330];
      const width = Math.min(w,available);
      const x = module.type === "fleet" ? left + available - width :
        left + Math.min(offset,available - width);
      return {x:Math.round(x),y:Math.round(Math.max(86,Math.min(210,innerHeight * .24)) + offset),
        w:Math.round(width),h,pinned:false,minimized:false};
    }
    const initial = placement();
    const root = Modules.create(key,registry[module.type].label,initial.w,initial.h,{
      minContentWidth:175,
      layout:{
        read:() => module.geometry || placement(),
        reflow:() => module.geometry ? null : placement(),
        save: geometry => saveGeometry(module,geometry),
        minimize: value => {
          const geometry = module.geometry || placement();
          module.geometry = {...geometry,minimized:value};
          changed();
        },
      },
    });
    const view = node("section", undefined, "studio-view");
    const tools = node("div", undefined, "studio-controls"), data = node("div");
    view.append(node("h3"),tools,data);
    root.append(view);
    const entry = {content:view,tools,data,planner:module.type === "tasks"};
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

  function showModule(module) {
    if (mounted.has(instanceID(module))) return Modules.focus(instanceID(module));
    module.view = "window";
    module.geometry ||= legacyGeometry(module.type);
    mount(module);
    changed();
  }

  function openType(type) {
    if (!layout) return;
    const items = workspace().modules;
    let module = items.find(item => item.type === type);
    if (!module) {
      if (items.length >= 16) return notify(tr("Не более 16 модулей в пространстве"),"error");
      module = newModule(type);
      items.push(module);
      drawCards();
    }
    showModule(module);
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
      const label = node("h2",tr(registry[module.type].label));
      const identity = node("div",undefined,"matrix-card-name");
      identity.append(lineIcon(registry[module.type].icon),label);
      label.draggable = true;
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
      const show = () => showModule(module);
      const float = tool("maximize","Открыть отдельное окно",show);
      const resize = tool("wide","Широкая карточка",() => {
        if (module.wide) delete module.wide;
        else module.wide = true;
        card.classList.toggle("matrix-wide", !!module.wide);
        resize.setAttribute("aria-pressed", String(!!module.wide));
        changed();
      });
      resize.setAttribute("aria-pressed", String(!!module.wide));
      const editing = node("div",undefined,"matrix-edit-actions");
      const remove = node("div",undefined,"matrix-remove-action");
      editing.append(
        tool("left","Переместить карточку влево",() => reorder(module,index,index-1),index === 0),
        tool("right","Переместить карточку вправо",() => reorder(module,index,index+1),index === w.modules.length-1),
      );
      remove.append(tool("close","Убрать карточку",() => {
          w.modules.splice(index,1);
          mounted.delete(instanceID(module));
          Modules.destroy(instanceID(module));
          drawCards(w.modules[index]?.id || w.modules[index-1]?.id);
          if (!w.modules.length) controls.querySelector(".matrix-add")?.focus();
          changed();
        })
      );
      tools.append(editing,resize,float,remove);
      head.append(identity,tools);
      const body = node("div",undefined,"matrix-card-body");
      const data = node("div");
      const settings = node("details",undefined,"matrix-card-settings");
      const binding = node("div",undefined,"studio-controls");
      if (registry[module.type].binding) settings.append(node("summary",tr("Настройки карточки")),binding);
      const title = node("h3",undefined,"matrix-context-title");
      body.append(title,data);
      if (settings.childElementCount) body.append(settings);
      const target = registry[module.type].target;
      if (target) body.append(button("Открыть", () => openModule(target)));
      if (module.type === "tasks") body.append(button("Открыть планировщик",show));
      if (module.type === "operations") body.append(button("Открыть планировщик",() => openType("tasks")));
      if (module.type === "market") body.append(button("Рынок игроков",() => Game.openMarket("orders")));
      card.append(head,body);
      grid.append(card);
      const entry = {content:body,tools:binding,data};
      cards.set(instanceID(module),entry);
      paintWindow(module,entry,true);
      if (module.id === focus) label.focus();
    }
    {
      const add = button("+ " + tr("Добавить модуль"),() => openCatalog(controls.querySelector(".matrix-catalog")),w.modules.length >= 16);
      add.className = "matrix-add-card";
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
      tab.prepend(lineIcon("studio"));
      tab.setAttribute("aria-current", String(item.id === w.id));
      tabs.append(tab);
    }
    toolbar.append(picker, select("Корабль пространства", shipOptions(w.ship_id), w.ship_id, selectShip));

    const manage = node("details",undefined,"matrix-manage");
    manage.open = controls.querySelector(".matrix-manage")?.open || false;
    manage.append(node("summary",tr("Настроить пространство")));
    manage.ontoggle = () => {
      if (!manage.isConnected) return;
      grid.classList.toggle("matrix-editing",manage.open);
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
      render(true,"input");
      changed();
    };
    const move = (delta) => {
      [layout.workspaces[index],layout.workspaces[index+delta]] =
        [layout.workspaces[index+delta],layout.workspaces[index]];
      render(false,"summary");
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
        render(false,"summary");
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
    catalog.open = controls.querySelector(".matrix-catalog")?.open || false;
    catalog.append(node("summary",tr("Добавить модуль")));
    const search = node("input");
    search.type = "search";
    search.setAttribute("aria-label",tr("Поиск модулей"));
    search.placeholder = tr("Поиск модулей");
    const modules = node("div", undefined, "studio-controls");
    const filter = () => {
      modules.replaceChildren();
      const sections = new Map();
      for (const [type,{label,section,icon}] of Object.entries(registry)) {
        const query = search.value.trim().toLocaleLowerCase(I18n.language);
        if (!tr(label).toLocaleLowerCase(I18n.language).includes(query)) continue;
        if (!sections.has(section)) {
          const group = node("section",undefined,"matrix-catalog-group");
          group.append(node("h3",tr(section)));
          modules.append(group);
          sections.set(section,group);
        }
        const item = button(label,() => {
          const module = newModule(type);
          w.modules.push(module);
          drawCards(module.id);
          filter();
          changed();
        },w.modules.length >= 16);
        item.prepend(lineIcon(icon));
        sections.get(section).append(item);
      }
      if (!sections.size) modules.append(node("p",tr("Модули не найдены"),"matrix-empty"));
    };
    search.oninput = filter;
    filter();
    catalog.append(search,modules);
    const heading = node("div",undefined,"matrix-workspace-heading");
    const title = node("h1",w.name,"matrix-workspace-title");
    const open = button("Добавить модуль",() => openCatalog(catalog),w.modules.length >= 16);
    open.classList.add("matrix-add");
    toolbar.append(open);
    heading.append(title,toolbar);
    const tabline = node("div",undefined,"matrix-tabline");
    const plus = button("",() => create(false),layout.workspaces.length >= 10);
    plus.append(lineIcon("plus"));
    plus.classList.add("matrix-new-workspace");
    plus.setAttribute("aria-label", tr("Новое пространство"));
    const quick = node("div",undefined,"matrix-workspace-actions");
    const controlsForWorkspace = [
      ["settings","Настроить пространство",() => { manage.open = !manage.open; }, false],
      ["restore","Дублировать",() => create(true),layout.workspaces.length >= 10],
      ["close","Удалить пространство",remove,layout.workspaces.length < 2],
    ];
    for (const [icon,caption,action,disabled] of controlsForWorkspace) {
      const control = button("",action,disabled);
      control.append(lineIcon(icon));
      control.setAttribute("aria-label",tr(caption));
      control.title = tr(caption);
      quick.append(control);
    }
    tabline.append(tabs,plus,quick);
    controls.replaceChildren(tabline,heading,manage,catalog);
    paintTelemetry();
    grid.classList.toggle("matrix-editing",manage.open);
    drawCards();
    if (focusSelector) {
      const candidate = controls.querySelector(focusSelector);
      (candidate?.getClientRects().length ? candidate :
        controls.querySelector(".studio-picker select"))?.focus();
    }
  }

  function paintTelemetry() {
    const selected = workspace().ship_id || Session.data.expedition?.ship?.id;
    const ship = ownedShips().find(({id}) => id === selected);
    focusMetric.textContent = ship ? shipLabel(ship) : "—";
    contextShortcut.textContent = ship ? shipLabel(ship) : "—";
    contextShortcut.title = tr("Контекст корабля");
    contextShortcut.setAttribute("aria-label",contextShortcut.title + " · " + contextShortcut.textContent);
  }

  function refreshShips(force = false) {
    if (!layout) return;
    const signature = JSON.stringify(ownedShips().map(ship => [ship.id,shipLabel(ship)]));
    const update = force || signature !== shipSignature;
    shipSignature = signature;
    paintTelemetry();
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
      const savedDraft = draft.read();
      const ownCommit = savedDraft?.pendingWrite &&
        savedDraft.pendingWrite.revision + 1 === result.revision &&
        sessionLayoutEqual(savedDraft.pendingWrite.layout,layout);
      const recover = savedDraft?.layout?.version === layout.version &&
        (savedDraft.revision === result.revision || ownCommit);
      if (recover) layout = savedDraft.layout;
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
      if (recover && !sessionLayoutEqual(layout,result.layout)) changed();
      else if (savedDraft && !recover) {
        conflict = true;
        report("Рабочие пространства изменились в другой вкладке");
      } else {
        draft.clear();
        report();
      }
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
    context.clear();
    const overview = document.querySelector("#game .matrix-overview");
    overview.prepend(pilot);
    if (telemetry) overview.append(telemetry);
    contextShortcut.hidden = true;
    for (const entry of navItems.values()) entry.hidden = true;
    mapRequest = dragID = null;
    writing = loading = dirty = failed = conflict = false;
    host.hidden = shortcut.hidden = true;
    host.replaceChildren();
  });
}
