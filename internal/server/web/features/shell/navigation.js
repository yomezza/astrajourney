import { Modules } from "../../ui/runtime.js";
import { I18n } from "../../core/i18n.js";
import { Preferences } from "../../core/preferences.js";
import { node } from "../../ui/dom.js";
import { arrangeShip } from "../fleet/layout.js";
const tr = (s) => I18n.t(s),
  read = (key, value) => Preferences.read(key, value);
export function initNavigation() {
  const maps = document.getElementById("maps-navigation");
  maps.open = read("maps-navigation-open", false) === true;
  maps.addEventListener("toggle", () => {
    try {
      Preferences.write("maps-navigation-open", maps.open);
    } catch {}
  });
  maps.querySelector("summary").onclick = (event) => {
    event.preventDefault();
    maps.open = !maps.open;
    try {
      Preferences.write("maps-navigation-open", maps.open);
    } catch {}
  };
  document.getElementById("current-system-map").onclick = () => {
    Modules.open("galaxy");
    window.dispatchEvent(new Event("showcurrentsystem"));
  };
  for (const [id, title, w, h] of [
    ["galaxy", "Галактическая карта", 1180, 700],
    ["ship", "Корабль", 860, 660],
    ["cargo", "Трюм", 360, 590],
    ["research", "Добыча", 1000, 660],
    ["market", "Рынок", 1060, 660],
    ["corporations", "Корпорация", 1000, 660],
    ["profile", "Профиль игрока", 480, 420],
    ["settings", "Настройки", 480, 430],
    ["users", "Пользователи", 920, 620],
    ["gameplay", "Игровой процесс", 800, 700],
    ["galaxies", "Галактики", 1120, 760],
    ["adminchanges", "Журнал изменений", 980, 650],
    ["notifications", "Уведомления", 420, 540],
    ["guide", "Руководство пилота", 950, 650],
    ["tasks", "Планировщик задач", 900, 670],
    ["finance", "История финансов", 820, 580],
    ["benchmark", "Бенчмарк", 960, 660],
    ["services", "Статус служб", 820, 600],
    ["portal", "Портал Эдема", 700, 560],
  ])
    Modules.create(id, title, w, h, {
      private: id !== "galaxy" && id !== "settings",
      minContentWidth: id === "notifications" ? 300 : undefined,
      audience: [
        "users",
        "gameplay",
        "galaxies",
        "adminchanges",
        "benchmark",
        "services",
      ].includes(id)
        ? "admin"
        : undefined,
    });
  let arrangeTimer;
  window.addEventListener("resize", () => {
    clearTimeout(arrangeTimer);
    arrangeTimer = setTimeout(() => arrangeShip(), 80);
  });
  const settings = document.querySelector("#window-settings .module-content");
  settings.append(node("h3", tr("Язык")));
  const language = node("select");
  language.id = "language";
  for (const [v, label] of [
    ["ru", "Русский"],
    ["en", "English"],
  ]) {
    const option = node("option", label);
    option.value = v;
    language.append(option);
  }
  settings.append(language);
  language.onchange = () => I18n.set(language.value);
  settings.append(node("h3", tr("Отображение")));
  const density = node("select");
  for (const [v, label] of [
    ["normal", "Обычный интерфейс"],
    ["compact", "Компактный интерфейс"],
  ]) {
    const o = node("option", tr(label));
    o.value = v;
    density.append(o);
  }
  density.value = Preferences.read("density", "normal", (v) =>
    ["normal", "compact"].includes(v),
  );
  document.body.classList.toggle("compact-ui", density.value === "compact");
  density.onchange = () => {
    Preferences.write("density", density.value);
    document.body.classList.toggle("compact-ui", density.value === "compact");
  };
  settings.append(
    density,
    node(
      "p",
      tr("Плотность меняет размеры элементов во всех рабочих модулях."),
      "muted",
    ),
    node("h3", tr("Управление окнами")),
  );
  const reset = node("button", tr("Восстановить расположение окон"));
  reset.onclick = () => {
    Modules.resetLayout();
    location.reload();
  };
  settings.append(reset);
  for (const el of settings.querySelectorAll("h3,p,button"))
    el.dataset.i18n = el.textContent;
  window.addEventListener("languagechange", () => {
    Modules.refreshTitles();
  });
  document.addEventListener("click", (e) => {
    const b = e.target.closest("[data-open]");
    if (b) {
      Modules.open(b.dataset.open);
      if (b.dataset.open === "galaxy")
        window.dispatchEvent(new Event("showgalaxy"));
      if (b.closest("#profile-menu"))
        document.getElementById("profile-menu").open = false;
    }
    for (const menu of document.querySelectorAll(".dropdown"))
      if (!menu.contains(e.target)) menu.open = false;
  });
  const sidebar = document.getElementById("sidebar");
  sidebar.classList.toggle("collapsed", read("sidebar-collapsed", false));
  const sidebarToggle = document.getElementById("sidebar-toggle");
  const updateSidebar = () => {
    const collapsed = sidebar.classList.contains("collapsed");
    sidebarToggle.setAttribute("aria-expanded", String(!collapsed));
    sidebarToggle.setAttribute(
      "aria-label",
      I18n.t(collapsed ? "Развернуть панель" : "Свернуть панель"),
    );
    sidebarToggle.dataset.help = collapsed
      ? "Развернуть панель"
      : "Свернуть панель";
  };
  sidebarToggle.onclick = () => {
    sidebar.classList.toggle("collapsed");
    Preferences.write(
      "sidebar-collapsed",
      sidebar.classList.contains("collapsed"),
    );
    updateSidebar();
  };
  window.addEventListener("languagechange", updateSidebar);
  updateSidebar();
  I18n.ready.then(updateSidebar);
}
