import { api } from "../../core/api.js";
import { I18n } from "../../core/i18n.js";
import { Preferences } from "../../core/preferences.js";
import { RequestScope, Session } from "../../core/session.js";
import { node } from "../../ui/dom.js";
import { Modules, UI } from "../../ui/runtime.js";
import { labels, readings, shipLabel } from "./views.js";

const tr = (text) => I18n.t(text);
const instanceID = (module) => `studio-${module.id}`;
const activeWorkspace = (layout) => layout.workspaces.find(({ id }) => id === layout.active);
const ownedShips = () => Session.data.fleet?.ships || [];

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
  const options = [[0, tr("Выберите корабль для просмотра")]];
  for (const ship of ownedShips()) options.push([ship.id, shipLabel(ship)]);
  if (current && !ownedShips().some(({ id }) => id === current))
    options.push([current, `${tr("Корабль недоступен")} · #${current}`]);
  return options;
}

function legacyGeometry(type) {
  const key = { fleet: "ship", cargo: "cargo", mining: "mining" }[type];
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
  shortcut.onclick = () => {
    host.tabIndex = -1;
    (controls?.querySelector('.studio-tabs [aria-current="true"]')?.getClientRects().length
      ? controls.querySelector('.studio-tabs [aria-current="true"]')
      : controls?.querySelector(".studio-picker select") || host).focus();
    host.scrollIntoView({block:"nearest"});
  };
  const requests = new RequestScope();
  const mounted = new Map();
  let layout, revision, timer, request, writing, loading, dirty, failed, conflict, pendingWrite;
  let controls, status, actions, shipSignature;
  const workspace = () => activeWorkspace(layout);

  function report(text = "") {
    status.textContent = tr(text);
    actions.replaceChildren();
    if (failed && !conflict)
      actions.append(button("Повторить", () => layout ? flush() : load()));
    if (conflict)
      actions.append(button("Загрузить сохранённую раскладку", () => load()));
  }

  function frame() {
    const title = node("h2", tr("Рабочие пространства"));
    controls = node("div");
    status = node("p");
    status.setAttribute("role","status");
    actions = node("div", undefined, "studio-controls");
    host.replaceChildren(title, controls, status, actions);
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

  function paintWindow(module, entry, rebuildControls = false) {
    const shipID = module.binding === "fixed" ? module.ship_id :
      module.binding === "workspace" ? workspace().ship_id : 0;
    const ship = ownedShips().find(({id}) => id === shipID);
    const output = readings(module, ship, shipID);
    const name = entry.content.firstElementChild;
    name.textContent = ship ? shipLabel(ship) : tr(labels[module.type]);
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
    entry.data.replaceChildren(output);
  }

  function mount(module) {
    const key = instanceID(module);
    const offset = (mounted.size % 6) * 26;
    const root = Modules.create(key, labels[module.type], 430, 330, {
      minContentWidth: 175,
      layout: {
        read: () => module.geometry || {x: 44 + offset, y: 114 + offset},
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
    view.append(node("h3"),tools,data);
    root.append(view);
    const entry = {content:view,tools,data};
    mounted.set(key,entry);
    Modules.header(key).closest("dialog").classList.add("studio-window");
    paintWindow(module,entry,true);
    Modules.open(key);
  }

  function showWindows() {
    for (const key of mounted.keys()) Modules.destroy(key);
    mounted.clear();
    if (!document.body.classList.contains("planet-mode"))
      for (const module of workspace().modules) mount(module);
  }

  function switchTo(value, selector = ".studio-picker select") {
    layout.active = value;
    render(true, selector);
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
      tab.setAttribute("aria-current", String(item.id === w.id));
      tabs.append(tab);
    }
    toolbar.append(picker, select("Корабль пространства", shipOptions(w.ship_id), w.ship_id, (value) => {
      w.ship_id = Number(value);
      for (const module of w.modules) {
        const entry = mounted.get(instanceID(module));
        if (entry && module.binding === "workspace") paintWindow(module,entry,true);
      }
      changed();
    }));

    const manage = node("details");
    manage.open = controls.querySelector("details")?.open || false;
    manage.append(node("summary",tr("Настроить пространство")));
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
        {name: tr("Новое пространство"), ship_id:0, modules:[]};
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

    const catalog = node("details");
    catalog.open = controls.querySelectorAll("details")[1]?.open || false;
    catalog.append(node("summary",tr("Добавить модуль")));
    const search = node("input");
    search.type = "search";
    search.setAttribute("aria-label",tr("Поиск модулей"));
    const modules = node("div", undefined, "studio-controls");
    const filter = () => {
      modules.replaceChildren();
      for (const [type,label] of Object.entries(labels)) {
        if (!tr(label).toLocaleLowerCase().includes(search.value.trim().toLocaleLowerCase())) continue;
        modules.append(button(label,() => {
          const module = {id:id(),type,binding:"workspace",ship_id:0,geometry:legacyGeometry(type)};
          w.modules.push(module);
          mount(module);
          render();
          changed();
        },w.modules.length >= 16));
      }
    };
    search.oninput = filter;
    filter();
    catalog.append(search,modules);
    controls.replaceChildren(tabs,toolbar,manage,catalog);
    if (focusSelector) {
      const candidate = controls.querySelector(focusSelector);
      (candidate?.getClientRects().length ? candidate :
        controls.querySelector(".studio-picker select"))?.focus();
    }
  }

  function refreshShips(force = false) {
    if (!layout) return;
    const signature = JSON.stringify(ownedShips().map(ship => [ship.id,shipLabel(ship)]));
    const update = force || signature !== shipSignature;
    shipSignature = signature;
    if (update) {
      // Keep the editor draft and current keyboard focus during background snapshots.
      const old = controls.querySelectorAll("select")[1];
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
    w.modules = w.modules.filter((module) => instanceID(module) !== detail);
    Modules.destroy(detail);
    render();
    changed();
  });
  window.addEventListener("planetmodechange", ({detail}) => {
    if (!detail && layout && !mounted.size) showWindows();
  });
  window.addEventListener("languagechange", () => {
    if (!layout) return;
    host.querySelector("h2").textContent = tr("Рабочие пространства");
    render();
    refreshShips(true);
  });
  window.addEventListener("signedout", () => {
    requests.invalidate();
    clearTimeout(timer);
    layout = pendingWrite = null;
    for (const key of mounted.keys()) Modules.destroy(key);
    mounted.clear();
    writing = loading = dirty = failed = conflict = false;
    host.hidden = shortcut.hidden = true;
    host.replaceChildren();
  });
}
