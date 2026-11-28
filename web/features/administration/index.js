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
  const root = Modules.create("administration", "Панель администратора", 1260, 820, { audience: "admin", initialWidthFloor: 1100, avoidSidebar: true });
  const tr = s => I18n.t(s), n = UI.node;
  const isAdmin = () => Session.signedIn && Session.data.state?.user.role === "admin";
  let selected = "services";
  let mounted;

  for (const [, , entries] of categories) {
    for (const [id] of entries) {
      Modules.routeEmbedded(id, () => {
        if (!isAdmin()) return;
        selected = id;
        return Modules.open("administration");
      });
    }
  }

  Modules.prepare("administration", () => {
    if (!isAdmin()) return;
    if (mounted) Modules.close(mounted);
    root.replaceChildren();
    root.classList.add("administration-content");
    const layout = n("div", undefined, "administration-layout");
    const nav = n("nav", undefined, "administration-categories");
    const panel = n("section", undefined, "administration-panel");
    const outlet = n("div", undefined, "administration-outlet");
    const title = n("h3");
    nav.setAttribute("aria-label", tr("Разделы управления"));
    panel.append(title, outlet);

    function show(id, label) {
      if (!isAdmin()) return;
      if (mounted && mounted !== id) Modules.close(mounted);
      selected = mounted = id;
      title.textContent = tr(label);
      for (const button of nav.querySelectorAll("[data-section]"))
        button.setAttribute("aria-current", String(button.dataset.section === id));
      return Modules.showEmbedded(id, outlet);
    }

    for (const [, group, entries] of categories) {
      const block = n("div", undefined, "administration-nav-group");
      block.append(n("p", tr(group)));
      for (const [id, label, description] of entries) {
        const button = UI.button(label, () => show(id, label));
        button.title = tr(description);
        button.dataset.section = id;
        block.append(button);
      }
      if (group === "Игровой мир") block.append(UI.button("Пропустить тик", async () => {
        const epoch = Session.epoch;
        await api("admin/tick", {});
        if (epoch !== Session.epoch) return;
        await refresh(true);
        if (epoch === Session.epoch && isAdmin()) notify("Тик выполнен");
      }));
      nav.append(block);
    }

    layout.append(nav, panel);
    root.append(layout);
    const item = categories.flatMap(([, , entries]) => entries).find(([id]) => id === selected);
    return show(item[0], item[1]);
  });
  window.addEventListener("moduleclose", ({detail}) => {
    if (detail === "administration" && mounted) {
      Modules.close(mounted);
      mounted = null;
    }
  });
  window.addEventListener("signedout", () => { selected = "services"; });
}
