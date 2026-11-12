import { pager } from "../../ui/pager.js";
import { mountSystemLineup } from "./system-view.js";
import { objectHologram } from "../objects/presentation.js";
import { shipName } from "../fleet/presentation.js";
import { Format } from "../../core/format.js";
import { Session } from "../../core/session.js";
import { I18n } from "../../core/i18n.js";
import { api } from "../../core/api.js";
import { notify } from "../../ui/feedback.js";
import { UI, Modules } from "../../ui/runtime.js";
import { Game } from "../runtime.js";
import { world, loadWorld } from "../../core/world-store.js";

// Approved v13 catalogue, using only the authenticated game's snapshots/API.
export function initStudies() {
  const n = UI.node,
    tr = (s) => I18n.t(s),
    b = UI.button,
    root = Modules.create("studies", "Исследования", 1100, 730);
  root.classList.add("studies-content");
  let query = "",
    scope = "all",
    page = 1,
    selectedStar = null,
    viewHandle = null,
    sideKey = "",
    entriesKey = "";
  const ship = () => Session.data.expedition?.ship;
  const known = () => Session.data.workbench?.known || [];
  const jobs = () => Session.data.workbench?.research || [];
  const personal = (item) => item.source === "survey";
  const star = (id) =>
    id === ship()?.star ? Session.data.expedition?.system : world.stars.get(id);
  const region = (s) =>
    tr(
      world.data?.regions?.find((r) => r.id === s?.region)?.name ||
        "Регион неизвестен",
    );
  const source = (item) =>
    personal(item)
      ? "Личное исследование"
      : item.source === "purchased"
        ? "Купленные разведданные"
        : "Получено ранее";
  const currentJob = () =>
    jobs().find((j) => j.ship === ship()?.id && j.star === ship()?.star);
  const currentSurvey = () =>
    known().find((item) => item.star === ship()?.star);
  const currentStatus = () =>
    currentSurvey()
      ? "Система исследована"
      : currentJob()?.paused
        ? "Исследование приостановлено"
        : currentJob()
          ? "Исследование выполняется"
          : "Система не исследована";

  function layout() {
    if (root.querySelector(".studies-layout")) return;
    const heading = n("div", undefined, "studies-heading"),
      title = n("div");
    title.append(
      n("h3", tr("Архив исследований")),
      n(
        "p",
        tr(
          "Открывайте новые системы и храните результаты экспедиций в личном каталоге.",
        ),
        "muted",
      ),
    );
    heading.append(n("span", "⌾", "studies-emblem"), title);
    const layout = n("div", undefined, "studies-layout"),
      library = n("section", undefined, "studies-library"),
      tools = n("div", undefined, "studies-toolbar");
    const search = UI.field(
      tools,
      "Поиск систем",
      "studies-search",
      query,
      "search",
    );
    search.oninput = () => {
      query = search.value;
      page = 1;
      entries();
    };
    // Same compact menu as the approved prototype, backed by the shared UI.menu.
    const label = n("label"),
      filter = b("Все исследования", () => {
        const r = filter.getBoundingClientRect();
        UI.menu(
          options.map(([value, text]) => ({
            label: text,
            checked: scope === value,
            action: () => {
              scope = value;
              page = 1;
              filter.textContent = tr(text);
              entries();
            },
          })),
          r.left,
          r.bottom + 3,
          { owner: filter, theme: "chat" },
        );
      });
    const options = [
      ["all", "Все исследования"],
      ["own", "Личные исследования"],
      ["received", "Полученные сведения"],
    ];
    filter.classList.add("studies-source-picker");
    filter.textContent = tr(options.find(([value]) => value === scope)[1]);
    filter.setAttribute("aria-haspopup", "menu");
    label.append(n("span", tr("Источник")), filter);
    tools.append(label);
    library.append(
      tools,
      n("div", undefined, "studies-current-pin"),
      n("div", undefined, "studies-entries"),
      n("div", undefined, "pagination"),
    );
    layout.append(n("aside", undefined, "studies-current"), library);
    root.append(heading, n("div", undefined, "studies-stats"), layout);
  }
  function markSelection() {
    for (const row of root.querySelectorAll(".study-entry[data-star]")) {
      const chosen = Number(row.dataset.star) === selectedStar;
      row.classList.toggle("study-entry-selected", chosen);
      row.setAttribute("aria-pressed", String(chosen));
    }
  }
  function choose(id) {
    if (id !== ship()?.star && !known().some((item) => item.star === id))
      return;
    selectedStar = id;
    markSelection();
    detail();
  }
  function showMap(id) {
    Modules.open("galaxy");
    window.dispatchEvent(new CustomEvent("showstar", { detail: id }));
  }
  function showSystem(id) {
    choose(id);
    viewHandle?.close();
    viewHandle = mountSystemLineup({
      host: root,
      starId: id,
      assetBase: "",
      fallbackAsset: "object-unknown.svg",
      language: () => I18n.language,
      translateKind: tr,
      resolveResource: (resource) =>
        tr(
          Session.data.expedition?.resources?.find((r) => r.id === resource)
            ?.name || resource,
        ),
      loadSystem: (value, signal) =>
        api("system?id=" + encodeURIComponent(value), undefined, signal),
      onBack: () => {
        viewHandle = null;
        markSelection();
      },
    });
  }
  function entry(item, isCurrent = false) {
    const s = star(item.star),
      card = n("article", undefined, "study-entry"),
      body = n("div"),
      heading = n("div", undefined, "study-entry-heading"),
      area = n("span", region(s), "study-region"),
      tag = n(
        "span",
        tr(isCurrent ? currentStatus() : source(item)),
        isCurrent ? "study-status study-current-status" : "study-source",
      );
    card.dataset.star = String(item.star);
    card.classList.toggle("study-entry-current", isCurrent);
    card.setAttribute("role", "button");
    card.tabIndex = 0;
    card.setAttribute(
      "aria-label",
      (s?.name || String(item.star)) + ". " + tr("Выбрать для просмотра"),
    );
    area.title = tr("Регион") + ": " + region(s);
    if (isCurrent) tag.dataset.known = String(!!currentSurvey());
    else tag.dataset.source = item.source;
    heading.append(n("strong", s?.name || String(item.star)), area);
    if (isCurrent)
      heading.append(n("span", "⌖ " + tr("КОРАБЛЬ ЗДЕСЬ"), "study-ship-badge"));
    body.append(heading, tag);
    if (!isCurrent) body.append(n("small", Format.timestamp(item.received_at)));
    const actions = n("div", undefined, "study-entry-actions");
    actions.append(
      b("На карте", () => showMap(item.star)),
      b("Подробнее", () => showSystem(item.star)),
    );
    card.append(n("span", s?.class || "✦", "study-star"), body, actions);
    card.onclick = (event) => {
      if (!event.target.closest("button")) choose(item.star);
    };
    card.onkeydown = (event) => {
      if (event.target === card && ["Enter", " "].includes(event.key)) {
        event.preventDefault();
        choose(item.star);
      }
    };
    return card;
  }
  function entries() {
    const list = root.querySelector(".studies-entries"),
      pin = root.querySelector(".studies-current-pin");
    if (!list || !ship()) return;
    const q = query.trim().toLocaleLowerCase(I18n.language);
    const items = known()
      .filter(
        (item) =>
          item.star !== ship().star &&
          (scope === "all" ||
            (scope === "own" ? personal(item) : !personal(item))) &&
          [
            star(item.star)?.name || String(item.star),
            tr(star(item.star)?.galaxy || "edem"),
            region(star(item.star)),
          ].some((text) => text.toLocaleLowerCase(I18n.language).includes(q)),
      )
      .sort(
        (a, b) =>
          new Date(b.received_at) - new Date(a.received_at) || a.star - b.star,
      );
    const pages = Math.max(1, Math.ceil(items.length / 10));
    page = Math.min(page, pages);
    const key = JSON.stringify([
      I18n.language,
      query,
      scope,
      page,
      currentStatus(),
      star(ship().star),
      items.map((i) => [i, star(i.star)]),
      world.data?.regions,
    ]);
    if (key === entriesKey) {
      markSelection();
      return;
    }
    entriesKey = key;
    const scroll = list.scrollTop,
      focused = list.contains(document.activeElement)
        ? document.activeElement.closest("[data-star]")?.dataset.star
        : null;
    pin.replaceChildren(entry({ star: ship().star }, true));
    list.replaceChildren(
      ...items.slice((page - 1) * 10, page * 10).map((item) => entry(item)),
    );
    if (!items.length)
      UI.empty(list, "Пока нет исследований по выбранным условиям");
    root.querySelector(".pagination").replaceChildren(
      ...pager({
        page,
        pages,
        total: items.length,
        change: (value) => {
          page = value;
          entries();
        },
      }).childNodes,
    );
    markSelection();
    if (focused)
      list
        .querySelector(`[data-star="${Number(focused)}"]`)
        ?.focus({ preventScroll: true });
    list.scrollTop = scroll;
  }
  function detail() {
    const s = ship(),
      system = star(selectedStar),
      isCurrent = selectedStar === s?.star,
      survey = known().find((item) => item.star === selectedStar),
      job = isCurrent ? currentJob() : null,
      side = root.querySelector(".studies-current");
    if (!side || !s) return;
    const key = JSON.stringify([
      I18n.language,
      selectedStar,
      system,
      survey,
      s,
      jobs(),
      Session.data.fleet?.ships,
      world.data?.regions,
    ]);
    if (sideKey === key) return;
    sideKey = key;
    const scroll = side.scrollTop;
    side.replaceChildren(
      n(
        "p",
        tr(isCurrent ? "Текущая система" : "Выбранная система"),
        "eyebrow",
      ),
    );
    const cover = n("div", undefined, "study-current-cover");
    // Never substitute the active ship's star for a selected star's metadata.
    if (system) cover.append(objectHologram({ kind: "Звезда" }, system));
    side.append(cover, n("h2", system?.name || String(selectedStar)));
    const status = n(
      "span",
      tr(isCurrent ? currentStatus() : "Система исследована"),
      "study-status",
    );
    status.dataset.known = String(!!survey);
    side.append(status, n("p", region(system), "muted"));
    if (!isCurrent && system) {
      const properties = n("p", undefined, "study-detail-properties");
      properties.append(
        n("span", tr("Звезда класса") + " " + system.class),
        n("span", tr(system.galaxy)),
      );
      side.append(properties);
    }
    if (survey) side.append(n("p", tr(source(survey)), "muted"));
    const actions = n("div", undefined, "study-detail-actions");
    if (job) {
      side.append(
        UI.bar(job.scanned, job.total, "Объекты"),
        n(
          "p",
          tr(job.paused ? "Приостановлено" : "Сканирование") +
            " · " +
            job.seconds +
            " s / " +
            tr("Объект"),
          "muted",
        ),
      );
      actions.append(
        b(job.paused ? "Продолжить" : "Приостановить", () =>
          Game.command({
            action: job.paused ? "resume_research" : "pause_research",
            ship: s.id,
          }),
        ),
      );
    } else if (isCurrent) {
      actions.append(
        b(
          "Исследовать систему · 10 CR",
          () => Game.explore(s.star),
          !!survey || !!s.destination || !!s.local_target,
          survey
            ? "Система уже исследована"
            : s.destination || s.local_target
              ? "Корабль в пути"
              : "Сканирование объектов текущей системы",
        ),
      );
    } else actions.append(b("На карте", () => showMap(selectedStar)));
    actions.append(b("Подробнее", () => showSystem(selectedStar)));
    side.append(actions);
    const active = n("section", undefined, "studies-active");
    active.append(n("h3", tr("Текущие экспедиции")));
    for (const j of jobs()) {
      const vessel = Session.data.fleet?.ships.find((v) => v.id === j.ship),
        card = n("div", undefined, "study-job");
      card.append(
        n("b", vessel ? shipName(vessel) : tr("Корабль")),
        n("p", star(j.star)?.name || String(j.star)),
        UI.bar(j.scanned, j.total, "Объекты"),
        n("small", tr(j.paused ? "Приостановлено" : "Сканирование")),
      );
      active.append(card);
    }
    if (!jobs().length) UI.empty(active, "Нет активных экспедиций");
    side.append(active);
    side.scrollTop = scroll;
  }
  function render() {
    if (!Session.signedIn || !ship() || !Modules.isOpen("studies")) return;
    layout();
    if (
      selectedStar !== ship().star &&
      !known().some((item) => item.star === selectedStar)
    )
      selectedStar = ship().star;
    const stats = root.querySelector(".studies-stats");
    stats.replaceChildren(
      ...[
        ["Известных систем", known().length],
        ["Лично исследовано", known().filter(personal).length],
        [
          "Получено из разведданных",
          known().filter((item) => item.source === "purchased").length,
        ],
      ].map(([label, value]) => {
        const card = n("div");
        card.append(n("strong", String(value)), n("span", tr(label)));
        return card;
      }),
    );
    detail();
    entries();
  }
  window.addEventListener("moduleopen", async (event) => {
    if (event.detail !== "studies" || !Session.signedIn) return;
    render();
    try {
      await loadWorld();
      render();
    } catch (error) {
      if (Session.signedIn) notify(error.message, "error");
    }
  });
  window.addEventListener("gamedata", render);
  window.addEventListener("languagechange", () => {
    const detailStar = viewHandle?.element.dataset.star;
    viewHandle?.close();
    root.replaceChildren();
    sideKey = entriesKey = "";
    render();
    if (detailStar && Modules.isOpen("studies")) showSystem(Number(detailStar));
  });
  window.addEventListener("signedout", () => {
    viewHandle?.close();
    selectedStar = null;
    query = "";
    scope = "all";
    page = 1;
    sideKey = entriesKey = "";
    root.replaceChildren();
    Modules.close("studies");
  });
}
