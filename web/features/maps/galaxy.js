import { sceneLoop } from "../../core/scene-loop.js";
import { openPlanet } from "../planet/loader.js";
import { adminPlanetSurface } from "../planet/loader.js";
import { Preferences } from "../../core/preferences.js";
import { objectHologram } from "../objects/presentation.js";
import { shipName } from "../fleet/presentation.js";
import { systems } from "../navigation/pickers.js";
import {
  personalizeStations,
  stationLabel,
  stationEditor,
} from "../stations/index.js";
import { showObject } from "../objects/cards.js";
import { Format } from "../../core/format.js";
import { loadWorld } from "../../core/world-store.js";
import { Session } from "../../core/session.js";
import { I18n } from "../../core/i18n.js";
import { api } from "../../core/api.js";
import { notify } from "../../ui/feedback.js";
import { UI, Modules } from "../../ui/runtime.js";
import { Game, Visuals, scenes } from "../runtime.js";
import { performExpedition } from "../commands.js";
import { world } from "../../core/world-store.js";

export function initGalaxy() {
  const root = document.querySelector("#window-galaxy .module-content"),
    tr = (s) => I18n.t(s),
    e = UI.node;
  root.classList.add("galaxy-content");
  const top = e("div", undefined, "map-toolbar"),
    back = UI.button("К галактике", () => {});
  back.hidden = true;
  let galaxyPicker = UI.button("Edem", () => {});
  galaxyPicker.hidden = true;
  let system = UI.button("Поиск системы", () => {});
  system.classList.add("system-picker");
  system.id = "map-system-id";
  system.value = 1;
  const inspect = e("button"),
    reset = UI.button("⌖", () => {}),
    minus = UI.button("−", () => {}),
    plus = UI.button("+", () => {}),
    label = e("span", undefined, "map-breadcrumb"),
    toggle = UI.button("☷", () => {
      body.classList.toggle("inspector-hidden");
      draw();
    });
  toggle.dataset.help = "Показать или скрыть панель";
  label.id = "map-breadcrumb";
  top.append(back, galaxyPicker, system, label, toggle);
  const filters = e("div", undefined, "map-filters"),
    filterDefaults = {
      ships: true,
      others: false,
      ports: true,
      planets: true,
      belts: true,
      labels: true,
      territories: true,
      lanes: true,
    };
  let flags = { ...filterDefaults },
    filterOwner,
    filterModes;
  function syncFilters() {
    const owner = Session.data.state?.user?.id;
    if (!owner) return;
    if (filterOwner !== owner) {
      filterOwner = owner;
      filterModes = {};
      for (const type of ["galaxy", "system"]) {
        const defaults = { ...filterDefaults, others: type === "system" };
        let saved;
        try {
          saved = Preferences.read(`map-filters:${owner}:${type}`, {});
        } catch {}
        for (const key of Object.keys(defaults))
          if (typeof saved?.[key] === "boolean") defaults[key] = saved[key];
        filterModes[type] = defaults;
      }
    }
    flags = filterModes[mode];
    filters.querySelectorAll("label").forEach((l) => {
      l.querySelector("input").checked = flags[l.dataset.filter];
      l.hidden =
        mode === "galaxy"
          ? !["ships", "others", "labels", "territories", "lanes"].includes(
              l.dataset.filter,
            )
          : ["territories", "lanes"].includes(l.dataset.filter);
    });
  }
  for (const [id, text] of [
    ["ships", "Мои корабли"],
    ["others", "Корабли других игроков"],
    ["ports", "Порты"],
    ["planets", "Планеты"],
    ["belts", "Месторождения"],
    ["labels", "Подписи"],
    ["territories", "Владения корпораций"],
    ["lanes", "Гиперкоридоры"],
  ]) {
    const l = e("label"),
      i = e("input"),
      span = e("span", tr(text));
    l.dataset.filter = id;
    span.dataset.i18n = text;
    if (id === "labels")
      l.dataset.help =
        "При приближении названия систем видны только рядом с курсором.";
    i.type = "checkbox";
    i.checked = true;
    i.onchange = () => {
      flags[id] = i.checked;
      try {
        Preferences.write(`map-filters:${filterOwner}:${mode}`, flags);
      } catch {}
      draw();
    };
    l.append(i, span);
    filters.append(l);
  }
  const display = UI.fold("map:display", false, "map-display-menu"),
    displayTitle = e("summary", tr("Отображение"));
  displayTitle.dataset.i18n = "Отображение";
  display.append(displayTitle, filters);
  top.append(display);
  display.addEventListener("toggle", () => {
    if (display.open) syncFilters();
  });
  const body = e("div", undefined, "map-workspace"),
    view = e("div", undefined, "map-canvas-wrap"),
    canvas = e("canvas"),
    side = e("aside", undefined, "map-inspector"),
    tools = e("div", undefined, "map-tools"),
    zoomLabel = e("span", "1×", "zoom-label"),
    legend = e("details", undefined, "map-legend");
  canvas.id = "galaxy-map";
  canvas.setAttribute("aria-label", tr("Галактическая карта Эдема"));
  window.addEventListener("languagechange", () =>
    canvas.setAttribute("aria-label", tr("Галактическая карта Эдема")),
  );
  tools.append(minus, zoomLabel, plus, reset);
  reset.dataset.help = "Вместить карту";
  function mapLegend() {
    legend.replaceChildren(e("summary", tr("Легенда")));
    for (const [symbol, label, cls] of [
      ["━", "Маршрут", "route"],
      ["◆", "Мои корабли", "fleet"],
      ["●", "Класс звезды", "star"],
      ["◯", "Владения корпораций", "territory"],
      ["┄", "Гиперкоридоры", "lanes"],
    ]) {
      const row = e("div", undefined, "legend-row");
      row.append(
        e("span", symbol, "legend-symbol " + cls),
        e("span", tr(label)),
      );
      legend.append(row);
    }
  }
  mapLegend();
  window.addEventListener("languagechange", mapLegend);
  view.append(canvas, tools, legend);
  body.append(view, side);
  root.append(top, body);
  const ctx = canvas.getContext("2d");
  const summary = e("div", undefined, "map-system-summary");
  summary.id = "map-system-summary";
  summary.hidden = true;
  summary.setAttribute("role", "tooltip");
  document.body.append(summary);
  let territorySource,
    territories = [],
    hoverID = null,
    hoverTimer,
    hoverGeneration = 0,
    labelFocus = null,
    labelFrame = null,
    projectedSource = null,
    projectedKey = "",
    projectedX = new Float64Array(),
    projectedY = new Float64Array(),
    visibleStars = [],
    regionCenters = [];
  const labelRadius = 160,
    labelLimit = 48;
  function requestLabelDraw() {
    if (labelFrame !== null) return;
    labelFrame = requestAnimationFrame(() => {
      labelFrame = null;
      draw();
    });
  }
  function trackLabelFocus(event) {
    const bounds = canvas.getBoundingClientRect(),
      x = event.clientX - bounds.left,
      y = event.clientY - bounds.top;
    labelFocus =
      x >= 0 && y >= 0 && x <= bounds.width && y <= bounds.height
        ? { x, y }
        : null;
    if (mode === "galaxy" && flags.labels && cam().zoom > 3) requestLabelDraw();
  }
  canvas.onpointerenter = trackLabelFocus;
  const summaryCache = new Map(),
    labelWidths = new Map();
  let knownStars = new Set();
  function hideSummary() {
    hoverID = null;
    clearTimeout(hoverTimer);
    hoverGeneration++;
    summary.hidden = true;
    summary.replaceChildren();
    summary.replaceChildren();
  }
  canvas.onpointerleave = () => {
    hideSummary();
    labelFocus = null;
    requestLabelDraw();
  };
  function buildTerritories() {
    if (territorySource === galaxy) return;
    territorySource = galaxy;
    labelWidths.clear();
    territories = [];
    const regions = new Map(),
      names = new Map(
        (galaxy.regions || []).map((region) => [region.id, region.name]),
      );
    for (const star of galaxy.stars) {
      if (star.deleted) continue;
      const key = star.galaxy + ":" + star.region,
        entry = regions.get(key) || {
          galaxy: star.galaxy,
          name: names.get(star.region),
          x: 0,
          y: 0,
          count: 0,
        };
      entry.x += star.x;
      entry.y += star.y;
      entry.count++;
      regions.set(key, entry);
    }
    regionCenters = [...regions.values()]
      .filter((region) => region.name)
      .map((region) => ({
        ...region,
        x: region.x / region.count,
        y: region.y / region.count,
      }));
    for (const definition of galaxy.galaxies || [])
      for (const area of definition.territories || []) {
        const center = galaxy.stars[area.anchor - 1],
          owner = galaxy.founders?.find((c) => c.founder_key === area.founder);
        if (center && owner)
          territories.push({
            galaxy: definition.id,
            center,
            radius: area.radius,
            color: owner.color,
          });
      }
  }
  function drawTerritories() {
    if (!flags.territories) return;
    ctx.save();
    for (const area of territories) {
      if (area.galaxy !== viewedGalaxy) continue;
      const p = galaxyPoint(area.center),
        edge = galaxyPoint({
          x: area.center.x + area.radius,
          y: area.center.y,
        }),
        radius = Math.abs(edge.x - p.x);
      ctx.beginPath();
      ctx.arc(p.x, p.y, radius, 0, Math.PI * 2);
      ctx.fillStyle = area.color;
      ctx.globalAlpha = 0.065;
      ctx.fill();
      ctx.strokeStyle = area.color;
      ctx.globalAlpha = 0.22;
      ctx.lineWidth = 1;
      ctx.stroke();
    }
    ctx.restore();
  }
  let presenceSource,
    presence = new Map();
  function systemPlayers(star) {
    if (presenceSource !== Session.data.fleet) {
      presenceSource = Session.data.fleet;
      presence = new Map();
      const add = (star, id, name) => {
        if (!presence.has(star)) presence.set(star, new Map());
        const owners = presence.get(star);
        if (!owners.has(id) && owners.size < 11) owners.set(id, name);
      };
      for (const ship of Session.data.fleet?.ships || [])
        add(
          ship.star,
          Session.data.state.user.id,
          Session.data.state.user.username,
        );
      for (const ship of Session.data.fleet?.public_ships || [])
        add(ship.star, ship.owner, ship.owner_name);
    }
    const names = [...(presence.get(star)?.values() || [])];
    return names.length
      ? names.slice(0, 10).join(", ") + (names.length > 10 ? "…" : "")
      : tr("Нет игроков");
  }
  function hoverSummary(event) {
    const bounds = canvas.getBoundingClientRect(),
      x = event.clientX - bounds.x,
      y = event.clientY - bounds.y;
    let closest = null,
      distance = 9;
    for (const { star, x: sx, y: sy } of visibleStars) {
      const d = Math.hypot(x - sx, y - sy);
      if (d < distance) {
        distance = d;
        closest = star;
      }
    }
    if (!closest) {
      hideSummary();
      return;
    }
    summary.style.left =
      Math.max(8, Math.min(event.clientX + 16, innerWidth - 310)) + "px";
    summary.style.top =
      Math.max(8, Math.min(event.clientY + 16, innerHeight - 240)) + "px";
    if (hoverID === closest.id) return;
    hideSummary();
    hoverID = closest.id;
    const generation = hoverGeneration,
      epoch = Session.epoch,
      star = closest;
    hoverTimer = setTimeout(async () => {
      if (hoverID !== star.id) return;
      const owner = galaxy.founders?.find(
          (c) => c.founder_key === star.founder,
        ),
        region = galaxy.regions?.find((r) => r.id === star.region);
      summary.replaceChildren(
        e("b", star.name),
        e(
          "small",
          tr(definition()?.name || "") + " · " + tr(region?.name || ""),
        ),
        e(
          "p",
          `${tr("Класс звезды")}: ${star.class} · ${star.mass} M☉ · ${star.luminosity} L☉`,
        ),
      );
      const badge = e(
        "span",
        owner
          ? `${owner.flag} [${owner.tag}] ${owner.name}`
          : tr("Нейтральная система"),
        "map-owner",
      );
      if (owner) badge.style.color = owner.color;
      summary.append(
        badge,
        e(
          "p",
          tr("Присутствуют игроки") + ": " + systemPlayers(star.id),
          "system-presence",
        ),
      );
      const known = Game.known(star.id),
        progress =
          Session.data.workbench?.research?.find((r) => r.star === star.id)
            ?.scanned || 0;
      summary.append(
        e(
          "p",
          tr(
            known
              ? "Система исследована"
              : progress
                ? "Сканирование раскрывает объекты по очереди."
                : "Система не исследована. Исследуйте её, чтобы открыть объекты.",
          ),
          "muted",
        ),
      );
      summary.hidden = false;
      if (!known && !progress) return;
      const cacheKey = `${star.id}/${known}/${progress}/${galaxy.revision}`;
      try {
        let data = summaryCache.get(cacheKey);
        if (!data) {
          data = await api("system?id=" + star.id);
          summaryCache.set(cacheKey, data);
        }
        if (
          generation !== hoverGeneration ||
          epoch !== Session.epoch ||
          hoverID !== star.id ||
          !Session.signedIn
        )
          return;
        const text = [
          `${tr("Планеты")}: ${data.sites.filter((s) => s.details).length}`,
          `${tr("Порты")}: ${data.sites.filter((s) => s.kind === "Орбитальная станция").length}`,
          `${tr("Месторождения")}: ${data.sites.filter((s) => s.planet >= 101).length}`,
        ];
        summary.append(e("p", text.join(" · ")));
      } catch (error) {
        if (generation === hoverGeneration)
          summary.append(e("p", tr("Сводка временно недоступна"), "muted"));
      }
    }, 180);
  }
  let galaxy,
    local,
    viewedGalaxy = "edem",
    mode = "galaxy",
    camera = {
      galaxy: { zoom: 1, x: 0, y: 0 },
      system: { zoom: 1, x: 0, y: 0 },
    },
    pointer,
    route = [],
    activeRoute = [],
    routeKey = "",
    selected = 1,
    selectedSite = null,
    sites = [],
    shipHits = [],
    request = 0,
    loading = false,
    pendingFind = null,
    currentSystemRequested = false,
    pendingStar = null,
    hoveredSite = null,
    panelTab = "route",
    routeStops = [];
  function syncMapNavigation() {
    const open = Modules.isOpen("galaxy");
    document
      .querySelectorAll('#sidebar [data-open="galaxy"]')
      .forEach((button) =>
        button.classList.toggle("module-open", open && mode === "galaxy"),
      );
    document
      .getElementById("current-system-map")
      .classList.toggle("module-open", open && mode === "system");
  }
  function setMode(value) {
    mode = value;
    root.dataset.mapMode = value;
    syncMapNavigation();
  }
  const starGalaxy = (id) => galaxy?.stars[id - 1]?.galaxy || "edem";
  const definition = () => galaxy?.galaxies?.find((g) => g.id === viewedGalaxy);
  const galaxyTitle = () =>
    tr(definition()?.name || "Edem") +
    " · " +
    (galaxy?.stars.filter((s) => s.galaxy === viewedGalaxy).length || 0);
  function updateGalaxyPicker() {
    if (!galaxy) return;
    const entries = (galaxy.galaxies || []).filter((g) => g.opened);
    if (!entries.some((g) => g.id === viewedGalaxy))
      viewedGalaxy = entries[0]?.id || "edem";
    const picker = UI.picker(
      entries.map((g) => [g.id, tr(g.name)]),
      viewedGalaxy,
      (value) => {
        request++;
        viewedGalaxy = value;
        selected =
          fleet().find((ship) => starGalaxy(ship.star) === viewedGalaxy)
            ?.star ||
          galaxy.stars.find((s) => s.galaxy === viewedGalaxy)?.id ||
          1;
        setMode("galaxy");
        local = null;
        back.hidden = true;
        route = [];
        camera.galaxy = { zoom: 1, x: 0, y: 0 };
        updateSystemPicker();
        updateGalaxyPicker();
        routePanel();
        draw();
      },
      { raw: true },
    );
    picker.id = "map-galaxy";
    picker.classList.add("galaxy-picker");
    picker.hidden = entries.length < 2;
    galaxyPicker.replaceWith(picker);
    galaxyPicker = picker;
    if (mode === "galaxy") label.textContent = galaxyTitle();
    buildTerritories();
  }
  function updateSystemPicker() {
    if (!galaxy) return;
    if (galaxy.stars[selected - 1]?.deleted)
      selected = Session.data.expedition.ship.star;
    const picker = systems(selected, (id) => {
      request++;
      selected = Number(id);
      viewedGalaxy = starGalaxy(selected);
      updateGalaxyPicker();
      const star = galaxy.stars[selected - 1];
      setMode("galaxy");
      back.hidden = true;
      camera.galaxy = { zoom: 3, x: 0, y: 0 };
      const p = galaxyPoint(star);
      camera.galaxy.x = canvas.clientWidth / 2 - p.x;
      camera.galaxy.y = canvas.clientHeight / 2 - p.y;
      routePanel();
      draw();
      window.dispatchEvent(
        new CustomEvent("galaxyselection", { detail: selected }),
      );
    }, viewedGalaxy);
    picker.id = "map-system-id";
    picker.classList.add("system-picker");
    system.replaceWith(picker);
    system = picker;
  }
  const now = () => Date.now() + (Session.data.serverOffset || 0),
    cam = () => camera[mode],
    fleet = () => Session.data.fleet?.ships || [];
  const shipOwner = (s) => s.owner ?? Session.data.state.user.id;
  const shipColor = (s) => `hsl(${(shipOwner(s) * 137.508) % 360} 72% 72%)`;
  const mapShips = () => [
    ...(flags.ships ? fleet() : []),
    ...(flags.others ? Session.data.fleet?.public_ships || [] : []),
  ];
  function action(text, fn) {
    const b = e("button", tr(text));
    b.onclick = fn;
    return b;
  }
  function hint(text, message) {
    const n = e("span", text, "help");
    n.tabIndex = 0;
    n.dataset.help = message;
    return n;
  }
  function galaxyPoint(s) {
    const c = camera.galaxy,
      scale =
        (Math.min(canvas.clientWidth, canvas.clientHeight) /
          ((definition()?.radius || 1250) * 2)) *
        c.zoom;
    return {
      x: canvas.clientWidth / 2 + c.x + s.x * scale,
      y: canvas.clientHeight / 2 + c.y + s.y * scale,
    };
  }
  function localPoint(p) {
    const c = camera.system,
      w = canvas.clientWidth,
      h = canvas.clientHeight;
    return {
      x: w / 2 + c.x + (p.x - w / 2) * c.zoom,
      y: h / 2 + c.y + (p.y - h / 2) * c.zoom,
    };
  }
  function objectBase(id) {
    const s = sites.find((s) => s.id === id);
    if (s) return { x: s.baseX, y: s.baseY };
    return { x: canvas.clientWidth * 0.12, y: canvas.clientHeight * 0.82 };
  }
  function localShip(ship) {
    if (ship.destination) {
      return localPoint(objectBase("entry"));
    }
    const base = objectBase(ship.object_id);
    let to = base,
      angle = 0;
    if (ship.local_target) {
      const target = objectBase(ship.local_target),
        start = new Date(ship.local_started).getTime(),
        end = new Date(ship.local_arrival).getTime(),
        f = Math.max(
          0,
          Math.min(1, (now() - start) / Math.max(1, end - start)),
        );
      to = {
        x: base.x + (target.x - base.x) * f,
        y: base.y + (target.y - base.y) * f,
      };
      angle = Math.atan2(target.y - base.y, target.x - base.x);
    } else if (ship.position_mode === "orbit") {
      const phase = now() / 16000;
      to = {
        x: base.x + Math.cos(phase) * 20,
        y: base.y + Math.sin(phase) * 14,
      };
      angle = phase + Math.PI / 2;
    } else to = { x: base.x + 16, y: base.y + 11 };
    return { ...localPoint(to), angle };
  }
  function galaxyShip(ship) {
    const path = ship.path?.length ? ship.path : [ship.star];
    if (!ship.destination || path.length < 2)
      return galaxyPoint(galaxy.stars[ship.star - 1]);
    const start = new Date(ship.departure).getTime(),
      end = new Date(ship.arrival).getTime(),
      phase = Math.max(
        0,
        Math.min(
          path.length - 1,
          ((now() - start) / Math.max(1, end - start)) * (path.length - 1),
        ),
      ),
      i = Math.min(path.length - 2, Math.floor(phase)),
      t = phase - i;
    const a = galaxyPoint(galaxy.stars[path[i] - 1]),
      b = galaxyPoint(galaxy.stars[path[i + 1] - 1]);
    return {
      x: a.x + (b.x - a.x) * t,
      y: a.y + (b.y - a.y) * t,
      angle: Math.atan2(b.y - a.y, b.x - a.x),
    };
  }
  function projectGalaxy(w, h) {
    const c = camera.galaxy,
      radius = definition()?.radius || 1250,
      key = [
        viewedGalaxy,
        galaxy.revision,
        galaxy.stars.length,
        w,
        h,
        c.zoom,
        c.x,
        c.y,
        radius,
      ].join("/");
    if (projectedSource === galaxy && projectedKey === key) return;
    projectedSource = galaxy;
    projectedKey = key;
    if (projectedX.length <= galaxy.stars.length) {
      projectedX = new Float64Array(galaxy.stars.length + 1);
      projectedY = new Float64Array(galaxy.stars.length + 1);
    }
    visibleStars = [];
    const scale = (Math.min(w, h) / (radius * 2)) * c.zoom,
      cx = w / 2 + c.x,
      cy = h / 2 + c.y;
    for (const star of galaxy.stars) {
      if (star.deleted || star.galaxy !== viewedGalaxy) continue;
      const x = cx + star.x * scale,
        y = cy + star.y * scale;
      projectedX[star.id] = x;
      projectedY[star.id] = y;
      if (x >= -10 && y >= -10 && x <= w + 10 && y <= h + 10)
        visibleStars.push({ star, x, y });
    }
  }
  function outsideView(ax, ay, bx, by, w, h) {
    return (
      (ax < 0 && bx < 0) ||
      (ay < 0 && by < 0) ||
      (ax > w && bx > w) ||
      (ay > h && by > h)
    );
  }
  function drawNearbyLabels(w, h) {
    if (!flags.labels || camera.galaxy.zoom <= 3 || !labelFocus) return;
    const { x, y } = labelFocus,
      candidates = visibleStars
        .map((point) => ({
          ...point,
          distance: (point.x - x) ** 2 + (point.y - y) ** 2,
        }))
        .filter((point) => point.distance <= labelRadius ** 2)
        .sort((a, b) => a.distance - b.distance)
        .slice(0, 256),
      placed = [];
    ctx.font = "10px monospace";
    for (const point of candidates) {
      if (placed.length >= labelLimit) break;
      let width = labelWidths.get(point.star.name);
      if (width === undefined) {
        width = ctx.measureText(point.star.name).width;
        labelWidths.set(point.star.name, width);
      }
      const left = point.x + 5,
        top = point.y - 15,
        right = left + width,
        bottom = point.y - 3;
      if (
        right < 0 ||
        left > w ||
        bottom < 0 ||
        top > h ||
        placed.some(
          (box) =>
            left < box.right + 3 &&
            right + 3 > box.left &&
            top < box.bottom + 2 &&
            bottom + 2 > box.top,
        )
      )
        continue;
      ctx.fillStyle = knownStars.has(point.star.id)
        ? {
            O: "#96baff",
            B: "#bdd5ff",
            A: "#edf1ff",
            F: "#fff0c4",
            G: "#ffdb78",
            K: "#f5ad65",
            M: "#ee8277",
          }[point.star.class] || "#fff"
        : "#91a5b8";
      ctx.fillText(point.star.name, left, point.y - 5);
      placed.push({ left, top, right, bottom });
    }
  }
  function draw() {
    if (document.hidden || !Modules.isOpen("galaxy") || !Session.signedIn)
      return;
    syncFilters();
    if (root.classList.contains("planet-open")) return;
    if (mode !== "galaxy") hideSummary();
    zoomLabel.textContent = cam().zoom.toFixed(1) + "×";
    if (
      !galaxy ||
      !Modules.isOpen("galaxy") ||
      fleet().some((ship) => ship.star > galaxy.stars.length)
    )
      return;
    const ratio = devicePixelRatio || 1,
      w = canvas.clientWidth,
      h = canvas.clientHeight;
    if (!w || !h) return;
    if (
      canvas.width !== Math.round(w * ratio) ||
      canvas.height !== Math.round(h * ratio)
    ) {
      canvas.width = w * ratio;
      canvas.height = h * ratio;
    }
    ctx.lineWidth = 1;
    ctx.setLineDash([]);
    ctx.shadowBlur = 0;
    ctx.setTransform(ratio, 0, 0, ratio, 0, 0);
    ctx.clearRect(0, 0, w, h);
    // Presentation only: this grid never participates in projection or hits.
    const gridStep = 80,
      gridX = ((cam().x % gridStep) + gridStep) % gridStep,
      gridY = ((cam().y % gridStep) + gridStep) % gridStep;
    ctx.strokeStyle = "#8db9cf0b";
    ctx.lineWidth = 1;
    ctx.beginPath();
    for (let x = gridX; x < w; x += gridStep) {
      ctx.moveTo(x, 0);
      ctx.lineTo(x, h);
    }
    for (let y = gridY; y < h; y += gridStep) {
      ctx.moveTo(0, y);
      ctx.lineTo(w, y);
    }
    ctx.stroke();
    shipHits = [];
    if (mode === "system" && local) {
      sites = [];
      const cx = w / 2,
        cy = h / 2,
        max = Math.max(1, ...local.sites.map((s) => Math.log(1 + s.orbit)));
      const center = localPoint({ x: cx, y: cy });
      ctx.fillStyle =
        {
          O: "#96baff",
          B: "#bdd5ff",
          A: "#edf1ff",
          F: "#fff0c4",
          G: "#ffdb78",
          K: "#f5ad65",
          M: "#ee8277",
        }[local.star.class] || "#efad58";
      ctx.shadowColor = ctx.fillStyle;
      ctx.shadowBlur = 18;
      ctx.beginPath();
      ctx.arc(center.x, center.y, 12 * Math.sqrt(cam().zoom), 0, Math.PI * 2);
      ctx.fill();
      ctx.shadowBlur = 0;
      ctx.font = "11px monospace";
      ctx.fillText(local.star.name, center.x + 18, center.y);
      for (let i = 0; i < local.sites.length; i++) {
        const s = local.sites[i],
          r =
            35 +
            (Math.log(1 + s.orbit) / max) *
              Math.max(20, Math.min(w, h) / 2 - 75),
          angle = i * 2.4 + local.star.id * 0.2,
          baseX = cx + Math.cos(angle) * r,
          baseY = cy + Math.sin(angle) * r * 0.72,
          p = localPoint({ x: baseX, y: baseY });
        const visible =
          s.kind === "Орбитальная станция"
            ? flags.ports
            : s.planet >= 101
              ? flags.belts
              : flags.planets;
        const radius =
          (s.kind === "Газовый гигант"
            ? 13
            : s.planet >= 101
              ? 13
              : s.kind === "Орбитальная станция"
                ? 10
                : 8) * Math.min(2.5, Math.sqrt(cam().zoom));
        sites.push({ ...s, x: p.x, y: p.y, baseX, baseY, radius, visible });
        if (!visible) continue;
        ctx.strokeStyle = "#54728b55";
        ctx.setLineDash(s.planet >= 101 ? [2, 5] : []);
        ctx.beginPath();
        ctx.ellipse(
          center.x,
          center.y,
          r * cam().zoom,
          r * 0.72 * cam().zoom,
          0,
          0,
          Math.PI * 2,
        );
        ctx.stroke();
        ctx.setLineDash([]);
        Visuals.object(ctx, p.x, p.y, radius, s);
        if (s.id === selectedSite || s.id === hoveredSite) {
          ctx.strokeStyle = "#efad58";
          ctx.lineWidth = 1.5;
          ctx.beginPath();
          ctx.arc(p.x, p.y, radius + 7, 0, Math.PI * 2);
          ctx.stroke();
          ctx.fillStyle = "#efad5812";
          ctx.fill();
        }
        if (flags.labels) {
          ctx.fillStyle = "#c7d4df";
          ctx.font = "10px monospace";
          ctx.fillText(s.name, p.x + radius + 5, p.y - 5);
        }
      }
      const entry = localPoint(objectBase("entry"));
      ctx.strokeStyle = "#55839b";
      ctx.beginPath();
      ctx.arc(entry.x, entry.y, 9, 0, Math.PI * 2);
      ctx.stroke();
      if (flags.labels) {
        ctx.fillStyle = "#79a4bc";
        ctx.fillText(tr("Точка гиперперехода"), entry.x + 14, entry.y);
      }
      if (flags.ships || flags.others)
        for (const ship of mapShips()) {
          const inSystem =
            ship.star === local.star.id ||
            (ship.destination === local.star.id &&
              new Date(ship.arrival).getTime() <= now());
          if (!inSystem) continue;
          const p = localShip(ship);
          if (ship.local_target) {
            const a = localPoint(objectBase(ship.object_id)),
              b = localPoint(objectBase(ship.local_target));
            ctx.strokeStyle = "#7fdce388";
            ctx.setLineDash([3, 5]);
            ctx.beginPath();
            ctx.moveTo(a.x, a.y);
            ctx.lineTo(b.x, b.y);
            ctx.stroke();
            ctx.setLineDash([]);
          }
          Visuals.ship(ctx, p.x, p.y, 10, p.angle || 0, shipColor(ship));
          ctx.fillStyle = shipColor(ship);
          ctx.fillText(
            tr(ship.name) +
              (ship.destination ? " · " + tr("Гиперпереход") : ""),
            p.x + 12,
            p.y - 13,
          );
          shipHits.push({ ship, x: p.x, y: p.y });
        }
      return;
    }
    projectGalaxy(w, h);
    drawTerritories();
    ctx.strokeStyle = "#4d728755";
    ctx.lineWidth = 0.6;
    ctx.beginPath();
    for (const lane of flags.lanes ? galaxy.lanes : []) {
      if (
        starGalaxy(lane.from) !== viewedGalaxy ||
        starGalaxy(lane.to) !== viewedGalaxy
      )
        continue;
      const ax = projectedX[lane.from],
        ay = projectedY[lane.from],
        bx = projectedX[lane.to],
        by = projectedY[lane.to];
      if (outsideView(ax, ay, bx, by, w, h)) continue;
      ctx.moveTo(ax, ay);
      ctx.lineTo(bx, by);
    }
    ctx.stroke();
    ctx.strokeStyle = "#efad58";
    ctx.lineWidth = 2;
    ctx.beginPath();
    updateRoute();
    for (const path of [activeRoute, route])
      for (let i = 1; i < path.length; i++) {
        if (
          starGalaxy(path[i - 1]) !== viewedGalaxy ||
          starGalaxy(path[i]) !== viewedGalaxy
        )
          continue;
        const a = galaxyPoint(galaxy.stars[path[i - 1] - 1]),
          b = galaxyPoint(galaxy.stars[path[i] - 1]);
        ctx.moveTo(a.x, a.y);
        ctx.lineTo(b.x, b.y);
      }
    ctx.stroke();
    const colors = {
      O: "#96baff",
      B: "#bdd5ff",
      A: "#edf1ff",
      F: "#fff0c4",
      G: "#ffdb78",
      K: "#f5ad65",
      M: "#ee8277",
    };
    const groups = new Map(),
      dotRadius = Math.min(3.2, 1.15 * Math.sqrt(cam().zoom));
    for (const point of visibleStars) {
      const color = knownStars.has(point.star.id)
          ? colors[point.star.class] || "#fff"
          : "#6d8296",
        group = groups.get(color) || [];
      group.push(point);
      groups.set(color, group);
    }
    for (const [color, points] of groups) {
      ctx.fillStyle = color;
      ctx.beginPath();
      for (const point of points) {
        ctx.moveTo(point.x + dotRadius, point.y);
        ctx.arc(point.x, point.y, dotRadius, 0, Math.PI * 2);
      }
      ctx.fill();
    }
    const selectedPoint = visibleStars.find(
      (point) => point.star.id === selected,
    );
    if (selectedPoint) {
      ctx.fillStyle = "#efad5818";
      ctx.beginPath();
      ctx.arc(selectedPoint.x, selectedPoint.y, 15, 0, Math.PI * 2);
      ctx.fill();
      ctx.strokeStyle = "#efad58";
      ctx.lineWidth = 1.5;
      ctx.beginPath();
      ctx.arc(selectedPoint.x, selectedPoint.y, 8, 0, Math.PI * 2);
      ctx.stroke();
      ctx.beginPath();
      for (const [dx, dy] of [[-1, 0], [1, 0], [0, -1], [0, 1]]) {
        ctx.moveTo(selectedPoint.x + dx * 12, selectedPoint.y + dy * 12);
        ctx.lineTo(selectedPoint.x + dx * 18, selectedPoint.y + dy * 18);
      }
      ctx.stroke();
    }
    drawNearbyLabels(w, h);
    if (flags.labels) {
      ctx.font = "11px monospace";
      ctx.fillStyle = "#dce5ed";
      for (const region of regionCenters) {
        if (region.galaxy !== viewedGalaxy) continue;
        const point = galaxyPoint(region);
        if (point.x >= -80 && point.y >= -20 && point.x <= w && point.y <= h)
          ctx.fillText(tr(region.name), point.x, point.y);
      }
    }
    if (
      galaxy.black_holes?.[viewedGalaxy] ||
      (viewedGalaxy === "edem" && galaxy.black_hole)
    ) {
      const p = galaxyPoint(
        galaxy.black_holes?.[viewedGalaxy] || galaxy.black_hole,
      );
      ctx.strokeStyle = "#70618c";
      ctx.lineWidth = 1;
      for (const id of (galaxy.black_holes?.[viewedGalaxy] || galaxy.black_hole)
        .systems) {
        const q = galaxyPoint(galaxy.stars[id - 1]);
        ctx.beginPath();
        ctx.moveTo(p.x, p.y);
        ctx.lineTo(q.x, q.y);
        ctx.stroke();
      }
      ctx.strokeStyle = "#c8a0ed";
      ctx.lineWidth = 3;
      ctx.beginPath();
      ctx.ellipse(p.x, p.y, 17, 7, 0.4, 0, Math.PI * 2);
      ctx.stroke();
      ctx.fillStyle = "#02030a";
      ctx.beginPath();
      ctx.arc(p.x, p.y, 8, 0, Math.PI * 2);
      ctx.fill();
    }
    if (flags.ships || flags.others) {
      const groups = new Map();
      for (const ship of mapShips()) {
        if (!galaxy.stars[ship.star - 1] || galaxy.stars[ship.star - 1].deleted)
          continue;
        const p = galaxyShip(ship),
          key =
            shipOwner(ship) +
            ":" +
            (ship.destination ? "flight-" + ship.id : "star-" + ship.star),
          group = groups.get(key) || [];
        group.push({ ship, p });
        groups.set(key, group);
      }
      const positions = new Map();
      for (const group of groups.values()) {
        const ship = group[0].ship,
          p = { ...group[0].p };
        const location = Math.round(p.x) + ":" + Math.round(p.y),
          rank = positions.get(location) || 0;
        positions.set(location, rank + 1);
        if (rank) {
          p.x += Math.cos(rank * 2.4) * (24 + rank * 5);
          p.y += Math.sin(rank * 2.4) * (24 + rank * 5);
        }
        if (starGalaxy(ship.star) !== viewedGalaxy) continue;
        ctx.strokeStyle = shipColor(ship);
        ctx.beginPath();
        ctx.arc(p.x, p.y, 15, 0, Math.PI * 2);
        ctx.stroke();
        Visuals.ship(ctx, p.x, p.y, 10, p.angle || 0, shipColor(ship));
        ctx.fillStyle = shipColor(ship);
        ctx.font = "11px monospace";
        ctx.fillText(
          group.length > 1
            ? `${group.length} ${tr("Кораблей")}`
            : tr(ship.name),
          p.x + 19,
          p.y - 10,
        );
        for (const item of group)
          shipHits.push({ ship: item.ship, x: p.x, y: p.y });
      }
    }
  }
  function fleetPanel() {
    const section = e("section");
    section.append(e("h3", tr("Мой флот")));
    for (const ship of fleet()) {
      const card = e("div", undefined, "fleet-card");
      card.append(
        e("b", shipName(ship)),
        e(
          "p",
          ship.destination
            ? `${tr("В пути")} · ${ship.star} → ${ship.destination}`
            : galaxy.stars[ship.star - 1].name,
        ),
        UI.button("Найти на карте", () => findShip(ship.id)),
      );
      if (ship.id !== Session.data.expedition.ship.id)
        card.append(
          UI.button("Выбрать", () =>
            Game.command({ action: "select_ship", ship: ship.id }),
          ),
        );
      section.append(card);
    }
    side.append(section);
  }
  function panelTabs() {
    UI.tabs(
      side,
      [
        ["system", "Система"],
        ["route", "Маршрут"],
        ["fleet", "Флот"],
      ],
      panelTab,
      (value) => {
        panelTab = value;
        mode === "system" ? systemPanel() : routePanel();
      },
    );
  }
  function routePanel() {
    UI.rememberFolds(side);
    side.replaceChildren();
    panelTabs();
    if (panelTab === "fleet") {
      fleetPanel();
      return;
    }
    if (panelTab === "system") {
      const star = galaxy.stars[selected - 1];
      side.append(
        e("h3", star.name),
        e(
          "p",
          tr(
            Game.known(selected)
              ? "Система исследована"
              : "Неизвестная система",
          ),
        ),
        UI.button("Открыть систему", () => openSystem(selected), false),
        UI.button(
          "Исследовать",
          () => Game.explore(selected),
          Game.known(selected),
          "Система уже исследована",
        ),
      );
      const knowledge = Session.data.workbench.known?.find(
        (item) => item.star === selected,
      );
      if (knowledge)
        side.append(
          e(
            "p",
            tr("Получено") +
              ": " +
              new Date(knowledge.received_at).toLocaleString(I18n.language) +
              " · " +
              tr(knowledge.source),
            "muted",
          ),
        );
      const note = Session.data.workbench.notes?.find(
        (v) => v.star === selected && !v.object_id,
      );
      const text = UI.field(
        side,
        "Личная заметка",
        "system-note",
        note?.note || "",
      );
      side.append(
        UI.button("Сохранить заметку", () =>
          Game.command({
            action: "note",
            star: selected,
            note: text.value,
            bookmark: note?.bookmark || false,
          }),
        ),
        UI.button(note?.bookmark ? "Убрать закладку" : "В закладки", () =>
          Game.command({
            action: "note",
            star: selected,
            note: text.value,
            bookmark: !note?.bookmark,
          }),
        ),
      );
      return;
    }
    side.append(e("h3", tr("Планировщик маршрута")));
    const from = Session.data.expedition?.ship.star || 1;
    UI.row(side, "Отправление", galaxy.stars[from - 1].name);
    const target = systems(selected, (value) => {
      selected = Number(value);
      routePanel();
      draw();
    }, viewedGalaxy);
    target.id = "route-to";
    side.append(target);
    const result = e("p");
    result.id = "route-result";
    const details = UI.fold("map:waypoints");
    details.append(e("summary", tr("Промежуточные точки")));
    details.append(
      UI.button("Добавить точку", () => {
        routeStops.push(selected);
        routePanel();
      }),
    );
    for (const [id, to] of routeStops.entries()) {
      const line = e("div", undefined, "inline-controls");
      line.append(
        e("span", galaxy.stars[to - 1]?.name || to),
        UI.button("×", () => {
          routeStops.splice(id, 1);
          routePanel();
        }),
      );
      details.append(line);
    }
    side.append(details);
    const path = plannedRoute(from, [...routeStops, selected]),
      jumps = path ? path.length - 1 : 0;
    result.textContent = path
      ? `${jumps} ${tr("переходов")} · ${jumps * (Session.data.expedition.ship.spec?.jump_seconds || 5)} s · ${jumps * (Session.data.expedition.ship.spec?.jump_fuel || 2)} ${tr("Топливо")}`
      : tr("Нет пути: системы не связаны гиперкоридорами.");
    side.append(result);
    if (
      path &&
      jumps * (Session.data.expedition.ship.spec?.jump_fuel || 2) * 2 >
        Session.data.expedition.ship.fuel
    )
      side.append(
        e("p", tr("Топлива на обратный путь может не хватить"), "error"),
      );
    side.append(
      UI.button(
        "Отправиться",
        () =>
          Game.command({
            action: "route",
            waypoints: [...routeStops, selected],
          }),
        !path ||
          !jumps ||
          jumps * (Session.data.expedition.ship.spec?.jump_fuel || 2) >
            Session.data.expedition.ship.fuel,
        "Недостаточно топлива или нет пути",
      ),
    );
    const way = UI.fold("map:route-systems");
    way.append(
      e("summary", tr("Системы маршрута")),
      e("p", path?.map((id) => galaxy.stars[id - 1].name).join(" → ") || "—"),
    );
    side.append(way);
    const bookmarks = UI.fold("map:bookmarks");
    bookmarks.append(e("summary", tr("Закладки")));
    for (const item of Session.data.workbench.notes?.filter(
      (n) => n.bookmark,
    ) || [])
      bookmarks.append(
        UI.button(galaxy.stars[item.star - 1]?.name || item.star, () => {
          selected = item.star;
          updateSystemPicker();
          routePanel();
          draw();
        }),
      );
    side.append(bookmarks);
  }
  function plannedRoute(from, points) {
    const full = [from];
    for (const to of points) {
      const current = full[full.length - 1];
      if (current === to) continue;
      const part = localPath(current, to);
      if (!part) return null;
      full.push(...part.slice(1));
    }
    return full;
  }
  function updateRoute() {
    const ship = Session.data.expedition?.ship;
    if (!ship) return;
    const key = JSON.stringify([
      galaxy.revision,
      ship.id,
      ship.star,
      ship.path,
      ship.waypoints,
      selected,
      routeStops,
    ]);
    if (key === routeKey) return;
    routeKey = key;
    route = plannedRoute(ship.star, [...routeStops, selected]) || [];
    activeRoute = ship.destination ? [...ship.path] : [ship.star];
    const tail = plannedRoute(
      activeRoute[activeRoute.length - 1],
      ship.waypoints || [],
    );
    if (tail) activeRoute.push(...tail.slice(1));
  }
  function localPath(from, to) {
    if (from === to) return [from];
    const adj = new Map();
    for (const lane of galaxy.lanes) {
      for (const [a, b] of [
        [lane.from, lane.to],
        [lane.to, lane.from],
      ]) {
        if (!adj.has(a)) adj.set(a, []);
        adj.get(a).push(b);
      }
    }
    const prev = new Map([[from, null]]),
      q = [from];
    for (let i = 0; i < q.length; i++) {
      const id = q[i];
      for (const next of adj.get(id) || []) {
        if (prev.has(next)) continue;
        prev.set(next, id);
        if (next === to) {
          const path = [];
          let v = to;
          while (v !== null) {
            path.unshift(v);
            v = prev.get(v);
          }
          return path;
        }
        q.push(next);
      }
    }
    return null;
  }
  function siteActions(site, x, y) {
    const targetStar = local.star.id;
    selectedSite = site.id;
    systemPanel();
    draw();
    const ship = Session.data.expedition?.ship,
      current = ship?.star === targetStar,
      moving = !!ship?.destination || !!ship?.local_target,
      blocked = !current || moving || !!Session.data.expedition?.job,
      items = [
        { heading: site.name },
        { label: "Информация", action: () => showObject(site, local.star) },
        {
          label: "Приблизиться",
          disabled: blocked,
          reason: current
            ? "Дождитесь завершения действия"
            : "Прибудьте в систему",
          action: () =>
            performExpedition({
              action: "move",
              target: site.id,
              mode: "approach",
            }),
        },
      ];
    if (site.details)
      items.push({
        label:
          site.kind === "Садовый мир"
            ? "Поверхность и колонизация"
            : "Поверхность планеты",
        action: () => openPlanet(targetStar, site.details.id),
      });
    if (site.details)
      items.push({
        label: "Перейти на орбиту",
        disabled: blocked,
        action: () =>
          performExpedition({ action: "move", target: site.id, mode: "orbit" }),
      });
    if (site.kind === "Орбитальная станция")
      items.push(
        {
          label: "Стыковаться",
          disabled: blocked,
          action: () =>
            performExpedition({
              action: "move",
              target: site.id,
              mode: "docked",
            }),
        },
        {
          label: "Рынок",
          action: () => {
            window.dispatchEvent(
              new CustomEvent("stationmarket", { detail: targetStar }),
            );
            Modules.open("market");
          },
        },
      );
    if (site.kind === "Орбитальная станция")
      items.push({
        label: "Личное название и заметка",
        action: () => stationEditor(targetStar, site.id),
      });
    if (site.resources?.length)
      items.push({
        label: "Добыча",
        action: () => Modules.open("research"),
      });
    if (site.details && Session.data.state?.user.role === "admin")
      items.push(
        { separator: true },
        {
          label: "Телепортироваться",
          action: () => Game.teleport(targetStar),
        },
        {
          label: "Колония",
          children: [
            {
              label: "Удалить генерацию колонии",
              action: () =>
                adminPlanetSurface(targetStar, site.details.id, "delete"),
            },
            {
              label: "Перегенерировать колонию",
              action: () =>
                adminPlanetSurface(targetStar, site.details.id, "regenerate"),
            },
          ],
        },
      );
    UI.menu(items, x, y);
  }
  function systemPanel() {
    UI.rememberFolds(side);
    side.replaceChildren();
    panelTabs();
    if (panelTab === "fleet") {
      fleetPanel();
      return;
    }
    if (panelTab === "route") {
      routePanel();
      return;
    }
    local = personalizeStations(local);
    side.append(e("h3", local.star.name), e("p", tr(local.region)));
    const stellar = e("p");
    stellar.append(
      hint(
        local.star.class,
        "Спектральный класс звезды: O, B, A, F, G, K, M — от горячих к холодным. Солнце относится к G.",
      ),
      document.createTextNode(" · "),
      hint(
        Format.science(local.star.mass) + " M☉",
        "M☉ — масса Солнца. Значение 1 означает массу, равную солнечной.",
      ),
      document.createTextNode(" · "),
      hint(
        Format.science(local.star.luminosity) + " L☉",
        "L☉ — светимость Солнца: полная мощность излучения, а не видимая яркость на небе.",
      ),
    );
    side.append(stellar);
    if (!local.surveyed)
      side.append(
        e(
          "p",
          tr(
            local.sites.length
              ? "Сканирование раскрывает объекты по очереди."
              : "Система не исследована. Исследуйте её, чтобы открыть объекты.",
          ),
          "muted",
        ),
      );
    const chosen = local.sites.find((s) => s.id === selectedSite);
    if (chosen) {
      const model = objectHologram(chosen, local.star);
      model.classList.add("inspector-model");
      side.append(
        model,
        chosen.kind === "Орбитальная станция"
          ? stationLabel(local.star.id, chosen.id, chosen.name, "h3")
          : e("h3", chosen.name),
        UI.button("Все объекты", () => {
          selectedSite = null;
          systemPanel();
          draw();
        }),
      );
    }
    for (const [kind, name] of [
      ["Орбитальная станция", "Порты"],
      ["planet", "Планеты"],
      ["belt", "Месторождения"],
    ]) {
      const group = UI.fold("map:objects:" + local.star.id + ":" + kind, true);
      const objects = local.sites.filter((s) =>
        kind === "planet"
          ? !!s.details
          : kind === "belt"
            ? s.resources?.length && !s.details
            : s.kind === kind,
      );
      group.append(e("summary", tr(name) + " · " + objects.length));
      for (const site of objects) {
        const button = UI.button("", (ev) => {
          const r = button.getBoundingClientRect();
          siteActions(site, r.x, r.bottom);
        });
        button.className = "object-row";
        button.dataset.site = site.id;
        const icon = e("canvas");
        icon.width = 50;
        icon.height = 50;
        Visuals.object(icon.getContext("2d"), 25, 25, 15, site);
        button.append(
          icon,
          site.kind === "Орбитальная станция"
            ? stationLabel(local.star.id, site.id, site.name)
            : e("span", site.name),
        );
        button.classList.toggle("selected", selectedSite === site.id);
        button.onpointerenter = () => {
          hoveredSite = site.id;
          draw();
        };
        button.onpointerleave = () => {
          hoveredSite = null;
          draw();
        };
        group.append(button);
      }
      side.append(group);
    }
  }
  function openSystem(id, notifySelection = true) {
    if (Modules.isLoading("galaxy")) return Modules.wait("galaxy", loadSystem(id, notifySelection, Modules.signal("galaxy")));
    return Modules.load("galaxy", signal => loadSystem(id, notifySelection, signal));
  }
  function refreshSystem() {
    if (!local || root.classList.contains("planet-open") || Modules.isLoading("galaxy")) return;
    const id = local.star.id, selection = request, epoch = Session.epoch;
    return Modules.update("galaxy", async signal => {
      const data = await api("system?id=" + id, undefined, signal);
      if (selection !== request || epoch !== Session.epoch || !Session.signedIn ||
          !Modules.isOpen("galaxy") || Modules.isLoading("galaxy") || mode !== "system" ||
          local?.star.id !== id || root.classList.contains("planet-open") || signal.aborted) return;
      local = data;
      label.textContent = data.star.name + " · " + tr(data.region);
      if (panelTab === "system") systemPanel();
      draw();
    }).catch(error => {
      if (error.name !== "AbortError" && selection === request && epoch === Session.epoch && Session.signedIn)
        notify(error.message, "error");
    });
  }
  async function loadSystem(id, notifySelection, signal) {
    if (!Number.isInteger(id) || id < 1 || id > (galaxy?.stars.length || 0)) {
      notify("Некорректная система");
      return;
    }
    const key = ++request,
      epoch = Session.epoch;
    try {
      const d = await api("system?id=" + id, undefined, signal);
      if (signal?.aborted || key !== request || epoch !== Session.epoch || !Session.signedIn || !Modules.isOpen("galaxy"))
        return;
      if (local?.star.id !== id) {
        selectedSite = null;
        camera.system = { zoom: 1, x: 0, y: 0 };
      }
      local = d;
      viewedGalaxy = starGalaxy(id);
      updateGalaxyPicker();
      setMode("system");
      panelTab = "system";
      selected = id;
      updateSystemPicker();
      back.hidden = false;
      label.textContent = d.star.name + " · " + tr(d.region);
      if (notifySelection)
        window.dispatchEvent(
          new CustomEvent("galaxyselection", { detail: id }),
        );
      systemPanel();
      draw();
    } catch (error) {
      if (key === request && epoch === Session.epoch && Session.signedIn)
        notify(error.message, "error");
    }
  }
  async function load() {
    if (galaxy) {
      if (world.data && world.data !== galaxy) {
        galaxy = world.data;
        updateSystemPicker();
      }
      if (mode === "galaxy") routePanel();
      draw();
      if (pendingFind) findShip(pendingFind);
      if (pendingStar) showStar(pendingStar);
      return;
    }
    if (loading) return;
    loading = true;
    const epoch = Session.epoch;
    try {
      const loaded = world.data || (await loadWorld());
      if (
        !loaded ||
        epoch !== Session.epoch ||
        !Session.signedIn ||
        !Modules.isOpen("galaxy")
      )
        return;
      galaxy = loaded;
      world.data = galaxy;
      selected = Session.data.expedition.ship.star;
      viewedGalaxy = starGalaxy(selected);
      updateSystemPicker();
      updateGalaxyPicker();
      label.textContent = galaxyTitle();
      routePanel();
      draw();
      const focusRequested = pendingFind || pendingStar;
      if (pendingFind) findShip(pendingFind);
      if (pendingStar) showStar(pendingStar);
      if (!focusRequested) findShip(Session.data.expedition.ship.id);
      if (currentSystemRequested) {
        currentSystemRequested = false;
        await openSystem(Session.data.expedition.ship.star);
      }
    } catch (error) {
      if (epoch === Session.epoch && Session.signedIn) notify(error.message);
    } finally {
      if (epoch === Session.epoch) loading = false;
    }
  }
  function showStar(id) {
    pendingStar = id;
    if (!galaxy) return;
    const star = galaxy.stars.find((s) => s.id === id);
    if (!star || star.deleted) return;
    request++;
    pendingStar = null;
    pendingFind = null;
    selected = id;
    viewedGalaxy = starGalaxy(id);
    setMode("galaxy");
    panelTab = "route";
    back.hidden = true;
    camera.galaxy = { zoom: 3, x: 0, y: 0 };
    const point = galaxyPoint(star);
    camera.galaxy.x = canvas.clientWidth / 2 - point.x;
    camera.galaxy.y = canvas.clientHeight / 2 - point.y;
    updateSystemPicker();
    updateGalaxyPicker();
    routePanel();
    draw();
  }
  window.addEventListener("returntoplanetsystem", async (event) => {
    if (!Session.signedIn) return;
    await load();
    if (galaxy && Session.signedIn) openSystem(event.detail);
  });
  window.addEventListener("showcurrentsystem", () => {
    scenes.planet?.dismiss(false);
    if (galaxy && Session.signedIn)
      openSystem(Session.data.expedition.ship.star);
    else {
      currentSystemRequested = true;
      load();
    }
  });
  window.addEventListener("showstar", (event) => {
    scenes.planet?.dismiss(false);
    showStar(Number(event.detail));
  });
  function findShip(id) {
    pendingFind = id;
    if (!galaxy) return;
    const ship = fleet().find((s) => s.id === id);
    if (!ship) return;
    request++;
    pendingFind = null;
    pendingStar = null;
    selected = ship.star;
    viewedGalaxy = starGalaxy(ship.star);
    updateSystemPicker();
    updateGalaxyPicker();
    setMode("galaxy");
    back.hidden = true;
    camera.galaxy.zoom = 2;
    camera.galaxy.x = 0;
    camera.galaxy.y = 0;
    const p = galaxyShip(ship);
    camera.galaxy.x = canvas.clientWidth / 2 - p.x;
    camera.galaxy.y = canvas.clientHeight / 2 - p.y;
    label.textContent = galaxyTitle();
    routePanel();
    draw();
  }
  function zoomAt(
    factor,
    x = canvas.clientWidth / 2,
    y = canvas.clientHeight / 2,
  ) {
    const c = cam(),
      next = Math.max(0.5, Math.min(18, c.zoom * factor)),
      scale = next / c.zoom;
    c.x =
      x - canvas.clientWidth / 2 - (x - canvas.clientWidth / 2 - c.x) * scale;
    c.y =
      y - canvas.clientHeight / 2 - (y - canvas.clientHeight / 2 - c.y) * scale;
    c.zoom = next;
    draw();
  }
  inspect.onclick = () => openSystem(Number(system.value));
  function showGalaxy() {
    panelTab = "route";
    currentSystemRequested = false;
    pendingStar = null;
    pendingFind = null;
    scenes.planet?.dismiss(false);
    request++;
    setMode("galaxy");
    back.hidden = true;
    if (galaxy) {
      label.textContent = galaxyTitle();
      routePanel();
      draw();
    }
  }
  back.onclick = showGalaxy;
  window.addEventListener("showgalaxy", showGalaxy);
  reset.onclick = () => {
    camera[mode] = { zoom: 1, x: 0, y: 0 };
    draw();
  };
  minus.onclick = () => zoomAt(0.8);
  plus.onclick = () => zoomAt(1.25);
  canvas.addEventListener(
    "wheel",
    (event) => {
      event.preventDefault();
      event.stopPropagation();
      const r = canvas.getBoundingClientRect();
      zoomAt(
        Math.exp(-event.deltaY * 0.001),
        event.clientX - r.x,
        event.clientY - r.y,
      );
    },
    { passive: false },
  );
  canvas.onpointerdown = (event) => {
    if (event.button !== 0) return;
    hideSummary();
    pointer = {
      x: event.clientX,
      y: event.clientY,
      startX: event.clientX,
      startY: event.clientY,
      moved: false,
    };
    canvas.setPointerCapture(event.pointerId);
  };
  canvas.onpointermove = (event) => {
    trackLabelFocus(event);
    if (!pointer) {
      if (mode === "galaxy") hoverSummary(event);
      if (mode === "system") {
        const r = canvas.getBoundingClientRect(),
          hit = sites.find(
            (s) =>
              s.visible &&
              Math.hypot(event.clientX - r.x - s.x, event.clientY - r.y - s.y) <
                s.radius + 7,
          );
        if (hoveredSite !== hit?.id) {
          hoveredSite = hit?.id;
          side
            .querySelectorAll("[data-site]")
            .forEach((b) =>
              b.classList.toggle(
                "selected",
                b.dataset.site === hoveredSite ||
                  b.dataset.site === selectedSite,
              ),
            );
          draw();
        }
      }
      return;
    }
    pointer.moved ||=
      Math.hypot(
        event.clientX - pointer.startX,
        event.clientY - pointer.startY,
      ) > 5;
    cam().x += event.clientX - pointer.x;
    cam().y += event.clientY - pointer.y;
    pointer.x = event.clientX;
    pointer.y = event.clientY;
    draw();
  };
  canvas.onpointercancel = () => (pointer = null);
  canvas.onlostpointercapture = () => (pointer = null);
  function clickMap(event) {
    if (!galaxy) return;
    hideSummary();
    const r = canvas.getBoundingClientRect(),
      x = event.clientX - r.x,
      y = event.clientY - r.y,
      hit = shipHits.find((s) => Math.hypot(x - s.x, y - s.y) < 13);
    if (hit) {
      UI.menu(
        [
          {
            heading:
              hit.ship.name +
              (hit.ship.owner_name ? " · " + hit.ship.owner_name : ""),
          },
          ...(hit.ship.owner
            ? []
            : [{ label: "Корабль", action: () => Modules.open("ship") }]),
          { label: "К системе", action: () => openSystem(hit.ship.star) },
        ],
        event.clientX,
        event.clientY,
      );
      return;
    }
    if (mode === "system") {
      const center = localPoint({
        x: canvas.clientWidth / 2,
        y: canvas.clientHeight / 2,
      });
      if (Math.hypot(x - center.x, y - center.y) < 20) {
        selectedSite = null;
        systemPanel();
        draw();
        return;
      }
      const candidates = sites
        .filter((s) => s.visible && Math.hypot(x - s.x, y - s.y) < s.radius + 8)
        .sort(
          (a, b) => Math.hypot(x - a.x, y - a.y) - Math.hypot(x - b.x, y - b.y),
        );
      if (candidates.length > 1) {
        UI.menu(
          candidates.map((s) => ({
            label: s.name + " · " + tr(s.kind),
            action: () => siteActions(s, event.clientX, event.clientY),
          })),
          event.clientX,
          event.clientY,
        );
      } else if (candidates[0])
        siteActions(candidates[0], event.clientX, event.clientY);
      return;
    }
    let closest,
      distance = 9;
    for (const { star, x: sx, y: sy } of visibleStars) {
      const d = Math.hypot(x - sx, y - sy);
      if (d < distance) {
        distance = d;
        closest = star;
      }
    }
    if (closest) {
      selected = closest.id;
      updateSystemPicker();
      draw();
      const star = closest;
      UI.menu(
        [
          { heading: star.name },
          {
            label: "Построить маршрут",
            action: () => {
              panelTab = "route";
              routePanel();
            },
          },
          {
            label: "Открыть систему",
            action: () => openSystem(star.id),
          },
          {
            label: "Исследовать",
            disabled: Game.known(star.id),
            reason: "Система уже исследована",
            action: () => Game.explore(star.id),
          },
          ...(Session.data.state?.user.role === "admin"
            ? [
                { separator: true },
                {
                  label: "Телепортироваться",
                  action: () => Game.teleport(star.id),
                },
              ]
            : []),
        ],
        event.clientX,
        event.clientY,
      );
    } else if (
      galaxy.black_holes?.[viewedGalaxy] ||
      (viewedGalaxy === "edem" && galaxy.black_hole)
    ) {
      const p = galaxyPoint(
        galaxy.black_holes?.[viewedGalaxy] || galaxy.black_hole,
      );
      if (Math.hypot(x - p.x, y - p.y) < 22) Modules.open("portal");
    }
  }
  canvas.onpointerup = (event) => {
    if (!pointer) return;
    const moved = pointer.moved;
    pointer = null;
    if (!moved) clickMap(event);
  };
  canvas.oncontextmenu = (event) => {
    event.preventDefault();
    clickMap(event);
  };
  window.addEventListener("universechange", () => {
    request++;
    hideSummary();
    summaryCache.clear();
    galaxy = world.data;
    selected = Session.data.expedition.ship.star;
    viewedGalaxy = starGalaxy(selected);
    updateGalaxyPicker();
    label.textContent = galaxyTitle();
    back.hidden = true;
    setMode("galaxy");
    local = null;
    route = [];
    camera.galaxy = { zoom: 1, x: 0, y: 0 };
    updateSystemPicker();
    routePanel();
    draw();
  });
  window.addEventListener("galaxyselection", (ev) => {
    if (!galaxy) return;
    selected = ev.detail;
    updateSystemPicker();
    if (mode === "galaxy") routePanel();
    draw();
  });
  window.addEventListener("findship", (event) => {
    pendingFind = event.detail;
    if (Modules.isOpen("galaxy")) findShip(event.detail);
  });
  window.addEventListener("moduleopen", (event) => {
    if (event.detail === "galaxy") {
      syncMapNavigation();
      Modules.wait("galaxy", load());
    }
  });
  window.addEventListener("moduleclose", (event) => {
    if (event.detail === "galaxy") {
      hideSummary();
      syncMapNavigation();
    }
  });
  window.addEventListener("moduleresize", (event) => {
    if (event.detail === "galaxy") draw();
  });
  window.addEventListener("languagechange", () => {
    if (mode === "system" && local) openSystem(local.star.id, false);
    else if (galaxy) routePanel();
    draw();
  });
  window.addEventListener("gamedata", () => {
    const players = summary.querySelector(".system-presence");
    if (!summary.hidden && hoverID && players)
      players.textContent =
        tr("Присутствуют игроки") + ": " + systemPlayers(hoverID);
    knownStars = new Set(
      (Session.data.workbench?.known || []).map((item) => item.star),
    );
    if (galaxy && Session.data.workbench?.universe_revision > galaxy.revision) {
      if (!loading) {
        loading = true;
        const epoch = Session.epoch,
          source = galaxy;
        api("galaxy")
          .then((data) => {
            if (
              !Session.signedIn ||
              epoch !== Session.epoch ||
              galaxy !== source
            )
              return;
            world.data = data;
            window.dispatchEvent(new Event("universechange"));
          })
          .catch((error) => {
            if (
              Session.signedIn &&
              epoch === Session.epoch &&
              galaxy === source
            )
              notify(error.message, "error");
          })
          .finally(() => {
            if (epoch === Session.epoch) loading = false;
          });
      }
      return;
    }

    if (
      mode === "galaxy" &&
      galaxy &&
      !side.contains(document.activeElement) &&
      !document.querySelector(".picker-popover")
    )
      routePanel();
    if (
      mode === "system" &&
      local &&
      Modules.isOpen("galaxy") &&
      !side.contains(document.activeElement) &&
      !document.querySelector(".picker-popover")
    )
      refreshSystem();
  });
  window.addEventListener("signedout", () => {
    galaxy = null;
    loading = false;
    knownStars.clear();
    territorySource = null;
    territories = [];
    presenceSource = null;
    presence.clear();
    label.textContent = "";
    system.replaceChildren();
    ctx.clearRect(0, 0, canvas.width, canvas.height);
    hideSummary();
    labelFocus = null;
    currentSystemRequested = false;
    visibleStars = [];
    projectedSource = null;
    summaryCache.clear();
    request++;
    pendingStar = null;
    local = null;
    setMode("galaxy");
    route = [];
    activeRoute = [];
    routeStops = [];
    routeKey = "";
    pendingFind = null;
    back.hidden = true;
    side.replaceChildren();
  });
  new ResizeObserver(draw).observe(canvas);
  sceneLoop(
    () =>
      Modules.isOpen("galaxy") &&
      Session.signedIn &&
      !root.classList.contains("planet-open"),
    draw,
    100,
  );
}
