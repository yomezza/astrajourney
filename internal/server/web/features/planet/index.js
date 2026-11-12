import { openVisit } from "./visit.js";
import { mountColonyCatalog, clearColonyCatalog } from "./colony-catalog.js";
import { world, loadWorld } from "../../core/world-store.js";

import { terrain, texture, materials } from "./terrain.js";

import { objectTag } from "../objects/presentation.js";
import { Format } from "../../core/format.js";
import { Session } from "../../core/session.js";
import { I18n } from "../../core/i18n.js";
import { api } from "../../core/api.js";
import { refresh } from "../../core/snapshot-store.js";
import { notify } from "../../ui/feedback.js";
import { UI, Modules } from "../../ui/runtime.js";
import { scenes } from "../runtime.js";

export function initPlanet() {
  const n = UI.node,
    tr = (s) => I18n.t(s),
    btn = UI.button;
  let current = null,
    epoch = 0,
    polling = false,
    selected = 1;
  const actions = new Map();
  const statusName = (s) =>
    ({
      funding: "Финансирование",
      building: "Строительство",
      complete: "Колонизация завершена",
      cancelled: "Отменён",
      expired: "Просрочен",
    })[s] || s;
  const asset = (id) =>
    id === "credits"
      ? "CR"
      : tr(
          Session.data.expedition.resources.find((r) => r.id === id)?.name ||
            id,
        );
  async function send(input) {
    const session = Session.epoch;
    const key = JSON.stringify(input);
    let request = actions.get(key);
    if (!request) {
      request = Array.from(crypto.getRandomValues(new Uint32Array(4)), (v) =>
        v.toString(16),
      ).join("-");
      actions.set(key, request);
    }
    try {
      const result = await api("planet/action", { ...input, request });
      if (!Session.signedIn || session !== Session.epoch) return null;
      actions.delete(key);
      notify(
        tr(
          result.pending_refunds
            ? "Возврат ожидает получения в разделе Колонии"
            : "Действие выполнено",
        ),
        result.pending_refunds ? "warning" : "success",
      );
      await refresh(true);
      await update();
      return result;
    } catch (e) {
      notify(e.message, "error");
      return null;
    }
  }
  function confirm(title, message, input, done) {
    UI.confirm(title, message, async () => {
      const result = await send(input);
      if (result) await done?.(result);
      return !!result;
    });
  }
  function close(returnToSystem = true) {
    if (!current) return;
    const old = current;
    current = null;
    epoch++;
    old.resize?.disconnect();
    if (returnToSystem)
      window.dispatchEvent(
        new CustomEvent("returntoplanetsystem", { detail: old.data.star }),
      );
    old.host.classList.add("planet-leaving");
    setTimeout(() => {
      old.host.remove();
      if (!current || current.root !== old.root)
        old.root.classList.remove("planet-open");
      window.dispatchEvent(
        new CustomEvent("moduleresize", { detail: "galaxy" }),
      );
    }, 160);
  }
  function hex(s) {
    return Array.from({ length: 6 }, (_, i) => {
      const a = (Math.PI / 3) * i;
      return { x: s.center.x + Math.sin(a), y: s.center.y + Math.cos(a) };
    });
  }
  function color(s, claim) {
    if (claim) {
      if (claim.status === "funding") return "#eac85e";
      if (claim.status === "building") return "#eea45f";
      if (claim.owner_user === Session.data.state.user.id) return "#7ad7a7";
      if (claim.owner_company === current.state.company) return "#baa0ef";
      return "#e09091";
    }
    return s.land < current.state.settings.min_land || !current.state.habitable
      ? "#607588"
      : "#b9cad0";
  }
  function draw() {
    const c = current;
    if (!c?.g || document.hidden || !Modules.isOpen("galaxy")) return;
    const { canvas, g, camera } = c;
    const w = canvas.clientWidth,
      h = canvas.clientHeight;
    if (!w || !h) return;
    const ratio = devicePixelRatio || 1;
    canvas.width = Math.round(w * ratio);
    canvas.height = Math.round(h * ratio);
    const ctx = canvas.getContext("2d");
    ctx.setTransform(ratio, 0, 0, ratio, 0, 0);
    ctx.fillStyle = "#0d1923";
    ctx.fillRect(0, 0, w, h);
    const base = Math.min((w - 30) / g.width, (h - 30) / g.height);
    c.scale = base * camera.zoom;
    c.ox = w / 2 - (g.width * c.scale) / 2 + camera.x;
    c.oy = h / 2 - (g.height * c.scale) / 2 + camera.y;
    ctx.translate(c.ox, c.oy);
    ctx.scale(c.scale, c.scale);
    // One coherent geographic raster, clipped to the exact union of sector polygons.
    ctx.beginPath();
    for (const s of g.sectors) {
      const points = hex(s);
      ctx.moveTo(points[0].x, points[0].y);
      for (const p of points.slice(1)) ctx.lineTo(p.x, p.y);
      ctx.closePath();
    }
    ctx.save();
    ctx.clip();
    ctx.imageSmoothingEnabled = true;
    ctx.drawImage(c.terrain, 0, 0, g.width, g.height);
    ctx.strokeStyle = "#72bccb";
    ctx.lineWidth = 0.04;
    ctx.lineCap = "round";
    for (const river of g.rivers) {
      ctx.beginPath();
      river.forEach((p, i) =>
        i ? ctx.lineTo(p.x, p.y) : ctx.moveTo(p.x, p.y),
      );
      ctx.stroke();
    }
    ctx.restore();
    const claims = new Map(c.state.claims.map((cl) => [cl.sector, cl]));
    const landings = new Map(
      (c.state.landing?.sites || []).map((site) => [site.sector, site]),
    );
    for (const s of g.sectors) {
      const points = hex(s),
        claim = claims.get(s.number);
      ctx.beginPath();
      ctx.moveTo(points[0].x, points[0].y);
      for (const p of points.slice(1)) ctx.lineTo(p.x, p.y);
      ctx.closePath();
      const line = color(s, claim);
      ctx.strokeStyle = line;
      ctx.lineWidth = (s.number === selected ? 2.5 : 1) / c.scale;
      ctx.globalAlpha = claim?.status
        ? 0.16
        : s.number === selected
          ? 0.18
          : 0.03;
      ctx.fillStyle = line;
      ctx.fill();
      ctx.globalAlpha = 1;
      ctx.stroke();
      if (camera.zoom > 1.4 || g.sectors.length <= 100) {
        ctx.fillStyle = "#e5eef1";
        ctx.font = `${10 / c.scale}px monospace`;
        ctx.textAlign = "center";
        ctx.fillText(s.number, s.center.x, s.center.y);
      }
      const landing = landings.get(s.number);
      if (landing) {
        ctx.fillStyle = landing.accessible ? "#72d4b1" : "#a4aebb";
        ctx.font = `bold ${Math.max(12 / c.scale, 0.23)}px system-ui`;
        ctx.textAlign = "center";
        ctx.fillText("⇣", s.center.x, s.center.y + 0.45);
      }
    }
  }
  // Public art is cached per browser page; no geography or player data enters
  // this cache. Nothing is fetched until an actual surface map is opened.

  async function applyTextures(c) {
    if (c.texturesPending || current !== c || !c.g) return;
    c.texturesPending = true;
    c.textureStatus.hidden = false;
    c.textureStatus.textContent = tr("Загружаем текстуры поверхности…");
    c.textureRetry.hidden = true;
    try {
      const names = materials(c.data.kind, c.g),
        loaded = await Promise.allSettled(names.map(texture));
      if (current !== c) return;
      const available = {};
      loaded.forEach((result, i) => {
        if (result.status === "fulfilled") available[names[i]] = result.value;
      });
      // Let the loading status paint before preparing the cached terrain raster.
      await new Promise((resolve) => requestAnimationFrame(resolve));
      if (current !== c) return;
      if (Object.keys(available).length) {
        c.terrain = terrain(c.g, c.data.kind, available);
        draw();
      }
      const complete = loaded.every((result) => result.status === "fulfilled");
      c.canvas.dataset.textures = complete ? "ready" : "partial";
      c.textureStatus.hidden = complete;
      if (!complete) {
        c.textureStatus.textContent = tr(
          "Часть текстур недоступна. Используется доступное оформление.",
        );
        c.textureRetry.hidden = false;
      }
    } catch (error) {
      if (current === c) {
        c.canvas.dataset.textures = "partial";
        c.textureStatus.hidden = false;
        c.textureStatus.textContent = tr(
          "Часть текстур недоступна. Используется доступное оформление.",
        );
        c.textureRetry.hidden = false;
      }
    } finally {
      c.texturesPending = false;
    }
  }

  function controls(c) {
    let pointers = new Map(),
      dragged = false,
      down = null,
      pinch = 0;
    const bounds = () => c.canvas.getBoundingClientRect();
    const point = (e) => {
      const r = bounds();
      return { x: e.clientX - r.left, y: e.clientY - r.top };
    };
    const constrain = () => {
      const w = c.canvas.clientWidth,
        h = c.canvas.clientHeight;
      c.camera.x = Math.max(
        (-w * c.camera.zoom) / 2,
        Math.min((w * c.camera.zoom) / 2, c.camera.x),
      );
      c.camera.y = Math.max(
        (-h * c.camera.zoom) / 2,
        Math.min((h * c.camera.zoom) / 2, c.camera.y),
      );
    };
    const zoom = (factor, x, y) => {
      const old = c.camera.zoom;
      c.camera.zoom = Math.max(0.7, Math.min(8, old * factor));
      const ratio = c.camera.zoom / old;
      c.camera.x =
        (c.camera.x + c.canvas.clientWidth / 2 - x) * ratio -
        (c.canvas.clientWidth / 2 - x);
      c.camera.y =
        (c.camera.y + c.canvas.clientHeight / 2 - y) * ratio -
        (c.canvas.clientHeight / 2 - y);
      constrain();
      draw();
    };
    c.canvas.onwheel = (e) => {
      e.preventDefault();
      const p = point(e);
      zoom(Math.exp(-e.deltaY * 0.0015), p.x, p.y);
    };
    c.canvas.onpointerdown = (e) => {
      if (e.button !== 0) return;
      c.canvas.setPointerCapture(e.pointerId);
      pointers.set(e.pointerId, point(e));
      down = point(e);
      dragged = false;
      if (pointers.size === 2) {
        const [a, b] = [...pointers.values()];
        pinch = Math.hypot(a.x - b.x, a.y - b.y);
      }
    };
    c.canvas.onpointermove = (e) => {
      if (!pointers.has(e.pointerId)) return;
      const old = pointers.get(e.pointerId),
        p = point(e);
      pointers.set(e.pointerId, p);
      if (pointers.size === 2) {
        const [a, b] = [...pointers.values()],
          distance = Math.hypot(a.x - b.x, a.y - b.y);
        if (pinch) zoom(distance / pinch, (a.x + b.x) / 2, (a.y + b.y) / 2);
        pinch = distance;
        dragged = true;
        return;
      }
      if (down && Math.hypot(p.x - down.x, p.y - down.y) > 4) dragged = true;
      if (dragged) {
        c.camera.x += p.x - old.x;
        c.camera.y += p.y - old.y;
        constrain();
        draw();
      }
    };
    const finish = (e) => {
      if (!pointers.has(e.pointerId)) return;
      const p = point(e);
      pointers.delete(e.pointerId);
      if (!dragged && down) {
        const x = (p.x - c.ox) / c.scale,
          y = (p.y - c.oy) / c.scale;
        const sector = c.g.sectors.find(
          (s) =>
            Math.abs(x - s.center.x) <= 0.867 &&
            Math.abs(y - s.center.y) <=
              1 - Math.abs(x - s.center.x) / Math.sqrt(3),
        );
        if (sector) {
          selected = sector.number;
          card();
          draw();
        }
      }
      if (!pointers.size) {
        down = null;
        pinch = 0;
      }
    };
    c.canvas.onpointerup = finish;
    c.canvas.onpointercancel = () => {
      pointers.clear();
      down = null;
      dragged = true;
    };
  }
  function block(root, title) {
    const section = n("section", undefined, "planet-info-block");
    section.append(n("h4", tr(title)));
    root.append(section);
    return section;
  }
  const area = (value) =>
    UI.formatNumber(value, { maximumFractionDigits: 0 }) + " km²";
  function planetInfo(c) {
    const info = block(c.card, "Информация о планете");
    info.append(n("h3", c.data.name), objectTag(c.data.kind));
    UI.row(info, "Размер", tr(c.g.size));
    UI.row(info, "Площадь поверхности", area(c.g.area));
    UI.row(info, "Количество секторов", UI.formatNumber(c.g.sectors.length));
    UI.row(
      info,
      "Температура планеты",
      UI.formatNumber(c.g.temperature) + " °C",
    );
    info.append(
      n(
        "small",
        tr(
          "Средняя равновесная оценка; локальный климат пока не моделируется.",
        ),
        "muted",
      ),
    );
    const land =
      c.g.sectors.reduce((sum, s) => sum + s.land * s.area, 0) /
      c.g.sectors.reduce((sum, s) => sum + s.area, 0);
    UI.row(info, "Суша по секторам", UI.percent(land));
    UI.row(
      info,
      "Вода по секторам",
      UI.percent(100 - Math.round(land * 10) / 10),
    );
    info.append(
      n(
        "small",
        tr(
          "Средняя доля по площади секторов, а не отдельный климатический показатель.",
        ),
        "muted",
      ),
    );
    c.sectorCard = n("div", undefined, "planet-sector-details");
    c.card.append(c.sectorCard);
  }
  function card() {
    const c = current;
    if (!c?.g) return;
    const s = c.g.sectors.find((s) => s.number === selected) || c.g.sectors[0],
      container = c.sectorCard;
    container.replaceChildren(
      n("h3", tr("Выбранный сектор") + " · " + s.number),
    );
    const selectedInfo = block(container, "Природные условия");
    UI.row(selectedInfo, "Географический тип", tr(s.type));
    UI.row(
      selectedInfo,
      "Биом суши",
      tr(s.land > 0 ? s.biome : "Суша отсутствует"),
    );
    selectedInfo.append(
      n("small", tr("Локальный климат сектора пока не моделируется."), "muted"),
    );
    const geography = block(container, "География сектора"),
      land =
        typeof s.land === "number" && Number.isFinite(s.land)
          ? Math.max(0, Math.min(100, s.land))
          : NaN;
    UI.row(geography, "Площадь", area(s.area));
    UI.row(
      geography,
      "Суша",
      UI.percent(land) + " · " + area((s.area * land) / 100),
    );
    UI.row(
      geography,
      "Вода",
      UI.percent(100 - Math.round(land * 10) / 10) +
        " · " +
        area((s.area * (100 - land)) / 100),
    );
    const resources = block(container, "Природные ресурсы");
    const levels = {
      "Бедные залежи": 1,
      "Умеренные залежи": 2,
      "Богатые залежи": 3,
      "Очень богатые залежи": 4,
    };
    for (const [id, estimate] of Object.entries(s.estimates)) {
      const level = levels[estimate];
      UI.row(
        resources,
        asset(id),
        (level ? "▰".repeat(level) + "▱".repeat(4 - level) + " · " : "") +
          tr(estimate),
      );
    }
    if (!Object.keys(s.estimates).length)
      resources.append(n("p", tr("Предварительных данных нет"), "muted"));
    resources.append(
      n(
        "small",
        tr("Точные объёмы требуют будущей геологической разведки."),
        "muted",
      ),
    );
    const root = block(container, "Владение и колонизация");
    const claim = c.state.claims.find((p) => p.sector === s.number);
    UI.row(root, "Владелец", claim?.owner || tr("Свободная территория"));
    UI.row(
      root,
      "Статус",
      tr(claim ? statusName(claim.status) : "Свободная территория"),
    );
    if (claim?.status === "complete") {
      const panel = block(container, "Посадочная инфраструктура"),
        landing = c.state.landing,
        site = landing?.sites.find((site) => site.sector === s.number),
        territory = landing?.territories.find((t) =>
          t.sectors.includes(s.number),
        );
      if (site) {
        const image = n("img");
        image.src = "planet-landing-core.png";
        image.alt = tr("Посадочная площадка");
        image.className = "landing-site-preview";
        panel.append(image);
        UI.row(panel, "Владелец", site.owner);
        panel.append(
          n(
            "p",
            tr(site.reason || "Посадочная площадка подготовлена"),
            site.accessible ? "signal" : "muted",
          ),
        );
        panel.append(
          btn(
            "Высадиться",
            () => openVisit(site.id),
            !site.accessible,
            site.reason,
          ),
        );
      } else if (landing?.preparing) {
        panel.append(
          n("p", tr("Подготавливаем посадочную инфраструктуру"), "muted"),
        );
      } else if (territory?.sites.length) {
        panel.append(
          n("p", tr("Сектор обслуживается площадкой связанной территории")),
        );
        for (const id of territory.sites) {
          const target = landing.sites.find((site) => site.id === id);
          if (target)
            panel.append(
              btn(tr("Сектор") + " " + target.sector, () => {
                selected = target.sector;
                card();
                draw();
              }),
            );
        }
      } else {
        panel.append(
          n(
            "p",
            tr(territory?.reason || "В этом секторе нет посадочной площадки"),
            "muted",
          ),
        );
      }
      if (territory)
        UI.row(
          panel,
          "Секторов в территории",
          Format.quantity(territory.sectors.length),
        );
    }
    if (claim) {
      root.append(btn("Проект колонизации", () => project(claim.id)));
      return;
    }
    let reason = !c.state.habitable
      ? "Планета непригодна для колонизации"
      : s.land < c.state.settings.min_land
        ? "Недостаточная доля суши"
        : c.state.level < c.state.settings.min_level
          ? "Для колонизации недостаточно стажа"
          : "";
    root.append(
      n(
        "p",
        reason
          ? tr(reason) +
              (reason.includes("стажа")
                ? " · " + c.state.settings.min_level
                : reason.includes("суши")
                  ? " · " + UI.percent(c.state.settings.min_land)
                  : "")
          : tr("Сектор доступен для колонизации"),
        reason ? "muted" : "signal",
      ),
    );
    for (const corporate of [false, true]) {
      const blocked =
        reason ||
        (corporate
          ? !c.state.can_corporate
            ? "Недостаточно корпоративных полномочий"
            : ""
          : c.state.personal_count >= c.state.settings.personal_limit
            ? "Достигнут лимит личных секторов"
            : "");
      root.append(
        btn(
          corporate ? "Корпоративный проект" : "Личный проект",
          () =>
            confirm(
              "Создание колонии",
              c.data.name +
                " · " +
                tr("Сектор") +
                " " +
                s.number +
                " · " +
                tr(corporate ? "Корпоративный проект" : "Личный проект") +
                "\n" +
                Object.entries(c.state.settings.cost)
                  .map(([id, q]) => asset(id) + ": " + q)
                  .join(" · ") +
                "\n" +
                tr("Срок финансирования, дней") +
                ": " +
                c.state.settings.funding_days +
                " · " +
                tr("Длительность строительства, импульсов") +
                ": " +
                c.state.settings.build_ticks +
                " · " +
                tr("Возврат при отмене, %") +
                ": " +
                c.state.settings.refund_percent,
              {
                action: "create",
                surface: c.data.id,
                sector: s.number,
                corporate,
              },
              (r) => project(r.project),
            ),
          !!blocked,
          blocked,
        ),
      );
    }
  }
  async function update() {
    if (!current || polling) return;
    const c = current;
    polling = true;
    try {
      if (c.data.absent) {
        const fresh = await api(
          `planet/surface?star=${c.data.star}&planet=${c.data.planet}`,
        );
        if (current === c && !fresh.absent)
          await open(c.data.star, c.data.planet);
        return;
      }
      const state = await api("planet/state?surface=" + c.data.id);
      if (current !== c) return;
      c.state = state;
      if (!c.card.contains(document.activeElement)) card();
      draw();
    } catch (e) {
      if (current === c) {
        if (e.status === 404) {
          invalidateProjects(c.data.id);
          await open(c.data.star, c.data.planet);
        } else notify(e.message, "error");
      }
    } finally {
      polling = false;
    }
  }
  async function open(star, planet) {
    const request = ++epoch;
    Modules.open("galaxy");
    const data = await api(`planet/surface?star=${star}&planet=${planet}`);
    if (request !== epoch || !Session.signedIn) return;
    let state;
    if (!data.absent) {
      try {
        state = await api("planet/state?surface=" + data.id);
      } catch (e) {
        if (e.status === 404 && request === epoch) return open(star, planet);
        throw e;
      }
    }
    if (request !== epoch || !Session.signedIn) return;
    const root = document.querySelector("#window-galaxy .module-content");
    current?.resize?.disconnect();
    root.querySelector(".planet-ui")?.remove();
    root.classList.add("planet-open");
    const host = n("section", undefined, "planet-ui"),
      tools = n("div", undefined, "planet-toolbar"),
      layout = n("div", undefined, "planet-workspace"),
      wrap = n("div", undefined, "planet-canvas-wrap"),
      canvas = n("canvas"),
      side = n("aside", undefined, "planet-sector-card");
    canvas.id = "planet-map";
    canvas.setAttribute("aria-label", tr("Поверхность планеты"));
    canvas.tabIndex = 0;
    const c = {
      root,
      host,
      canvas,
      card: side,
      data,
      state,
      g: data.geography,
      camera: { zoom: 1, x: 0, y: 0 },
      terrain: data.absent ? null : terrain(data.geography, data.kind),
    };
    current = c;
    selected = 1;
    tools.append(
      btn("К карте системы", () => close()),
      n("strong", data.name),
      btn("Вместить карту", () => {
        c.camera = { zoom: 1, x: 0, y: 0 };
        draw();
      }),
      btn("Колонии", () => manager()),
    );
    if (data.absent) {
      const empty = n("div", undefined, "planet-surface-empty");
      empty.append(
        n("h3", tr("Генерация поверхности отсутствует")),
        n(
          "p",
          tr(
            "Поверхность удалена администратором. Астрономическая планета сохранена.",
          ),
        ),
      );
      if (Session.data.state.user.role === "admin")
        empty.append(
          btn("Перегенерировать колонию", () =>
            admin(star, planet, "regenerate"),
          ),
        );
      host.append(tools, empty);
      root.append(host);
      return;
    }
    c.textureStatus = n("small", undefined, "planet-texture-status");
    c.textureStatus.setAttribute("role", "status");
    c.textureRetry = btn("Повторить загрузку текстур", () => applyTextures(c));
    c.textureRetry.hidden = true;
    tools.append(c.textureStatus, c.textureRetry);
    const legend = n("div", undefined, "planet-legend");
    for (const [label, color] of [
      ["Свободная территория", "#b9cad0"],
      ["Недоступно", "#607588"],
      ["Финансирование", "#eac85e"],
      ["Строительство", "#eea45f"],
      ["Личная колония", "#7ad7a7"],
      ["Корпоративная колония", "#baa0ef"],
      ["Чужая колония", "#e09091"],
    ]) {
      const item = n("span", tr(label));
      item.style.borderLeft = "3px solid " + color;
      legend.append(item);
    }
    legend.append(
      n("span", "⇣ " + tr("Доступная посадочная площадка"), "signal"),
      n("span", "⇣ " + tr("Недоступная посадочная площадка"), "muted"),
    );
    wrap.append(canvas);
    layout.append(wrap, side);
    host.append(tools, layout, legend);
    root.append(host);
    controls(c);
    c.resize = new ResizeObserver(draw);
    c.resize.observe(wrap);
    planetInfo(c);
    card();
    requestAnimationFrame(draw);
    applyTextures(c);
  }
  function invalidateProjects(surface) {
    for (const [id, root] of projectRoots) {
      if (Number(root.dataset.surface) !== surface) continue;
      Modules.close("colony-project-" + id);
      root.replaceChildren();
      projectRoots.delete(id);
      contributionCache.delete(id);
    }
  }
  async function admin(star, planet, operation) {
    if (Session.data.state?.user.role !== "admin") return;
    const session = Session.epoch,
      info = await api(`admin/planet/surface?star=${star}&planet=${planet}`);
    if (
      session !== Session.epoch ||
      !Session.signedIn ||
      Session.data.state?.user.role !== "admin"
    )
      return;
    const title =
        operation === "delete"
          ? "Удалить генерацию колонии"
          : "Перегенерировать колонию",
      key = "surface-admin",
      root = Modules.create(key, "Управление поверхностью", 640, 720, {
        audience: "admin",
        transient: true,
      }),
      phrase = operation === "delete" ? "УДАЛИТЬ" : "ПЕРЕГЕНЕРИРОВАТЬ",
      request = Array.from(crypto.getRandomValues(new Uint32Array(4)), (v) =>
        v.toString(16),
      ).join("-");
    root.replaceChildren(n("h3", info.name), n("h4", tr(title)));
    for (const [label, key] of [
      ["Количество секторов", "sectors"],
      ["Колонии", "colonies"],
      ["Проекты", "projects"],
      ["Активные проекты", "active"],
      ["Строительство", "building"],
      ["Корпоративные проекты", "corporate"],
      ["Высадки", "visits"],
      ["Неполученные возвраты", "pending_refunds"],
    ])
      UI.row(root, label, UI.formatNumber(info[key]));
    root.append(
      n(
        "p",
        tr(
          "Будут безвозвратно удалены поверхность, сектора, проекты, владения и история колонизации этой планеты. Планета, корабли, аккаунты и корпорации сохранятся.",
        ),
        "surface-warning",
      ),
    );
    const invested = Object.entries(info.invested);
    for (const [id, value] of invested)
      UI.row(
        root,
        asset(id),
        UI.formatNumber(value, { maximumFractionDigits: 0 }),
      );
    const check = n("input"),
      label = n("label", undefined, "surface-forfeit");
    check.type = "checkbox";
    label.append(
      check,
      n(
        "span",
        tr(
          "Подтверждаю безвозвратное списание вкладов без возврата кредитов и ресурсов.",
        ),
      ),
    );
    if (invested.length) root.append(label);
    if (info.blocked) root.append(n("p", tr(info.blocked), "surface-warning"));
    const input = UI.field(
        root,
        "Введите строку подтверждения",
        "surface-confirm",
        "",
      ),
      hint = n("p", tr("Строка подтверждения") + ": " + phrase, "muted"),
      submit = btn(title, async () => {
        submit.disabled = true;
        cancel.disabled = true;
        input.disabled = true;
        check.disabled = true;
        try {
          const result = await api("admin/planet/surface", {
            action: operation,
            star,
            planet,
            token: info.token,
            confirm: input.value,
            forfeit: check.checked,
            request,
          });
          if (Session.epoch !== session || !Session.signedIn) return;
          invalidateProjects(result.old_surface);
          if (current?.data.star === star && current?.data.planet === planet)
            await open(star, planet);
          if (Modules.isOpen("colonies")) await manager(true);
          Modules.close(key);
          notify(
            tr(
              operation === "delete"
                ? "Генерация поверхности удалена"
                : "Поверхность перегенерирована",
            ),
            "success",
          );
        } catch (e) {
          if (Session.epoch !== session || !Session.signedIn) return;
          notify(e.message, "error");
          root.append(
            btn("Обновить предупреждение", () =>
              admin(star, planet, operation),
            ),
          );
        } finally {
          cancel.disabled = false;
          input.disabled = false;
          check.disabled = false;
          validate();
        }
      }),
      cancel = btn("Отмена", () => Modules.close(key));
    function validate() {
      submit.disabled =
        !!info.blocked ||
        input.value !== phrase ||
        (invested.length > 0 && !check.checked) ||
        (operation === "delete" && !info.surface);
    }
    input.oninput = validate;
    check.onchange = validate;
    validate();
    root.append(hint, submit, cancel);
    Modules.open(key);
  }
  const projectRoots = new Map(),
    contributionCache = new Map();
  async function project(
    id,
    before = 0,
    background = false,
    contributionsBefore = 0,
  ) {
    const epochAt = epoch;
    let data;
    try {
      data = await api(
        "planet/project?id=" +
          id +
          (before ? "&before=" + before : "") +
          (contributionsBefore
            ? "&contributions_before=" + contributionsBefore
            : ""),
      );
    } catch (e) {
      if (e.status === 404) {
        Modules.close("colony-project-" + id);
        projectRoots.get(id)?.replaceChildren();
        projectRoots.delete(id);
        contributionCache.delete(id);
        if (!background) notify(e.message, "warning");
        return;
      }
      throw e;
    }
    if (
      !Session.signedIn ||
      epochAt !== epoch ||
      (background && !Modules.isOpen("colony-project-" + id))
    )
      return;
    const p = data.project,
      key = "colony-project-" + id,
      root = Modules.create(key, "Проект колонизации", 760, 750);
    projectRoots.set(id, root);
    root.dataset.surface = String(p.surface);
    if (!data.can_contribute && Session.data.state.user.role !== "admin") {
      const cached = contributionCache.get(id);
      if (cached) {
        for (const [key, item] of cached.items)
          if (item.actor !== Session.data.state.user.id)
            cached.items.delete(key);
        if (cached.events)
          for (const [key, item] of cached.events)
            if (item.actor !== Session.data.state.user.id)
              cached.events.delete(key);
      }
    }
    if (contributionsBefore) {
      const cache = contributionCache.get(id) || { items: new Map() };
      data.contributions.forEach((item) => cache.items.set(item.id, item));
      cache.more = data.contributions.length === 50;
      contributionCache.set(id, cache);
      const host = root.querySelector(".colony-contributions");
      if (host) {
        host.replaceChildren();
        appendContributions(
          host,
          [...cache.items.values()].sort((a, b) => b.id - a.id),
          p,
          id,
          cache.more,
        );
      }
      return;
    }
    if (before) {
      const history = root.querySelector(".colony-history");
      appendEvents(history, data.events, id, true);
      return;
    }
    if (
      root.contains(document.activeElement) &&
      document.activeElement.matches("input,select")
    )
      return;
    const scroll = root.scrollTop;
    root.replaceChildren(
      n("h3", data.name + " · " + tr("Сектор") + " " + p.sector),
    );
    UI.row(root, "Владелец", data.owner);
    UI.row(root, "Статус", tr(statusName(p.status)));
    UI.row(root, "Инициатор", data.initiator_name || String(p.initiator));
    if (p.status === "funding") {
      const remaining = Math.max(
        0,
        (new Date(p.expires_at) - new Date(data.server_time)) / 3600000,
      );
      root.append(
        n(
          "p",
          tr("Осталось на финансирование, часов") +
            ": " +
            Format.number(remaining),
        ),
        n(
          "p",
          tr(
            "Материалы передаются с личного склада станции при стыковке в системе планеты. Кредиты списываются с личного счёта.",
          ),
          "muted",
        ),
      );
    }
    if (p.status === "funding")
      root.append(
        n(
          "p",
          tr(
            "Полное финансирование автоматически начинает строительство. После этого возврат вкладов и отмена недоступны.",
          ),
          "muted",
        ),
      );
    const rows = Object.entries(p.required).map(([id, required]) => [
      asset(id),
      String(required),
      p.received ? String(p.received[id] || 0) : "—",
      p.received ? String(required - (p.received[id] || 0)) : "—",
    ]);
    root.append(
      UI.table(["Материал", "Требуется", "Внесено", "Осталось"], rows),
    );
    if (p.status === "building" || p.status === "complete")
      root.append(
        UI.bar(p.progress, p.build_ticks, "Импульсы строительства"),
        n("p", tr("Осталось импульсов") + ": " + (p.build_ticks - p.progress)),
      );
    if (p.status === "funding" && data.can_contribute) {
      const form = n("form"),
        resource = n("select");
      for (const [id, q] of Object.entries(p.required)) {
        if (q <= (p.received[id] || 0)) continue;
        const option = n("option", asset(id));
        option.value = id;
        resource.append(option);
      }
      const label = n("label");
      label.append(n("span", tr("Материал")), resource);
      form.append(label);
      const amount = UI.field(
        form,
        "Количество",
        "colony-amount-" + id,
        1,
        "number",
      );
      amount.min = 1;
      amount.step = 1;
      form.append(
        btn("Внести вклад", () =>
          confirm(
            "Подтверждение взноса",
            asset(resource.value) + " × " + amount.value,
            {
              action: "contribute",
              id,
              resource: resource.value,
              amount: Number(amount.value),
            },
            () => project(id),
          ),
        ),
      );
      form.onsubmit = (e) => e.preventDefault();
      root.append(form);
    }
    const tools = n("div", undefined, "inline-controls");
    if (p.status === "funding" && data.can_manage)
      tools.append(
        btn("Отменить проект", () =>
          confirm(
            "Отмена проекта",
            tr("При отмене возвращается") +
              " " +
              UI.percent(p.refund_percent) +
              " · " +
              tr(
                "Ядро возвращается полностью. Возвраты направляются вкладчикам на исходные личные склады и счета.",
              ),
            { action: "cancel", id },
            () => project(id),
          ),
        ),
      );
    if (p.status === "funding" && Session.data.state.user.role === "admin")
      tools.append(
        btn("Пополнить ресурсы", () =>
          confirm(
            "Административное пополнение",
            tr("Выдать недостающие материалы и начать обычное строительство?"),
            { action: "admin_fill", id },
            () => project(id),
          ),
        ),
      );
    if (p.status === "complete")
      tools.append(
        btn(
          "Выбрать площадку",
          () =>
            open(data.star, data.planet).then(() => {
              selected = p.sector;
              card();
              draw();
            }),
          !!data.land_reason,
          data.land_reason,
        ),
      );
    tools.append(
      btn("На поверхности", () =>
        open(data.star, data.planet).then(() => {
          selected = p.sector;
          card();
          draw();
        }),
      ),
    );
    root.append(tools, n("h4", tr("История взносов")));
    const contributionsRoot = n("div", undefined, "colony-contributions");
    const cache = contributionCache.get(id) || {
      items: new Map(),
      more: data.contributions.length === 50,
    };
    data.contributions.forEach((item) => cache.items.set(item.id, item));
    contributionCache.set(id, cache);
    appendContributions(
      contributionsRoot,
      [...cache.items.values()].sort((a, b) => b.id - a.id),
      p,
      id,
      cache.more,
    );
    root.append(contributionsRoot);
    const history = n("div", undefined, "colony-history");
    appendEvents(history, data.events, id);
    root.append(history);
    root.scrollTop = scroll;
    if (!background) Modules.open(key);
  }
  function appendContributions(root, items, p, id, more) {
    for (const item of items) {
      const returned =
        item.returned || ["cancelled", "expired"].includes(p.status);
      const row = n("div", undefined, "colony-contribution");
      row.append(
        n(
          "span",
          item.username +
            " · " +
            asset(item.resource) +
            " × " +
            item.amount +
            " · " +
            Format.timestamp(item.created_at) +
            (item.admin_grant
              ? " · " + tr("Выдача администратора")
              : returned
                ? " · " + tr("Возвращён")
                : ""),
        ),
      );
      if (
        p.status === "funding" &&
        item.actor === Session.data.state.user.id &&
        !item.admin_grant &&
        !returned
      )
        row.append(
          btn("Вернуть вклад", () =>
            confirm(
              "Возврат вклада",
              asset(item.resource) + " × " + item.amount + " · 100%",
              { action: "withdraw", id, contribution: item.id },
              () => project(id),
            ),
          ),
        );
      root.append(row);
    }
    if (more && items.length)
      root.append(
        btn("Ранее", () => project(id, 0, false, items[items.length - 1].id)),
      );
  }
  function appendEvents(root, events, id, older = false) {
    const cache = contributionCache.get(id) || {
      items: new Map(),
      more: false,
    };
    cache.events ||= new Map();
    if (cache.eventMore === undefined || older)
      cache.eventMore = events.length === 30;
    events.forEach((e) => cache.events.set(e.id, e));
    contributionCache.set(id, cache);
    const all = [...cache.events.values()].sort((a, b) => b.id - a.id);
    root.replaceChildren();
    for (const e of all)
      root.append(
        n(
          "p",
          Format.timestamp(e.created_at) +
            " · " +
            tr(
              {
                create: "Проект создан",
                contribute: "Взнос",
                refund: "Возврат вклада",
                building: "Строительство",
                complete: "Колонизация завершена",
                cancelled: "Отменён",
                expired: "Просрочен",
                admin_fill: "Выдача администратора",
              }[e.kind] || e.kind,
            ) +
            " · " +
            (e.username || tr("Система")) +
            " · " +
            (e.data?.resource
              ? asset(e.data.resource) + " × " + e.data.amount
              : e.kind === "admin_fill"
                ? Object.entries(e.data || {})
                    .map(([id, q]) => asset(id) + " × " + q)
                    .join(" · ")
                : ""),
          "muted",
        ),
      );
    if (cache.eventMore && all.length) {
      const more = btn("Ранее", () => project(id, all[all.length - 1].id));
      more.classList.add("colony-more");
      root.append(more);
    }
  }
  let colonyRefresh = 0;
  async function manager(background = false) {
    const session = Session.epoch, ticket = ++colonyRefresh;
    let data;
    try {
      data = await api("planet/projects");
      if (!world.data) await loadWorld().catch(() => null);
    } catch (error) {
      if (Session.signedIn && session === Session.epoch && (!background || Modules.isOpen("colonies")))
        notify(error.message, "error");
      return;
    }
    if (!Session.signedIn || session !== Session.epoch || ticket !== colonyRefresh ||
        (background && !Modules.isOpen("colonies"))) return;
    const root = Modules.create("colonies", "Колонии", 950, 700);
    mountColonyCatalog(root, data, {
      language: () => I18n.language,
      assetBase: "",
      revision: JSON.stringify([Session.data.workbench?.universe_revision,
        Session.data.workbench?.known, Session.data.workbench?.research]),
      starName: id => world.stars.get(Number(id))?.name || "",
      translateKind: kind => tr(kind),
      loadSystem: id => api("system?id=" + encodeURIComponent(id)),
      resourceName: id => asset(id),
      onProject: id => project(id).catch(error => {
        if (Session.signedIn && session === Session.epoch) notify(error.message, "error");
      }),
      onSurface: (star, planet) => open(star, planet).catch(error => {
        if (Session.signedIn && session === Session.epoch) notify(error.message, "error");
      }),
      onVisit: () => openVisit().catch(error => {
        if (Session.signedIn && session === Session.epoch) notify(error.message, "error");
      }),
      onRefund: refund => send({action:"claim_refund",id:refund.id}).then(result => {
        if (result) manager();
      })
    });
    if (!background) Modules.open("colonies");
  }
  async function settings(root) {
    const session = Session.epoch;
    const current = () =>
      Session.signedIn &&
      session === Session.epoch &&
      root.isConnected &&
      Session.data.state.user.role === "admin";
    const data = await api("admin/planet/settings"),
      form = n("form"),
      fields = {};
    if (!current()) return false;
    form.onsubmit = (e) => e.preventDefault();
    form.append(
      n("h3", tr("Планетарная колонизация")),
      n(
        "p",
        tr(
          "Изменения применяются к новым проектам и ещё не созданным поверхностям.",
        ),
        "muted",
      ),
    );
    for (const [key, label] of [
      ["min_level", "Минимальный стаж колонизации"],
      ["personal_limit", "Лимит личных секторов"],
      ["min_land", "Минимальная доля суши, %"],
      ["funding_days", "Срок финансирования, дней"],
      ["build_ticks", "Длительность строительства, импульсов"],
      ["refund_percent", "Возврат при отмене, %"],
    ])
      fields[key] = UI.field(
        form,
        label,
        "planet-setting-" + key,
        data[key],
        "number",
      );
    const counts = data.counts.map((value, i) =>
      UI.field(
        form,
        ["Маленькая", "Средняя", "Большая", "Гигантская"][i],
        "planet-count-" + i,
        value,
        "number",
      ),
    );
    const costs = {};
    for (const [id, value] of Object.entries(data.cost))
      costs[id] = UI.field(
        form,
        asset(id),
        "planet-cost-" + id,
        value,
        "number",
      );
    form.append(
      btn("Сохранить настройки колонизации", async () => {
        if (!current() || !form.reportValidity()) return;
        try {
          await api("admin/planet/settings", {
            ...Object.fromEntries(
              Object.entries(fields).map(([k, v]) => [k, Number(v.value)]),
            ),
            counts: counts.map((v) => Number(v.value)),
            cost: Object.fromEntries(
              Object.entries(costs).map(([k, v]) => [k, Number(v.value)]),
            ),
          });
          if (current()) notify(tr("Действие выполнено"));
        } catch (e) {
          if (current()) notify(e.message, "error");
        }
      }),
    );
    root.append(form);
    return true;
  }
  scenes.planet = { open, project, manager, settings, admin, dismiss: close };
  window.addEventListener("moduleresize", (e) => {
    if (e.detail === "galaxy") draw();
  });
  window.addEventListener("gamedata", () => {
    if (Modules.isOpen("colonies")) manager(true).catch(() => {});
    if (current && Modules.isOpen("galaxy")) update();
    for (const [id, root] of projectRoots) {
      if (
        Modules.isOpen("colony-project-" + id) &&
        !root.contains(document.activeElement)
      )
        project(id, 0, true).catch(() => {});
    }
  });
  window.addEventListener("languagechange", () => {
    if (Modules.isOpen("colonies")) manager(true).catch(() => {});
    if (current && Modules.isOpen("galaxy")) {
      const { data } = current;
      open(data.star, data.planet);
    }
    for (const [id] of projectRoots) {
      if (Modules.isOpen("colony-project-" + id))
        project(id, 0, true).catch(() => {});
    }
  });
  window.addEventListener("signedout", () => {
    colonyRefresh++;
    const colonyRoot = document.querySelector("#window-colonies .module-content");
    if (colonyRoot) clearColonyCatalog(colonyRoot);
    epoch++;
    close(false);
    actions.clear();
    projectRoots.clear();
    contributionCache.clear();
    document
      .querySelectorAll('[id^="window-colony"],#window-colonies')
      .forEach((w) => {
        w.close();
        w.querySelector(".module-content")?.replaceChildren();
      });
  });
}
