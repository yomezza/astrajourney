import { Session } from "../../core/session.js";
import { api } from "../../core/api.js";
import { refresh } from "../../core/snapshot-store.js";
import { I18n } from "../../core/i18n.js";
import { Modules, UI } from "../../ui/runtime.js";
import { notify } from "../../ui/feedback.js";

const categories = [
  ["operations", "Службы и диагностика", [
    ["services", "Статус служб", "Доступность служб и соединений"],
    ["benchmark", "Бенчмарк", "Измерение производительности"],
  ]],
  ["world", "Игровой мир", [
    ["gameplay", "Игровой процесс", "Параметры игрового процесса"],
    ["galaxies", "Галактики", "Создание и управление галактиками"],
    ["resourcegrant", "Выдача ресурсов", "Ресурсы игроков и кораблей"],
  ]],
  ["community", "Игроки и контент", [
    ["users", "Пользователи", "Учётные записи и права доступа"],
    ["adminideas", "Предложения игроков", "Обратная связь и обсуждения"],
    ["guide-admin", "Управление базой знаний", "Разделы и статьи руководства"],
  ]],
  ["audit", "История изменений", [
    ["adminchanges", "Журнал изменений", "История действий администратора"],
  ]],
];

export function initAdministration() {
  const root = Modules.create("administration", "Панель администратора", 920, 650, { audience: "admin" });
  let selected = "operations";
  Modules.prepare("administration", () => {
    if (!Session.signedIn || Session.data.state?.user.role !== "admin") return;
    const tr = (s) => I18n.t(s), n = UI.node;
    root.replaceChildren();
    root.classList.add("administration-content");
    const heading = n("div", undefined, "administration-heading");
    heading.append(n("span", "⌘", "administration-emblem"), n("div"));
    heading.lastChild.append(n("h3", tr("Центр управления")));
    const layout = n("div", undefined, "administration-layout"), nav = n("nav", undefined, "administration-categories"), panel = n("section", undefined, "administration-panel");
    nav.setAttribute("aria-label", tr("Категории управления"));
    for (const [id, title] of categories) {
      const button = UI.button(title, () => { selected = id; renderCategory(); });
      button.dataset.category = id;
      nav.append(button);
    }
    function renderCategory() {
      const [, title, entries] = categories.find(([id]) => id === selected);
      for (const b of nav.children) b.setAttribute("aria-pressed", String(b.dataset.category === selected));
      panel.replaceChildren(n("h3", tr(title)));
      const cards = n("div", undefined, "administration-cards");
      for (const [id, label, description] of entries) {
        const card = n("button", undefined, "administration-card");
        card.type = "button";
        card.dataset.open = id;
        card.append(n("strong", tr(label)), n("span", tr(description)), n("span", "↗", "administration-arrow"));
        cards.append(card);
      }
      panel.append(cards);
      if (selected === "world") panel.append(UI.button("Пропустить тик", async () => {
        const epoch = Session.epoch;
        await api("admin/tick", {});
        if (epoch !== Session.epoch) return;
        await refresh(true);
        if (epoch === Session.epoch && Session.data.state?.user.role === "admin") notify("Тик выполнен");
      }));
    }
    layout.append(nav, panel);
    root.append(heading, layout);
    renderCategory();
  });
  window.addEventListener("languagechange", () => {
    if (Modules.isOpen("administration")) Modules.open("administration");
  });
}
