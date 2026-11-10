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
import { world } from "../../core/world-store.js";

export function initStudies() {
  const n = UI.node,
    tr = (s) => I18n.t(s),
    b = UI.button,
    root = Modules.create("studies", "Исследования", 1100, 730);
  root.classList.add("studies-content");
  let query = "",
    scope = "all",
    page = 1,
    epoch = 0,
    viewHandle = null;
  const ship = () => Session.data.expedition?.ship,
    known = () => Session.data.workbench?.known || [];
  const personal = (item) => item.source === "survey";
  const star = (id) => world.stars.get(id);
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
  function layout() {
    if (root.querySelector(".studies-layout")) return;
    const heading = n("div", undefined, "studies-heading");
    heading.append(n("span", "⌾", "studies-emblem"), n("div"));
    heading.lastChild.append(
      n("h3", tr("Архив исследований")),
      n(
        "p",
        tr(
          "Открывайте новые системы и храните результаты экспедиций в личном каталоге.",
        ),
        "muted",
      ),
    );
    const stats = n("div", undefined, "studies-stats"),
      layout = n("div", undefined, "studies-layout"),
      side = n("aside", undefined, "studies-current"),
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
    UI.choice(
      tools,
      "Источник",
      [
        ["all", "Все исследования"],
        ["own", "Личные исследования"],
        ["received", "Полученные сведения"],
      ],
      scope,
      (value) => {
        scope = value;
        page = 1;
        entries();
      },
    );
    library.append(
      tools,
      n("div", undefined, "studies-entries"),
      n("div", undefined, "pagination"),
    );
    layout.append(side, library);
    root.append(heading, stats, layout);
  }
  function showSystem(starId) {
    if (viewHandle) viewHandle.close();
    viewHandle = mountSystemLineup({
      host: root,
      starId,
      assetBase: "",
      fallbackAsset: "object-unknown.svg",
      language: () => I18n.language,
      resolveResource: (id) =>
        Session.data.expedition?.resources?.find((r) => r.id === id)?.name || id,
      loadSystem: (id, signal) => api("system?id=" + encodeURIComponent(id), undefined, signal),
      onBack: () => { viewHandle = null; }
    });
  }

  function entries() {
    const list = root.querySelector(".studies-entries"),
      pagination = root.querySelector(".pagination");
    if (!list) return;
    const q = query.trim().toLocaleLowerCase(I18n.language),
      items = known()
        .filter(
          (item) =>
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
            new Date(b.received_at) - new Date(a.received_at) ||
            a.star - b.star,
        );
    const pages = Math.max(1, Math.ceil(items.length / 10));
    page = Math.min(page, pages);
    list.replaceChildren();
    for (const item of items.slice((page - 1) * 10, page * 10)) {
      const s = star(item.star),
        card = n("article", undefined, "study-entry"),
        body = n("div"),
        heading = n("div", undefined, "study-entry-heading"),
        area = n("span", region(s), "study-region"),
        tag = n("span", tr(source(item)), "study-source");
      tag.dataset.source = item.source;
      area.title = tr("Регион") + ": " + region(s);
      heading.append(n("strong", s?.name || String(item.star)), area);
      body.append(heading, tag, n("small", Format.timestamp(item.received_at)));
      const actions = n("div", undefined, "study-entry-actions");
      actions.append(
        b("На карте", () => {
          Modules.open("galaxy");
          window.dispatchEvent(
            new CustomEvent("showstar", { detail: item.star }),
          );
        }),
        b("Подробнее", () => showSystem(item.star)),
      );
      card.append(n("span", s?.class || "✦", "study-star"), body, actions);
      list.append(card);
    }
    if (!items.length)
      UI.empty(list, "Пока нет исследований по выбранным условиям");
    pagination.replaceChildren(
      pager({
        page,
        pages,
        total: items.length,
        change: (value) => {
          page = value;
          entries();
        },
      }),
    );
  }

  function render() {
    if (!Session.signedIn || !ship() || !Modules.isOpen("studies")) return;
    layout();
    const e = Session.data.expedition,
      s = ship(),
      jobs = Session.data.workbench?.research || [],
      job = jobs.find((j) => j.ship === s.id && j.star === s.star),
      survey = known().find((item) => item.star === s.star),
      stats = root.querySelector(".studies-stats"),
      side = root.querySelector(".studies-current");
    stats.replaceChildren();
    for (const [label, value] of [
      ["Известных систем", known().length],
      ["Лично исследовано", known().filter(personal).length],
      [
        "Получено из разведданных",
        known().filter((item) => item.source === "purchased").length,
      ],
    ]) {
      const card = n("div");
      card.append(n("strong", String(value)), n("span", tr(label)));
      stats.append(card);
    }
    side.replaceChildren(n("p", tr("Текущая система"), "eyebrow"));
    const cover = n("div", undefined, "study-current-cover");
    cover.append(objectHologram({ kind: "Звезда" }, e.system));
    side.append(cover, n("h2", e.system.name));
    const status = n(
      "span",
      tr(
        survey
          ? "Система исследована"
          : job?.paused
            ? "Исследование приостановлено"
            : job
              ? "Исследование выполняется"
              : "Система не исследована",
      ),
      "study-status",
    );
    status.dataset.known = String(!!survey);
    side.append(status, n("p", tr(e.region), "muted"));
    if (survey) side.append(n("p", tr(source(survey)), "muted"));
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
        b(job.paused ? "Продолжить" : "Приостановить", () =>
          Game.command({
            action: job.paused ? "resume_research" : "pause_research",
            ship: s.id,
          }),
        ),
      );
    } else
      side.append(
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
    side.append(b("Подробнее", () => showSystem(s.star)));
    const active = n("section", undefined, "studies-active");
    active.append(n("h3", tr("Текущие экспедиции")));
    for (const j of jobs) {
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
    if (!jobs.length) UI.empty(active, "Нет активных экспедиций");
    side.append(active);
    entries();
  }
  window.addEventListener("moduleopen", async (event) => {
    if (event.detail !== "studies" || !Session.signedIn) return;
    const current = ++epoch;
    render();
    if (!world.data) {
      try {
        const map = await api("galaxy");
        if (current !== epoch || !Session.signedIn) return;
        world.data = map;
        render();
      } catch (error) {
        notify(error.message, "error");
      }
    }
  });
  window.addEventListener("gamedata", render);
  window.addEventListener("languagechange", () => {
    if (viewHandle) { viewHandle.close(); viewHandle = null; }
    root.replaceChildren();
    render();
  });
  window.addEventListener("signedout", () => {
    if (viewHandle) { viewHandle.close(); viewHandle = null; }
    epoch++;
    query = "";
    scope = "all";
    page = 1;
    root.replaceChildren();
    Modules.close("studies");
  });
}
