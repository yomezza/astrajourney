import { acquireArt } from "./scene-art.js";
import { ART_PATHS, PACK_REVISION } from "./art-pack.js";
import { I18n } from "../../core/i18n.js";
import { uuid } from "../../core/uuid.js";

const tr = (s) => I18n.t(s),
  TILE = 4,
  REACH = 32;
const messages = {
  build_invalid_place:
    "Здесь нельзя строить: вода, препятствие или защищённый проход.",
  build_occupied: "Эта клетка уже занята.",
  build_player_here: "Здесь находится персонаж. Выберите свободное место.",
  build_too_far: "Слишком далеко. Подойдите ближе к месту строительства.",
  build_access_denied:
    "Недостаточно прав: строить может владелец территории или участник с правом колонизации.",
  build_limit: "В секторе достигнут лимит: 256 объектов.",
  build_rate_limited: "Подождите немного перед следующей установкой.",
};
export function buildingKeys(data) {
  for (const d of data?.catalog || [])
    ART_PATHS["build_" + d.id] =
      `/planet-pixelart-v1/${d.asset}?v=${PACK_REVISION}`;
  return (data?.catalog || []).map((d) => "build_" + d.id);
}
export function drawBuilding(ctx, art, o, definition, alpha = 1) {
  const image = art.get("build_" + o.kind);
  if (!image || !definition) return;
  ctx.save();
  ctx.globalAlpha *= alpha;
  ctx.imageSmoothingEnabled = false;
  ctx.translate(o.x * TILE + TILE / 2, o.y * TILE + TILE / 2);
  ctx.rotate((o.rotation * Math.PI) / 2);
  // Pack pieces are tile-sized. Rotation is about the centre of their footprint.
  const height = (TILE * image.height) / image.width;
  ctx.drawImage(image, -TILE / 2, -height / 2, TILE, height);
  ctx.restore();
}
export function createBuildingMode(
  canvas,
  data,
  art,
  { button, current, position, people, send, reset },
) {
  let snapshot = data?.snapshot || { version: 0, objects: [] },
    selected = data?.catalog?.[0],
    rotation = 0,
    active = false,
    remove = false,
    pointer = null,
    pending = false,
    stopped = false,
    showGrid = true;
  const definitions = new Map((data?.catalog || []).map((d) => [d.id, d]));
  const objects = () => snapshot.objects || [];
  const cells = data?.cells
    ? Uint8Array.from(atob(data.cells), (c) => c.charCodeAt(0))
    : null;
  const node = (tag, text, cls) => {
    const e = document.createElement(tag);
    if (text) e.textContent = tr(text);
    if (cls) e.className = cls;
    return e;
  };
  const makeButton = (text, action, cls) => {
    const b = node("button", text, cls);
    b.type = "button";
    b.onclick = action;
    return b;
  };
  const panel = node("section", null, "planet-build-panel");
  panel.id = "planet-build-panel";
  panel.hidden = true;
  panel.setAttribute("aria-label", tr("Строительство"));
  const title = node("div", null, "planet-build-heading"),
    titleText = node("div"),
    close = makeButton(
      "Закрыть строительство",
      () => toggle(false),
      "planet-build-close",
    );
  titleText.append(
    node("h2", "Строительство"),
    node("span", "Всё бесплатно", "planet-build-free"),
  );
  title.append(titleText, close);
  const search = node("input");
  search.type = "search";
  search.placeholder = tr("Найти объект…");
  search.setAttribute("aria-label", tr("Найти объект"));
  const tabs = node("div", null, "planet-build-tabs");
  tabs.setAttribute("aria-label", tr("Категории строительства"));
  const list = node("div", null, "planet-build-catalog"),
    info = node("div", null, "planet-build-selection"),
    feedback = node("p", null, "planet-build-feedback");
  feedback.setAttribute("role", "status");
  const rotateRow = node("div", null, "planet-build-rotation"),
    angle = node("strong", "0°");
  rotateRow.append(
    makeButton("↶", () => rotate(-1)),
    angle,
    makeButton("↷", () => rotate(1)),
    node("span", "R — повернуть"),
  );
  const actions = node("div", null, "planet-build-actions"),
    erase = makeButton("Убрать своё", () => {
      remove = !remove;
      erase.setAttribute("aria-pressed", String(remove));
      panel.dataset.tool = remove ? "remove" : "place";
      refreshInfo();
    });
  erase.setAttribute("aria-pressed", "false");
  const gridLabel = node("label", null, "planet-build-grid-toggle"),
    grid = node("input");
  grid.type = "checkbox";
  grid.checked = true;
  grid.onchange = () => (showGrid = grid.checked);
  gridLabel.append(grid, node("span", "Сетка"));
  actions.append(erase, gridLabel);
  panel.append(
    title,
    node(
      "p",
      "Создавайте объекты и обустраивайте сектор.",
      "planet-build-intro",
    ),
    search,
    tabs,
    list,
    info,
    rotateRow,
    actions,
    feedback,
    node(
      "p",
      "ЛКМ — поставить · ПКМ — обзор · R — поворот · Esc — выйти",
      "planet-build-help",
    ),
  );
  canvas.closest(".visit-viewport")?.append(panel);
  let category = data?.catalog?.[0]?.group || "";
  for (const group of new Set((data?.catalog || []).map((d) => d.group))) {
    const tab = makeButton(group, () => {
      category = group;
      search.value = "";
      renderCatalog();
    });
    tab.dataset.category = group;
    tabs.append(tab);
  }
  function refreshInfo() {
    info.replaceChildren(
      node("strong", remove ? "Убрать свою постройку" : selected?.title),
      node(
        "span",
        remove
          ? "Наведите на свой объект и нажмите ЛКМ."
          : selected?.solid
            ? "Препятствие · 1 клетка"
            : "Проходимый объект · 1 клетка",
      ),
    );
    angle.textContent = rotation * 90 + "°";
    rotateRow.hidden = remove;
  }
  function renderCatalog() {
    for (const tab of tabs.children)
      tab.setAttribute(
        "aria-pressed",
        String(tab.dataset.category === category),
      );
    list.replaceChildren();
    const filter = search.value.trim().toLocaleLowerCase();
    for (const d of data?.catalog || []) {
      if (
        filter
          ? !tr(d.title).toLocaleLowerCase().includes(filter)
          : d.group !== category
      )
        continue;
      const card = makeButton(
        null,
        () => {
          selected = d;
          remove = false;
          erase.setAttribute("aria-pressed", "false");
          panel.dataset.tool = "place";
          renderCatalog();
          refreshInfo();
        },
        "planet-build-card",
      );
      card.dataset.kind = d.id;
      card.setAttribute("aria-label", tr(d.title));
      card.setAttribute(
        "aria-pressed",
        String(selected?.id === d.id && !remove),
      );
      const image = node("img");
      image.src = ART_PATHS["build_" + d.id];
      image.alt = "";
      image.draggable = false;
      card.append(image, node("span", d.title));
      list.append(card);
    }
  }
  search.oninput = renderCatalog;
  function rotate(step) {
    if (!active || remove) return;
    rotation = (rotation + step + 4) % 4;
    panel.dataset.rotation = String(rotation);
    refreshInfo();
  }
  function toggle(value) {
    if (stopped) return;
    active = value;
    pointer = null;
    pending = false;
    panel.hidden = !active;
    button?.setAttribute("aria-pressed", String(active));
    if (active) {
      reset();
      send("steer", { dx: 0, dy: 0 });
      feedback.textContent = tr(
        data?.allowed
          ? "Выберите объект и место на поверхности."
          : messages.build_access_denied,
      );
    }
    canvas.focus({ preventScroll: true });
  }
  if (button) {
    button.disabled = !data;
    button.onclick = () => toggle(!active);
    button.setAttribute("aria-pressed", "false");
  }
  function key(event) {
    if (
      !active ||
      !current() ||
      event.repeat ||
      event.ctrlKey ||
      event.altKey ||
      event.metaKey
    )
      return;
    if (
      event.target?.closest?.(
        'input,textarea,select,[contenteditable]:not([contenteditable="false"]),[role="textbox"],#window-chat',
      )
    )
      return;
    if (event.code === "KeyR") {
      event.preventDefault();
      rotate(event.shiftKey ? -1 : 1);
    }
    if (event.code === "Escape") {
      event.preventDefault();
      toggle(false);
    }
  }
  window.addEventListener("keydown", key, true);
  function target(p, commit = false) {
    return {
      id: commit ? uuid() : null,
      kind: selected?.id,
      x: Math.floor(p.x / TILE),
      y: Math.floor(p.y / TILE),
      rotation,
    };
  }
  function at(p) {
    return [...objects()]
      .reverse()
      .find(
        (o) =>
          o.x === Math.floor(p.x / TILE) &&
          o.y === Math.floor(p.y / TILE) &&
          o.mine,
      );
  }
  function valid(o) {
    if (
      !data?.allowed ||
      !selected ||
      !cells ||
      o.x < 0 ||
      o.y < 0 ||
      o.x >= 64 ||
      o.y >= 64
    )
      return "build_access_denied";
    const p = position();
    if (Math.hypot(o.x * TILE + 2 - p.x, o.y * TILE + 2 - p.y) > REACH)
      return "build_too_far";
    for (let y = o.y * TILE - 1; y < (o.y + 1) * TILE + 1; y++)
      for (let x = o.x * TILE - 1; x < (o.x + 1) * TILE + 1; x++)
        if (x < 0 || y < 0 || x >= 256 || y >= 256 || !cells[y * 256 + x])
          return "build_invalid_place";
    if (
      objects().some(
        (v) =>
          v.x === o.x &&
          v.y === o.y &&
          definitions.get(v.kind)?.ground === selected.ground,
      )
    )
      return "build_occupied";
    if (
      selected.solid &&
      people().some(
        (p) =>
          p.x >= o.x * TILE - 1 &&
          p.x < (o.x + 1) * TILE + 1 &&
          p.y >= o.y * TILE - 1 &&
          p.y < (o.y + 1) * TILE + 1,
      )
    )
      return "build_player_here";
    return null;
  }
  refreshInfo();
  renderCatalog();
  return {
    get active() {
      return active;
    },
    update(value) {
      if (value && value.version >= snapshot.version) {
        snapshot = value;
        canvas.dataset.buildCount = String(objects().length);
      }
    },
    result(result) {
      pending = false;
      feedback.textContent = tr(
        result.ok
          ? remove
            ? "Объект убран."
            : "Готово! Можно поставить следующий объект."
          : messages[result.code] ||
              "Не удалось сохранить объект. Повторите после проверки связи.",
      );
      feedback.dataset.error = String(!result.ok);
    },
    pointer(p, click = false) {
      if (!active) return false;
      pointer = p;
      if (!click) return true;
      if (pending) return true;
      if (!data.allowed) {
        feedback.textContent = tr(messages.build_access_denied);
        return true;
      }
      const o = remove ? at(p) : target(p, true),
        error = remove ? (!o ? "build_invalid_place" : null) : valid(o);
      if (error) {
        feedback.textContent = tr(
          messages[error] || messages.build_invalid_place,
        );
        return true;
      }
      pending = !!send("build", {
        action: remove ? "remove" : "place",
        object: o,
      });
      feedback.textContent = tr(pending ? "Сохраняем…" : "Нет соединения.");
      return true;
    },
    leave() {
      pointer = null;
    },
    ground(ctx) {
      for (const o of objects()) {
        const d = definitions.get(o.kind);
        if (d?.ground) drawBuilding(ctx, art, o, d);
      }
    },
    placed() {
      return objects()
        .filter((o) => !definitions.get(o.kind)?.ground)
        .map((o) => ({
          x: o.x * TILE + 2,
          y: o.y * TILE + 2,
          building: o,
          definition: definitions.get(o.kind),
        }));
    },
    draw(ctx, view) {
      if (!active) return;
      ctx.save();
      ctx.lineWidth = 1 / view.zoom;
      if (showGrid && view.zoom >= 3) {
        ctx.strokeStyle = "#8ce4db38";
        ctx.beginPath();
        for (
          let x = Math.max(0, Math.floor(view.x / TILE) * TILE);
          x < Math.min(256, view.x + view.worldW);
          x += TILE
        ) {
          ctx.moveTo(x, Math.max(0, view.y));
          ctx.lineTo(x, Math.min(256, view.y + view.worldH));
        }
        for (
          let y = Math.max(0, Math.floor(view.y / TILE) * TILE);
          y < Math.min(256, view.y + view.worldH);
          y += TILE
        ) {
          ctx.moveTo(Math.max(0, view.x), y);
          ctx.lineTo(Math.min(256, view.x + view.worldW), y);
        }
        ctx.stroke();
      }
      if (pointer) {
        const o = remove ? at(pointer) : target(pointer),
          error = remove ? !o : valid(o),
          x = Math.floor(pointer.x / TILE) * TILE,
          y = Math.floor(pointer.y / TILE) * TILE;
        ctx.fillStyle = error
          ? "#ff6b744a"
          : remove
            ? "#ffc26855"
            : "#67eac355";
        ctx.strokeStyle = error ? "#ff6b74" : remove ? "#ffc268" : "#67eac3";
        ctx.fillRect(x, y, TILE, TILE);
        ctx.strokeRect(x, y, TILE, TILE);
        if (!remove && o) drawBuilding(ctx, art, o, selected, 0.6);
        canvas.dataset.buildValid = String(!error);
        canvas.dataset.buildX = String(x / TILE);
        canvas.dataset.buildY = String(y / TILE);
      }
      ctx.restore();
    },
    stop() {
      stopped = true;
      active = false;
      panel.remove();
      window.removeEventListener("keydown", key, true);
      if (button) {
        button.onclick = null;
        button.disabled = true;
        button.setAttribute("aria-pressed", "false");
      }
    },
  };
}
