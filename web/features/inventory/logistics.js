import { resourceTile } from "./resource-tile.js";
import { shipName } from "../fleet/presentation.js";
import { stationLabel } from "../stations/index.js";
import { showResource } from "../objects/cards.js";
import { Format } from "../../core/format.js";
import { loadWorld } from "../../core/world-store.js";
import { Session } from "../../core/session.js";
import { I18n } from "../../core/i18n.js";
import { api } from "../../core/api.js";
import { refresh as refreshSnapshot } from "../../core/snapshot-store.js";

import { notify } from "../../ui/feedback.js";
import { UI, Modules } from "../../ui/runtime.js";
import { Logistics } from "../runtime.js";
import { world } from "../../core/world-store.js";

export function initLogistics() {
  const n = UI.node,
    tr = (s) => I18n.t(s),
    b = UI.button;
  const root = Modules.create("logistics", "Логистика", 1120, 720);
  root.classList.add("logistics-content");
  let data = null,
    request = 0,
    open = new Set(),
    query = "",
    scope = "all",
    drag = null,
    suppressClick = 0,
    pending = false,
    formOpen = false,
    operation = 0;
  const uuid = () =>
    [...crypto.getRandomValues(new Uint32Array(4))]
      .map((v) => v.toString(16).padStart(8, "0"))
      .join("");
  const resources = () => Session.data.expedition?.resources || [],
    resource = (id) => resources().find((r) => r.id === id),
    ports = () => Session.data.workbench?.stations || [];
  const key = (p) => p.star + "|" + (p.station || p.object_id),
    stock = (kind, p) =>
      ((kind === "company" ? data?.corporate : data?.personal) || []).find(
        (s) => key(s) === key(p),
      )?.cargo || {};
  const built = (p) => (data?.built || []).some((s) => key(s) === key(p));
  const ship = () => Session.data.expedition?.ship;
  const shipAt = (s, p) =>
    s &&
    !s.destination &&
    !s.local_target &&
    s.position_mode === "docked" &&
    s.star === p.star &&
    s.object_id === (p.station || p.object_id);
  const same = (a, b) =>
    a.kind === b.kind &&
    (a.kind === "ship"
      ? a.ship === b.ship
      : a.star === b.star &&
        a.station === b.station &&
        (a.kind !== "company" || a.company === b.company));
  function label(loc) {
    if (loc.kind === "ship") {
      const s = Session.data.fleet.ships.find((s) => s.id === loc.ship);
      return tr("Трюм") + " · " + (s ? shipName(s) : "");
    }
    const p = ports().find((p) => key(p) === loc.star + "|" + loc.station);
    return (
      (p?.name || world.data?.stars[loc.star - 1]?.name || tr("Станция")) +
      " · " +
      tr(loc.kind === "company" ? "Корпоративный склад" : "Личный склад")
    );
  }
  let selectionMode = false,
    selectionFrom = null;
  const selectedResources = new Set();
  function availableCargo(from) {
    if (from?.kind === "ship")
      return (
        Session.data.fleet?.ships.find((s) => s.id === from.ship)?.cargo || {}
      );
    return from ? stock(from.kind, from) : {};
  }
  function selectionItems() {
    const cargo = availableCargo(selectionFrom);
    return [...selectedResources]
      .filter((id) => cargo[id] > 0)
      .map((id) => ({ resource: id, quantity: cargo[id] }));
  }
  function paintSelection() {
    if (selectionFrom) {
      const cargo = availableCargo(selectionFrom);
      for (const id of selectedResources)
        if (!(cargo[id] > 0)) selectedResources.delete(id);
      if (!selectedResources.size) selectionFrom = null;
    }

    document.querySelectorAll("[data-cargo-resource]").forEach((tile) => {
      const active =
        selectionFrom &&
        tile.dataset.cargoLocation === JSON.stringify(selectionFrom) &&
        selectedResources.has(tile.dataset.cargoResource);
      tile.classList.toggle("cargo-selected", !!active);
      tile.setAttribute("aria-pressed", String(!!active));
      tile.classList.toggle(
        "cargo-select-mode",
        selectionMode && tile.dataset.cargoSelectable === "true",
      );
    });
    document
      .querySelectorAll(".cargo-selection-tools")
      .forEach((bar) => bar.updateSelection?.());
  }
  function clearSelection() {
    selectionFrom = null;
    selectedResources.clear();
    paintSelection();
  }
  function select(from, id) {
    if (!selectionFrom || !same(selectionFrom, from)) {
      selectedResources.clear();
      selectionFrom = from;
    }
    if (selectedResources.has(id)) selectedResources.delete(id);
    else selectedResources.add(id);
    if (!selectedResources.size) selectionFrom = null;
    paintSelection();
  }
  function selectionToolbar(holder) {
    const bar = n("div", undefined, "cargo-selection-tools"),
      toggle = b("Выделить ресурсы", () => {
        selectionMode = !selectionMode;
        paintSelection();
      }),
      count = n("span"),
      move = b("Переместить выбранное", () =>
        chooseDestination(selectionFrom, selectionItems()),
      ),
      clear = b("Снять выделение", clearSelection);
    toggle.dataset.help =
      "Ctrl/⌘ + щелчок добавляет ресурс к выделению. Для переноса выберите ресурсы из одного трюма или склада.";
    const update = () => {
      if (!bar.isConnected) return;
      const amount = selectionItems().length;
      toggle.setAttribute("aria-pressed", String(selectionMode));
      count.textContent = amount ? tr("Выбрано") + ": " + amount : "";
      move.hidden = clear.hidden = !amount;
    };
    bar.updateSelection = update;
    bar.append(toggle, count, move, clear);
    holder.append(bar);
    queueMicrotask(update);
    return bar;
  }
  function cleanDrag() {
    drag = null;
    document.body.classList.remove("cargo-dragging");
    document
      .querySelectorAll(".cargo-drop-ready")
      .forEach((e) => e.classList.remove("cargo-drop-ready"));
    suppressClick = performance.now() + 200;
  }
  const apiError = (e) => notify(e.message, "error");
  async function refresh() {
    if (!Session.signedIn || !Modules.isOpen("logistics")) return;
    const id = ++request,
      epoch = Session.epoch;
    const current = () =>
      id === request &&
      epoch === Session.epoch &&
      Session.signedIn &&
      Modules.isOpen("logistics");
    try {
      const result = await api("logistics");
      if (
        id !== request ||
        epoch !== Session.epoch ||
        !Session.signedIn ||
        !Modules.isOpen("logistics")
      )
        return;
      data = result;
      paintSelection();
      if (!drag && !formOpen) draw();
    } catch (error) {
      if (current() && error.name !== "AbortError") apiError(error);
    }
  }
  function local(from, to) {
    const fromShip =
        from.kind === "ship"
          ? Session.data.fleet.ships.find((s) => s.id === from.ship)
          : null,
      toShip =
        to.kind === "ship"
          ? Session.data.fleet.ships.find((s) => s.id === to.ship)
          : null,
      carrier = fromShip || toShip || ship(),
      a = fromShip
        ? { star: fromShip.star, station: fromShip.object_id }
        : from,
      c = toShip ? { star: toShip.star, station: toShip.object_id } : to;
    return (
      carrier &&
      shipAt(carrier, a) &&
      a.star === c.star &&
      a.station === c.station &&
      (!fromShip || shipAt(fromShip, c)) &&
      (!toShip || shipAt(toShip, a))
    );
  }
  function transfer(from, to, id, q) {
    if (!from || same(from, to) || pending) return;
    const items = Array.isArray(id) ? id : [{ resource: id, quantity: q }],
      carrier =
        from.kind === "ship"
          ? Session.data.fleet.ships.find((s) => s.id === from.ship)
          : to.kind === "ship"
            ? Session.data.fleet.ships.find((s) => s.id === to.ship)
            : ship();
    if (!carrier || !items.length) return;
    const mode = local(from, to) ? "direct" : "queue",
      single = items.length === 1,
      holder = Modules.create(
        "cargo-transfer",
        "Перемещение ресурса",
        single ? 560 : 680,
        single ? 450 : 620,
      );
    formOpen = true;
    holder.replaceChildren();
    holder.append(
      n(
        "h3",
        single
          ? tr(resource(items[0].resource)?.name || items[0].resource)
          : tr("Перемещение выбранных ресурсов"),
      ),
      n("p", label(from)),
      n("p", "→ " + label(to), "transfer-target"),
      n(
        "p",
        tr(
          mode === "direct"
            ? "Перенос на текущей станции"
            : "Будут созданы задачи: корабль перевезёт ресурсы по существующему маршруту.",
        ),
        "muted",
      ),
    );
    let free = Infinity;
    if (to.kind === "ship") {
      const target = Session.data.fleet.ships.find((s) => s.id === to.ship),
        reserved = (Session.data.workbench.mining || [])
          .filter((j) => j.ship_id === target.id)
          .reduce((a, j) => a + j.quantity, 0);
      free =
        target.spec.cargo -
        Object.values(target.cargo).reduce((a, v) => a + v, 0) -
        reserved;
    }
    const list = n("div", undefined, "cargo-transfer-items");
    holder.append(list);
    const fields = items.map((item) => {
      const row = n("div", undefined, "cargo-transfer-item");
      row.dataset.resource = item.resource;
      const input = UI.field(
        row,
        tr(resource(item.resource)?.name || item.resource),
        single ? "cargo-transfer-quantity" : "cargo-transfer-" + item.resource,
        single
          ? Math.min(item.quantity, 100000, Math.max(1, free))
          : Math.min(item.quantity, 100000),
        "number",
      );
      input.min = 1;
      input.max = Math.min(item.quantity, 100000);
      list.append(row);
      return { resource: item.resource, input };
    });
    let requestID = uuid();
    const total = n("p", undefined, "muted");
    holder.append(total);
    const submit = b(
        mode === "direct" ? "Переместить" : "Создать доставку",
        async () => {
          if (pending || !fields.every((f) => f.input.reportValidity())) return;
          const payloadItems = fields.map((f) => ({
            resource: f.resource,
            quantity: Number(f.input.value),
          }));
          if (payloadItems.reduce((a, v) => a + v.quantity, 0) > free) return;
          const serial = ++operation,
            epoch = Session.epoch;
          pending = true;
          cancel.disabled = true;
          fields.forEach((f) => (f.input.disabled = true));
          try {
            await api("logistics", {
              request: requestID,
              mode,
              from,
              to,
              ship: carrier.id,
              ...(single
                ? {
                    resource: payloadItems[0].resource,
                    quantity: payloadItems[0].quantity,
                  }
                : { items: payloadItems }),
            });
            if (
              serial !== operation ||
              epoch !== Session.epoch ||
              !Session.signedIn
            )
              return;
            clearSelection();
            Modules.close("cargo-transfer");
            notify(
              tr(mode === "direct" ? "Ресурс перемещён" : "Задача добавлена"),
            );
            await refreshSnapshot(true);
            await refresh();
          } catch (e) {
            if (
              serial === operation &&
              epoch === Session.epoch &&
              Session.signedIn
            )
              apiError(e);
          } finally {
            if (serial === operation) {
              pending = false;
              cancel.disabled = false;
              fields.forEach((f) => (f.input.disabled = false));
            }
          }
        },
      ),
      cancel = b("Отмена", () => Modules.close("cargo-transfer"));
    function validate() {
      const amount = fields.reduce((a, f) => a + Number(f.input.value), 0);
      total.textContent =
        tr("Общий объём") +
        ": " +
        amount +
        (Number.isFinite(free) ? " / " + free : "");
      total.classList.toggle("error", amount > free);
      submit.disabled =
        amount > free ||
        fields.some((f) => !f.input.checkValidity()) ||
        pending;
    }
    fields.forEach(
      (f) =>
        (f.input.oninput = () => {
          requestID = uuid();
          validate();
        }),
    );
    holder.append(submit, cancel);
    validate();
    Modules.open("cargo-transfer");
  }
  function draggable(tile, from, id, q, enabled = true) {
    tile.draggable = enabled;
    tile.dataset.cargoResource = id;
    tile.dataset.cargoSelectable = String(enabled);
    tile.dataset.cargoLocation = JSON.stringify(from);
    tile.classList.toggle(
      "cargo-selected",
      !!selectionFrom && same(selectionFrom, from) && selectedResources.has(id),
    );
    tile.classList.toggle(
      "cargo-select-mode",
      selectionMode && tile.dataset.cargoSelectable === "true",
    );
    tile.setAttribute(
      "aria-pressed",
      String(tile.classList.contains("cargo-selected")),
    );
    if (!enabled) return;
    tile.classList.add("cargo-draggable");
    const prior = tile.onclick;
    tile.onclick = (e) => {
      if (e.ctrlKey || e.metaKey) {
        e.preventDefault();
        select(from, id);
        return;
      }
      if (performance.now() < suppressClick) return;
      if (selectionMode) {
        e.preventDefault();
        select(from, id);
        return;
      }
      return prior?.call(tile, e);
    };
    tile.ondragstart = (e) => {
      if (pending || formOpen) {
        e.preventDefault();
        return;
      }
      const token = uuid();
      const items =
        selectionFrom && same(selectionFrom, from) && selectedResources.has(id)
          ? selectionItems()
          : [{ resource: id, quantity: q }];
      drag = { from, id, q, token, items };
      e.dataTransfer.effectAllowed = "move";
      e.dataTransfer.setData("application/x-cosmos-cargo", token);
      tile.classList.add("cargo-drag-source");
      document.body.classList.add("cargo-dragging");
    };
    tile.ondragend = () => {
      tile.classList.remove("cargo-drag-source");
      cleanDrag();
    };
  }
  function dropzone(element, target, allowed = () => true) {
    const epoch = Session.epoch;
    let timer;
    const get = () => (typeof target === "function" ? target() : target);
    element.classList.add("cargo-dropzone");
    element.tabIndex = element.tabIndex < 0 ? 0 : element.tabIndex;
    element.ondragover = (e) => {
      if (!drag || !allowed() || same(drag.from, get())) return;
      e.preventDefault();
      e.stopPropagation();
      e.dataTransfer.dropEffect = "move";
      element.classList.add("cargo-drop-ready");
      if (element.tagName === "DETAILS" && !element.open && !timer)
        timer = setTimeout(() => {
          timer = null;
          if (drag && element.isConnected && epoch === Session.epoch) {
            element.open = true;
            open.add(element.dataset.station);
          }
        }, 450);
    };
    element.ondragleave = (e) => {
      if (element.contains(e.relatedTarget)) return;
      clearTimeout(timer);
      timer = null;
      element.classList.remove("cargo-drop-ready");
    };
    element.ondrop = (e) => {
      e.preventDefault();
      e.stopPropagation();
      clearTimeout(timer);
      timer = null;
      const item = drag,
        target = get();
      if (!item || !allowed() || same(item.from, target)) return;
      const token = e.dataTransfer.getData("application/x-cosmos-cargo");
      if (token !== item.token) return;
      cleanDrag();
      transfer(item.from, target, item.items);
    };
  }
  function tile(from, id, q, canMove) {
    const r = resource(id),
      tile = resourceTile(r, id, q, (e, tile) => {
        const rect = tile.getBoundingClientRect();
        UI.menu(
          [
            { heading: r?.name || id },
            { label: "Информация", action: () => showResource(id) },
            {
              label: "Переместить ресурс",
              disabled: !canMove,
              reason: "Недостаточно корпоративных полномочий",
              action: () => chooseDestination(from, id, q),
            },
          ],
          e.clientX || rect.x,
          e.clientY || rect.bottom,
        );
      });
    draggable(tile, from, id, q, canMove);
    return tile;
  }
  function grid(from, cargo, canMove) {
    const grid = n("div", undefined, "cargo-grid");
    for (const [id, q] of Object.entries(cargo || {})) {
      if (q > 0) grid.append(tile(from, id, q, canMove));
    }
    if (!grid.children.length)
      grid.append(
        n(
          "p",
          tr("Здесь пока пусто. Перетащите ресурс для переноса."),
          "cargo-drop-hint",
        ),
      );
    return grid;
  }
  async function chooseDestination(from, id, q) {
    if (!from) return;
    const epoch = Session.epoch;
    try {
      const info = await api("logistics");
      if (epoch !== Session.epoch || !Session.signedIn) return;
      data = info;
    } catch (e) {
      if (epoch === Session.epoch && Session.signedIn) apiError(e);
      return;
    }
    const holder = Modules.create(
        "cargo-destination",
        "Место назначения",
        620,
        420,
      ),
      options = [];
    if (ship())
      options.push({
        id: "ship",
        label: tr("Трюм") + " · " + shipName(ship()),
        location: { kind: "ship", ship: ship().id },
      });
    for (const p of ports()) {
      options.push({
        id: "personal:" + key(p),
        code: String(p.star),
        label: p.name + " · " + tr("Личный склад"),
        station: { star: p.star, id: p.object_id },
        location: { kind: "personal", star: p.star, station: p.object_id },
      });
      if (data?.can_deposit && built(p))
        options.push({
          id: "company:" + key(p),
          code: String(p.star),
          label: p.name + " · " + tr("Корпоративный склад"),
          station: { star: p.star, id: p.object_id },
          location: {
            kind: "company",
            star: p.star,
            station: p.object_id,
            company: data.company,
          },
        });
    }
    const available = options.filter((o) => !same(from, o.location));
    holder.replaceChildren(
      n(
        "h3",
        Array.isArray(id)
          ? tr("Перемещение выбранных ресурсов")
          : tr(resource(id)?.name || id),
      ),
    );
    if (!available.length) {
      UI.empty(holder, "Нет доступных складов");
      Modules.open("cargo-destination");
      return;
    }
    let selected = available[0].id;
    holder.append(
      UI.picker(available, selected, (v) => (selected = v), { system: true }),
      b("Продолжить", () => {
        Modules.close("cargo-destination");
        transfer(
          from,
          available.find((o) => o.id === selected).location,
          id,
          q,
        );
      }),
    );
    Modules.open("cargo-destination");
  }
  function draw() {
    if (!data || drag || formOpen) return;
    const focus = document.activeElement?.id,
      caret = document.activeElement?.selectionStart,
      scroll = root.querySelector(".storage-tree")?.scrollTop || 0;
    UI.rememberFolds(root);
    root.replaceChildren();
    const top = n("div", undefined, "logistics-toolbar"),
      search = UI.field(
        top,
        "Поиск станций и ресурсов",
        "logistics-search",
        query,
        "search",
      );
    search.oninput = () => {
      query = search.value;
      draw();
    };
    UI.choice(
      top,
      "Показывать",
      [
        ["all", "Склады с грузом и текущая станция"],
        ["system", "Текущая система"],
      ],
      scope,
      (v) => {
        scope = v;
        draw();
      },
    );
    top.append(b("Планировщик задач", () => Modules.open("tasks")));
    root.append(top);
    selectionToolbar(root);
    const layout = n("div", undefined, "logistics-layout"),
      hold = n("aside", undefined, "logistics-hold"),
      main = n("section", undefined, "storage-tree"),
      s = ship();
    layout.append(hold, main);
    root.append(layout);
    if (s) {
      hold.append(
        n("h3", shipName(s)),
        n(
          "p",
          (world.data?.stars[s.star - 1]?.name || "") +
            " · " +
            tr(s.position_mode),
          "muted",
        ),
      );
      const reserved = (Session.data.workbench.mining || [])
        .filter((j) => j.ship_id === s.id)
        .reduce((a, j) => a + j.quantity, 0);
      hold.append(
        UI.bar(
          Object.values(s.cargo).reduce((a, v) => a + v, 0) + reserved,
          s.spec.cargo,
          "Трюм",
        ),
        grid({ kind: "ship", ship: s.id }, s.cargo, true),
      );
      dropzone(hold, { kind: "ship", ship: s.id });
      hold.append(
        n(
          "p",
          tr("Перетащите плитку ресурса на нужный склад или в трюм."),
          "muted",
        ),
      );
    }
    const q = query.trim().toLowerCase();
    const filtered = ports()
      .filter((p) => {
        const inventories = [stock("personal", p), stock("company", p)],
          has = inventories.some((c) => Object.values(c).some((v) => v > 0));
        return (
          (has || shipAt(s, p)) &&
          (scope !== "system" || p.star === s?.star) &&
          (!q ||
            [
              p.name,
              world.data?.stars[p.star - 1]?.name,
              p.owner_name,
              ...inventories.flatMap((c) =>
                Object.keys(c)
                  .filter((id) => c[id] > 0)
                  .flatMap((id) => [
                    tr(resource(id)?.name || id),
                    resource(id)?.code || id,
                  ]),
              ),
            ].some((v) =>
              String(v || "")
                .toLowerCase()
                .includes(q),
            ))
        );
      })
      .sort(
        (a, b) =>
          Number(b.star === s?.star) - Number(a.star === s?.star) ||
          a.name.localeCompare(b.name),
      );
    main.append(
      n("p", tr("Доступные станции") + ": " + filtered.length, "storage-count"),
    );
    for (const p of filtered) {
      const details = n("details", undefined, "storage-station"),
        summary = n("summary"),
        name = n("span", undefined, "storage-station-name"),
        count = [stock("personal", p), stock("company", p)]
          .flatMap((c) => Object.values(c))
          .reduce((a, v) => a + v, 0);
      details.dataset.station = key(p);
      details.open = open.has(key(p));
      name.append(
        stationLabel(p.star, p.object_id, p.name, "b"),
        n("small", world.data?.stars[p.star - 1]?.name || ""),
      );
      summary.append(
        n("span", "◉", "station-symbol"),
        name,
        n(
          "span",
          tr(shipAt(s, p) ? "На станции" : "Удалённый склад"),
          "storage-location",
        ),
        n("b", Format.quantity(count), "storage-total"),
      );
      details.append(summary);
      const contents = n("div", undefined, "storage-contents");
      details.append(contents);
      const render = () => {
        if (contents.childElementCount) return;
        for (const kind of ["personal", "company"]) {
          if (kind === "company" && !data.company) continue;
          const enabled = kind === "personal" || built(p),
            section = n("section", undefined, "storage-inventory"),
            loc = {
              kind,
              star: p.star,
              station: p.object_id,
              ...(kind === "company" ? { company: data.company } : {}),
            };
          section.dataset.inventory = kind;
          section.append(
            n(
              "h4",
              tr(kind === "personal" ? "Личный склад" : "Корпоративный склад") +
                (kind === "company"
                  ? " [" + (Session.data.profile.company_tag || "") + "]"
                  : ""),
            ),
          );
          if (enabled) {
            section.append(
              grid(
                loc,
                stock(kind, p),
                kind === "personal" || data.can_withdraw,
              ),
            );
            dropzone(
              section,
              loc,
              () => kind === "personal" || data.can_deposit,
            );
          } else {
            section.append(
              n("p", tr("Корпоративный склад ещё не построен"), "muted"),
              grid(loc, stock(kind, p), false),
            );
            section.ondragover = (e) => {
              e.preventDefault();
              e.stopPropagation();
              e.dataTransfer.dropEffect = "none";
            };
            section.ondrop = (e) => {
              e.preventDefault();
              e.stopPropagation();
            };
          }
          contents.append(section);
        }
      };
      details.ontoggle = () => {
        if (details.open) {
          open.add(key(p));
          render();
        } else open.delete(key(p));
      };
      if (details.open) render();
      dropzone(details, {
        kind: "personal",
        star: p.star,
        station: p.object_id,
      });
      main.append(details);
    }
    if (!filtered.length) UI.empty(main, "Ничего не найдено");
    const history = UI.fold("logistics:history", false, "logistics-history");
    history.append(n("summary", tr("История перемещений")));
    for (const entry of data.history || []) {
      const r = entry.result;
      history.append(
        n(
          "p",
          (r.items || [{ resource: r.resource, quantity: r.quantity }])
            .map(
              (item) =>
                tr(resource(item.resource)?.name || item.resource) +
                " × " +
                item.quantity,
            )
            .join(", ") +
            " · " +
            label(r.from) +
            " → " +
            label(r.to) +
            " · " +
            tr(
              r.mode === "direct"
                ? "Перемещено"
                : r.tasks?.length
                  ? "Созданы доставки"
                  : entry.task_status || "queued",
            ),
        ),
      );
    }
    root.append(history);
    main.scrollTop = scroll;
    if (focus === "logistics-search") {
      search.focus();
      if (caret !== null) search.setSelectionRange(caret, caret);
    }
  }
  Object.defineProperty(Logistics, "dragging", { get: () => !!drag });
  Object.assign(Logistics, {
    draggable,
    selectionToolbar,
    dropzone,
    chooseDestination,
    refresh,
  });
  window.addEventListener("moduleopen", (e) => {
    if (e.detail === "logistics") {
      if (!world.data)
        Modules.wait(e.detail, loadWorld()
          .then((g) => {
            if (!g || !Session.signedIn || !Modules.isOpen("logistics")) return;
            return refresh();
          })
          );
      else Modules.wait(e.detail, refresh());
    }
  });
  window.addEventListener("gamedata", () => {
    if (Modules.isOpen("logistics") && !drag) refresh().catch(apiError);
  });
  window.addEventListener("languagechange", () => {
    if (data && Modules.isOpen("logistics")) draw();
  });
  window.addEventListener("moduleclose", (e) => {
    if (e.detail === "cargo-transfer") {
      formOpen = false;
      suppressClick = 0;
      if (Modules.isOpen("logistics")) refresh().catch(apiError);
    }
  });
  window.addEventListener("signedout", () => {
    request++;
    operation++;
    data = null;
    selectionMode = false;
    clearSelection();
    open.clear();
    query = "";
    scope = "all";
    formOpen = false;
    pending = false;
    cleanDrag();
    root.replaceChildren();
    for (const id of ["logistics", "cargo-transfer", "cargo-destination"])
      Modules.close(id);
  });
}
