import { assetURL } from "../../core/assets.js";
/* Shared colony catalogue: presentation only, no colony mutations or fabricated sites. */
const views = new WeakMap();
const TYPES = Object.freeze({
  Каменистая: "object-rocky.svg",
  "Садовый мир": "object-garden.svg",
  Пустынная: "object-desert.svg",
  Ледяная: "object-icy.svg",
  Лавовая: "object-lava.svg",
  Вулканическая: "object-lava.svg",
  "Газовый гигант": "object-gas.svg",
});
const STATUS = {
  funding: ["Финансирование", "Funding"],
  building: ["Строительство", "Building"],
  complete: ["Колонизация завершена", "Completed"],
  cancelled: ["Отменён", "Cancelled"],
  expired: ["Просрочен", "Expired"],
};
const LABELS = {
  "Колонии и проекты": "Colonies and projects",
  "Мои колонии": "My colonies",
  "Выбранная колония": "Selected colony",
  Планета: "Planet",
  Система: "System",
  "Тип планеты": "Planet type",
  Владелец: "Owner",
  "Тип владения": "Ownership",
  Личная: "Personal",
  Корпоративная: "Corporate",
  Сектор: "Sector",
  Статус: "Status",
  Прогресс: "Progress",
  "Открыть проект": "Open project",
  "Поверхность планеты": "Planet surface",
  "Продолжить высадку": "Resume landing",
  "Получить возврат": "Claim refund",
  "Нет доступных колоний или проектов колонизации":
    "No colonies or colonization projects available",
  "Ничего не найдено": "No matching colonies",
  "Поиск колонии, планеты или системы": "Search colonies, planets or systems",
  "Сведения о планете недоступны": "Planet information unavailable",
  "Загрузка сведений о планете…": "Loading planet information…",
  "Доступные проекты и колонии": "Available projects and colonies",
  Возвраты: "Refunds",
  Всего: "Total",
  Активные: "Active",
  Завершённые: "Completed",
  Все: "All",
  "Сбросить фильтры": "Reset filters",
  "Этапы строительства": "Construction stages",
  "Выберите колонию в списке, чтобы посмотреть её состояние и открыть планету.":
    "Select a colony to view its status and open its planet.",
  "Попробуйте другой запрос или сбросьте фильтры.":
    "Try a different search or reset the filters.",
};
export function mountColonyCatalog(root, data, options) {
  let state = views.get(root);
  if (!state) {
    state = {
      selected: null,
      query: "",
      filter: "all",
      cache: new Map(),
      serial: 0,
      request: 0,
      scroll: 0,
    };
    views.set(root, state);
  }
  // Authenticated research/world changes can reveal a previously hidden planet.
  // The optional revision comes from production snapshots, never from mock data.
  if (state.revision !== options.revision) {
    state.revision = options.revision;
    state.serial++;
    state.cache.clear();
    state.pending = new Set();
  }
  const lang = options.language?.() === "en" ? "en" : "ru";
  const tr = (s) => (lang === "en" ? LABELS[s] || s : s);
  const status = (s) =>
    STATUS[s] ? STATUS[s][lang === "en" ? 1 : 0] : String(s || "");
  const node = (tag, cls, value) => {
    const e = document.createElement(tag);
    if (cls) e.className = cls;
    if (value !== undefined && value !== null) e.textContent = String(value);
    return e;
  };
  const makeAction = (label, fn) => {
    const b = node(
      "button",
      "terminal-action colony-catalog-action",
      tr(label),
    );
    b.type = "button";
    b.addEventListener("click", fn);
    return b;
  };
  const projects = Array.isArray(data?.projects) ? data.projects : [];
  const active = (p) => p.status === "funding" || p.status === "building";
  const matchesFilter = (p) =>
    state.filter === "active" ? active(p) :
    state.filter === "complete" ? p.status === "complete" : true;
  if (!projects.some((p) => String(p.id) === String(state.selected)))
    state.selected = projects.length ? projects[0].id : null;
  state.request++;
  const oldSearch = root.querySelector(".colony-catalog-search");
  const restoreFocus = document.activeElement === oldSearch;
  const cursor = restoreFocus ? oldSearch.selectionStart : null;
  const oldList = root.querySelector(".colony-catalog-entries");
  if (oldList) state.scroll = oldList.scrollTop;
  root.classList.add("colony-catalog-root");
  root.replaceChildren();
  const header = node("div", "colony-catalog-heading");
  const heading = node("div");
  heading.append(
    node("h3", null, tr("Колонии и проекты")),
    node("p", "muted", tr("Доступные проекты и колонии")),
  );
  header.append(heading, makeAction("Продолжить высадку", options.onVisit));
  const summary = node("div", "colony-catalog-summary");
  for (const [label, count] of [
    ["Всего", projects.length],
    ["Активные", projects.filter(active).length],
    ["Завершённые", projects.filter((p) => p.status === "complete").length],
  ]) {
    const stat = node("div", "colony-catalog-stat");
    stat.append(node("strong", null, count), node("span", null, tr(label)));
    summary.append(stat);
  }
  const layout = node("div", "colony-catalog-layout"),
    detail = node("section", "colony-catalog-detail"),
    library = node("section", "colony-catalog-library");
  const libraryHeading = node("div", "colony-catalog-library-heading");
  const count = node("span", "colony-catalog-count");
  libraryHeading.append(node("h3", null, tr("Мои колонии")), count);
  const search = node("input", "colony-catalog-search");
  search.type = "search";
  search.value = state.query;
  search.placeholder = tr("Поиск колонии, планеты или системы");
  search.setAttribute("aria-label", search.placeholder);
  const entries = node("div", "colony-catalog-entries");
  entries.setAttribute("aria-label", tr("Мои колонии"));
  const filters = node("div", "colony-catalog-filters");
  filters.setAttribute("role", "group");
  filters.setAttribute("aria-label", tr("Статус"));
  for (const [value, label] of [["all", "Все"], ["active", "Активные"], ["complete", "Завершённые"]]) {
    const button = makeAction(label, () => {
      state.filter = value;
      for (const b of filters.children)
        b.setAttribute("aria-pressed", String(b.dataset.filter === value));
      renderEntries();
    });
    button.dataset.filter = value;
    button.setAttribute("aria-pressed", String(state.filter === value));
    filters.append(button);
  }
  library.append(libraryHeading, search, filters, entries);
  layout.append(library, detail);
  root.append(header, summary, layout);
  const refunds = Array.isArray(data?.refunds) ? data.refunds : [];
  if (refunds.length) {
    const section = node("section", "colony-catalog-refunds");
    section.append(node("h3", null, tr("Возвраты")));
    for (const refund of refunds) {
      section.append(
        makeAction("Получить возврат", () => options.onRefund(refund)),
      );
      const b = section.lastChild;
      b.textContent =
        tr("Получить возврат") +
        " · " +
        options.resourceName(refund.resource) +
        " × " +
        refund.amount;
    }
    root.append(section);
  }
  const planetData = (p) =>
    state.cache.get(String(p.star) + ":" + String(p.planet));
  const starName = (p) => options.starName?.(p.star) || "";
  function row(host, label, value) {
    if (value === undefined || value === null || String(value).trim() === "")
      return;
    const item = node("div", "colony-catalog-row");
    item.append(node("dt", null, tr(label)), node("dd", null, value));
    host.append(item);
  }
  function renderDetail() {
    detail.replaceChildren(
      node("p", "colony-catalog-eyebrow", tr("Выбранная колония")),
    );
    const p = projects.find((x) => String(x.id) === String(state.selected));
    if (!p) {
      const empty = node("div", "colony-catalog-empty");
      empty.append(node("h3", null, tr("Нет доступных колоний или проектов колонизации")),
        node("p", "muted", tr("Выберите колонию в списке, чтобы посмотреть её состояние и открыть планету.")));
      detail.append(empty);
      return;
    }
    const info = planetData(p);
    const visual = node("div", "colony-catalog-visual");
    const img = node("img", "colony-catalog-planet");
    img.alt = info?.site?.name || p.name || tr("Планета");
    const kind = info?.site?.kind;
    img.src = assetURL(options.assetBase + (TYPES[kind] || "object-unknown.svg"));
    img.onerror = () => {
      if (!img.dataset.fallback) {
        img.dataset.fallback = "1";
        img.src = assetURL(options.assetBase + "object-unknown.svg");
      }
    };
    visual.append(img);
    if (info?.site?.name)
      visual.append(node("span", "colony-catalog-planet-name", info.site.name));
    const hero = node("div", "colony-catalog-hero");
    const title = node("div", "colony-catalog-title");
    title.append(node("h2", null, p.colony_name || p.name || info?.site?.name || tr("Планета")));
    const location = [info?.star?.name || starName(p), p.sector != null ? tr("Сектор") + " " + p.sector : ""].filter(Boolean).join(" · ");
    title.append(node("p", "colony-catalog-location", location));
    const badge = node("span", "colony-catalog-status", status(p.status));
    badge.dataset.status = p.status || "";
    title.append(badge);
    hero.append(visual, title);
    detail.append(hero);
    const planet = node("dl", "colony-catalog-data"),
      colony = node("dl", "colony-catalog-data");
    row(planet, "Планета", info?.site?.name || p.name);
    row(planet, "Система", info?.star?.name || starName(p));
    row(
      planet,
      "Тип планеты",
      kind && (lang === "en" ? options.translateKind?.(kind) || kind : kind),
    );
    if (!info)
      planet.append(node("small", "muted", tr("Загрузка сведений о планете…")));
    else if (!info.site)
      planet.append(
        node("small", "muted", tr("Сведения о планете недоступны")),
      );
    row(colony, "Владелец", p.owner);
    row(
      colony,
      "Тип владения",
      p.owner_company != null
        ? tr("Корпоративная")
        : p.owner_user != null
          ? tr("Личная")
          : null,
    );
    row(colony, "Сектор", p.sector);
    const facts = node("div", "colony-catalog-facts");
    const planetSection = node("section"), colonySection = node("section");
    planetSection.append(node("h3", null, tr("Планета")), planet);
    colonySection.append(node("h3", null, tr("Колонии и проекты")), colony);
    facts.append(planetSection, colonySection);
    detail.append(facts);
    if (
      Number.isFinite(Number(p.progress)) &&
      Number.isFinite(Number(p.build_ticks)) &&
      Number(p.build_ticks) > 0 &&
      p.status === "building"
    ) {
      const construction = node("div", "colony-catalog-construction");
      const label = node("div", "colony-catalog-progress-label");
      const value = Math.max(0, Math.min(Number(p.progress), Number(p.build_ticks)));
      label.append(node("span", null, tr("Прогресс")), node("strong", null, Math.floor(value / Number(p.build_ticks) * 100) + "%"));
      const progress = node("progress", "colony-catalog-progress");
      progress.max = Number(p.build_ticks);
      progress.value = value;
      progress.setAttribute("aria-label", tr("Этапы строительства"));
      construction.append(label, progress, node("small", "muted", p.progress + " / " + p.build_ticks));
      detail.append(construction);
    }
    const actions = node("div", "colony-catalog-actions");
    actions.append(
      makeAction("Открыть проект", () => options.onProject(p.id)),
      makeAction("Поверхность планеты", () =>
        options.onSurface(p.star, p.planet),
      ),
    );
    actions.firstChild.classList.add("colony-catalog-primary");
    detail.append(actions);
    if (!info && p.star != null && p.planet != null) resolve(p);
  }
  async function resolve(p) {
    const key = String(p.star) + ":" + String(p.planet);
    if (state.cache.has(key) || state.pending?.has(key)) return;
    if (!state.pending) state.pending = new Set();
    state.pending.add(key);
    const serial = state.serial,
      pending = state.pending;
    try {
      const system = await options.loadSystem(p.star);
      if (state.serial !== serial) return;
      const site = system?.sites?.find(
        (s) =>
          Number(s.planet) === Number(p.planet) &&
          TYPES[s.kind] &&
          s.kind !== "Орбитальная станция",
      );
      state.cache.set(key, { site: site || null, star: system?.star || null });
    } catch (error) {
      if (state.serial !== serial) return;
      state.cache.set(key, { site: null, star: null });
      options.onLoadError?.(error);
    } finally {
      pending.delete(key);
      if (state.serial === serial && root.isConnected) {
        state.renderEntries?.();
        if (String(state.selected) === String(p.id)) state.renderDetail?.();
      }
    }
  }
  function renderEntries() {
    const oldScroll = entries.scrollTop;
    entries.replaceChildren();
    const q = state.query.trim().toLocaleLowerCase();
    const filtered = projects.filter((p) => matchesFilter(p) &&
      [
        p.colony_name,
        p.name,
        p.owner,
        starName(p),
        planetData(p)?.site?.name,
      ].some((v) =>
        String(v || "")
          .toLocaleLowerCase()
          .includes(q),
      ),
    );
    count.textContent = filtered.length + " / " + projects.length;
    for (const p of filtered) {
      const item = node("button", "colony-catalog-entry");
      item.type = "button";
      item.dataset.projectId = String(p.id);
      item.setAttribute(
        "aria-pressed",
        String(p.id) === String(state.selected) ? "true" : "false",
      );
      const top = node("span", "colony-catalog-entry-top");
      const entryStatus = node("span", "colony-catalog-entry-status", status(p.status));
      entryStatus.dataset.status = p.status || "";
      top.append(node("strong", null, p.colony_name || p.name || tr("Планета")));
      const secondary = [
        starName(p),
        p.sector != null ? tr("Сектор") + " " + p.sector : "",
      ]
        .filter(Boolean)
        .join(" · ");
      item.append(
        top,
        node("small", null, secondary),
        node("small", "muted", p.owner || ""),
        entryStatus,
      );
      item.addEventListener("click", () => {
        state.selected = p.id;
        for (const e of entries.children)
          e.setAttribute(
            "aria-pressed",
            String(e.dataset.projectId) === String(p.id) ? "true" : "false",
          );
        renderDetail();
      });
      entries.append(item);
    }
    if (!filtered.length) {
      const empty = node("div", "colony-catalog-empty");
      empty.append(node("p", null, projects.length ? tr("Ничего не найдено") : tr("Нет доступных колоний или проектов колонизации")));
      if (projects.length) empty.append(node("small", "muted", tr("Попробуйте другой запрос или сбросьте фильтры.")), makeAction("Сбросить фильтры", () => {
        state.query = "";
        state.filter = "all";
        mountColonyCatalog(root, data, options);
        root.querySelector(".colony-catalog-search").focus({preventScroll: true});
      }));
      entries.append(empty);
    }
    entries.scrollTop = oldScroll;
  }
  state.renderDetail = renderDetail;
  state.renderEntries = renderEntries;
  search.addEventListener("input", () => {
    state.query = search.value;
    renderEntries();
  });
  renderEntries();
  entries.scrollTop = state.scroll || 0;
  renderDetail();
  if (restoreFocus) {
    search.focus({ preventScroll: true });
    if (cursor !== null) search.setSelectionRange(cursor, cursor);
  }
  return {
    select: (id) => {
      state.selected = id;
      renderEntries();
      renderDetail();
    },
  };
}
export function clearColonyCatalog(root) {
  const s = views.get(root);
  if (s) {
    s.serial++;
    views.delete(root);
  }
}
