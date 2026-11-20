import { Session } from "../../core/session.js";
import { I18n } from "../../core/i18n.js";
import { Modules } from "../../ui/runtime.js";
import { node } from "../../ui/dom.js";
import { lineIcon } from "../../ui/icons.js";

const groups = [
  ["Навигация и флот", [
    ["galaxy","Галактическая карта"], ["system","Карта текущей системы"],
    ["ship","Корабль"], ["cargo","Трюм"], ["portal","Портал Эдема"],
  ]],
  ["Операции", [
    ["research","Добыча"], ["tasks","Планировщик задач"],
    ["logistics","Логистика"], ["market","Рынок"], ["finance","История финансов"],
  ]],
  ["Развитие", [
    ["studies","Исследования"], ["colonies","Колонии"],
    ["corporations","Корпорация"], ["guide","Руководство пилота"],
  ]],
  ["Связь и настройки", [
    ["chat","Чат"], ["notifications","Уведомления"],
    ["profile","Профиль игрока"], ["settings","Настройки"], ["ideas","Предложить идею"],
  ]],
];
const tr = text => I18n.t(text);

export function initModuleCatalog() {
  const trigger = document.getElementById("matrix-catalog-shortcut");
  const mobile = document.getElementById("matrix-mobile");
  const dialog = node("dialog", undefined, "matrix-launcher");
  dialog.setAttribute("aria-labelledby","matrix-launcher-title");
  const header = node("div",undefined,"matrix-launcher-head");
  const title = node("h2");
  title.id = "matrix-launcher-title";
  const close = node("button","×");
  close.replaceChildren(lineIcon("close"));
  close.type = "button";
  close.onclick = () => dialog.close();
  header.append(title,close);
  const search = node("input");
  search.type = "search";
  search.autocomplete = "off";
  const results = node("div",undefined,"matrix-launcher-results");
  dialog.append(header,search,results);
  document.body.append(dialog);
  dialog.addEventListener("keydown", event => {
    if (event.key !== "Escape") return;
    event.preventDefault();
    event.stopPropagation();
    dialog.close();
  });

  function launch(id) {
    dialog.close();
    if (id === "system") {
      document.getElementById("current-system-map").click();
      return;
    }
    if (id === "colonies") {
      document.getElementById("nav-colonies").click();
      return;
    }
    if (Modules.isOpen(id)) Modules.focus(id);
    else {
      Modules.open(id);
      if (id === "galaxy") window.dispatchEvent(new Event("showgalaxy"));
    }
  }
  function paint() {
    const term = search.value.trim().toLocaleLowerCase();
    let sections = groups.slice();
    if (Session.data.state?.user.role === "admin")
      sections.push(["Управление", [["administration","Панель администратора"]]]);
    const items = new Map(sections.flatMap(([,entries]) => entries.map(entry => [entry[0],entry])));
    const open = Modules.openWindows().map(({id,title}) => items.get(id) || [id,title]);
    if (open.length) {
      const openIDs = new Set(open.map(([id]) => id));
      sections = [
        ["Открытые окна",open],
        ...sections.map(([label,entries]) => [label,entries.filter(([id]) => !openIDs.has(id))]),
      ];
    }
    results.replaceChildren();
    for (const [label, entries] of sections) {
      const matches = entries.filter(([,name]) => tr(name).toLocaleLowerCase().includes(term));
      if (!matches.length) continue;
      const section = node("section",undefined,"matrix-launcher-group");
      section.append(node("h3",tr(label)));
      const cards = node("div",undefined,"matrix-launcher-cards");
      for (const [id,name] of matches) {
        const card = node("button",undefined,"matrix-launcher-card");
        card.type = "button";
        if (id === "administration") card.dataset.admin = "";
        const icon = node("span",undefined,"matrix-launcher-icon");
        icon.append(lineIcon(id) || lineIcon("maximize"));
        card.append(icon,node("span",tr(name)));
        card.onclick = () => launch(id);
        cards.append(card);
      }
      section.append(cards);
      results.append(section);
    }
    if (!results.children.length) results.append(node("p",tr("Нет совпадений"),"muted"));
  }
  function show() {
    if (!Session.signedIn || document.querySelector(":modal") || document.body.classList.contains("planet-mode")) return;
    title.textContent = tr("Каталог игровых модулей");
    search.placeholder = tr("Поиск модулей");
    search.setAttribute("aria-label",tr("Поиск модулей"));
    close.setAttribute("aria-label",tr("Закрыть"));
    search.value = "";
    paint();
    dialog.showModal();
    search.focus();
  }
  trigger.onclick = show;
  search.oninput = paint;
  search.onkeydown = event => {
    if (event.key === "ArrowDown") {
      event.preventDefault();
      results.querySelector("button")?.focus();
    }
    if (event.key === "Enter") results.querySelector("button")?.click();
  };
  results.addEventListener("keydown", event => {
    if (!["ArrowUp","ArrowDown","Home","End"].includes(event.key)) return;
    const cards = [...results.querySelectorAll("button")];
    const index = cards.indexOf(event.target);
    if (index < 0) return;
    event.preventDefault();
    if (event.key === "Home") cards[0].focus();
    else if (event.key === "End") cards.at(-1).focus();
    else if (event.key === "ArrowUp") (cards[index - 1] || search).focus();
    else cards[Math.min(index + 1, cards.length - 1)].focus();
  });
  mobile.querySelector("[data-launcher]").onclick = show;
  mobile.querySelector("[data-studio]").onclick = () => {
    const studio = document.getElementById("studio-shortcut");
    if (!studio.hidden) studio.click();
  };
  window.addEventListener("keydown", event => {
    if (event.defaultPrevented || !(event.ctrlKey || event.metaKey) || event.key.toLowerCase() !== "k") return;
    if (!Session.signedIn || document.body.classList.contains("planet-mode")) return;
    event.preventDefault();
    show();
  });
  window.addEventListener("gamedata", () => {
    trigger.hidden = mobile.hidden = !Session.signedIn;
    if (dialog.open && Session.data.state?.user.role !== "admin" && results.querySelector("[data-admin]")) paint();
  });
  window.addEventListener("moduleclose", () => { if (dialog.open) paint(); });
  window.addEventListener("languagechange", () => { if (dialog.open) paint(); });
  window.addEventListener("planetmodechange", ({detail}) => {
    if (detail && dialog.open) dialog.close();
  });
  window.addEventListener("signedout", () => {
    if (dialog.open) dialog.close();
    trigger.hidden = mobile.hidden = true;
    results.replaceChildren();
  });
}
