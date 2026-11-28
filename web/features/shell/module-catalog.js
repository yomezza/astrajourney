import { Session } from "../../core/session.js";
import { I18n } from "../../core/i18n.js";
import { Modules } from "../../ui/runtime.js";
import { node } from "../../ui/dom.js";
import { lineIcon } from "../../ui/icons.js";
import { openIntent, openModule } from "./module-routes.js";

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

const commands = new Map([
  ["FLEET",["overview","Флот"]],
  ["SHIP",["fleet","Корабль","ship"]], ["КОРАБЛЬ",["fleet","Корабль","ship"]],
  ["CARGO",["cargo","Трюм","ship"]], ["ТРЮМ",["cargo","Трюм","ship"]],
  ["MAP",["map","Галактическая карта"]],
  ["SYSTEM",["map","Галактическая карта","system"]], ["СИСТЕМА",["map","Галактическая карта","system"]],
  ["MARKET",["market","Рынок","ship"]], ["РЫНОК",["market","Рынок","ship"]],
  ["MINING",["mining","Добыча","ship"]], ["ДОБЫЧА",["mining","Добыча","ship"]],
  ["LOGISTICS",["logistics","Логистика","ship"]],
  ["TASKS",["tasks","Планировщик задач","ship"]],
  ["FINANCE",["finance","История финансов","global"]],
  ["HELP",["guide","Руководство пилота","global"]],
]);

function commandChoices(text) {
  const [verb, argument] = text.trim().split(/\s+/,2);
  const entry = commands.get(verb?.toLocaleUpperCase());
  // Keep localized catalogue search available alongside explicit commands.
  if (!entry || (!argument && verb !== verb.toLocaleUpperCase())) return null;
  const [module,label,targetKind] = entry;
  const scope = targetKind === "global" ? "global" : "workspace";
  const id = /^[1-9]\d*$/.test(argument || "") ? Number(argument) : 0;
  const intent = {module,scope};
  if (argument) {
    if (!["ship","system"].includes(targetKind) || !Number.isSafeInteger(id) || !id ||
        text.trim().split(/\s+/).length !== 2)
      return {error:"Укажите числовой ID объекта"};
    return {items:[{label:tr(label) + " · #" + id,intent:{...intent,target:{kind:targetKind,id}}}]};
  }
  if (targetKind === "ship") {
    const ships = Session.data.fleet?.ships || [];
    if (!ships.length) return {error:"Корабль недоступен"};
    return {items:ships.map(ship => ({
      label:tr(label) + " · " + ship.name + " #" + ship.id,
      intent:{...intent,target:{kind:"ship",id:ship.id}},
    }))};
  }
  if (targetKind === "system") return {error:"Укажите числовой ID объекта"};
  return {items:[{label:tr(label),intent}]};
}

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

  function paint() {
    const focused = results.contains(document.activeElement) && document.activeElement.dataset.route;
    const term = search.value.trim().toLocaleLowerCase();
    let sections = groups.slice();
    if (Session.data.state?.user.role === "admin")
      sections.push(["Управление", [["administration","Панель администратора"]]]);
    const items = new Map(sections.flatMap(([,entries]) => entries.map(entry => [entry[0],entry])));
    const open = Modules.openWindows().map(({id,title}) =>
      id === "galaxy" ? [id,"Открытая карта"] : [id,title,items.get(id)?.[1]]);
    if (open.length) {
      const openIDs = new Set(open.map(([id]) => id));
      sections = [
        ["Открытые окна",open],
        ...sections.map(([label,entries]) => [label,entries.filter(([id]) => id === "galaxy" || !openIDs.has(id))]),
      ];
    }
    results.replaceChildren();
    const choices = commandChoices(search.value);
    if (choices) {
      if (choices.error) results.append(node("p",tr(choices.error),"muted"));
      else {
        const section = node("section",undefined,"matrix-launcher-group");
        section.append(node("h3",tr("Команды")));
        const cards = node("div",undefined,"matrix-launcher-cards");
        for (const {label,intent} of choices.items) {
          const card = node("button",label,"matrix-launcher-card");
          card.type = "button";
          card.onclick = () => {
            dialog.close();
            openIntent(intent);
          };
          cards.append(card);
        }
        section.append(cards);
        results.append(section);
      }
      return;
    }
    for (const [label, entries] of sections) {
      const windowGroup = label === "Открытые окна";
      const matches = entries.filter(([,name,alias]) =>
        tr(name).toLocaleLowerCase().includes(term) ||
        (alias && tr(alias).toLocaleLowerCase().includes(term)));
      if (!matches.length) continue;
      const section = node("section",undefined,"matrix-launcher-group");
      section.append(node("h3",tr(label)));
      const cards = node("div",undefined,"matrix-launcher-cards");
      for (const [id,name] of matches) {
        const card = node("button",undefined,"matrix-launcher-card");
        card.type = "button";
        card.dataset.route = windowGroup ? `window-${id}` : id;
        if (id === "administration") card.dataset.admin = "";
        const icon = node("span",undefined,"matrix-launcher-icon");
        icon.append(lineIcon(id) || lineIcon("maximize"));
        card.append(icon,node("span",tr(name)));
        card.onclick = () => {
          dialog.close();
          if (windowGroup) openIntent({module:id,scope:"existing"});
          else openModule(id);
        };
        cards.append(card);
      }
      section.append(cards);
      results.append(section);
    }
    if (!results.children.length) results.append(node("p",tr("Нет совпадений"),"muted"));
    if (focused) (results.querySelector(`[data-route="${CSS.escape(focused)}"]`) || search).focus();
  }
  function show() {
    if (!Session.signedIn || document.querySelector(":modal") || document.body.classList.contains("planet-mode")) return;
    title.textContent = tr("Каталог игровых модулей");
    search.placeholder = tr("Поиск модулей");
    search.setAttribute("aria-label",tr("Поиск модулей"));
    close.setAttribute("aria-label",tr("Закрыть"));
    search.value = "";
    search.title = "FLEET · SHIP 123 · CARGO 123 · SYSTEM 42 · MARKET · TASKS";
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
