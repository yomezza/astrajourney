import { I18n } from "../../core/i18n.js";
import { node } from "../../ui/dom.js";
import { lineIcon } from "../../ui/icons.js";
import { Modules } from "../../ui/runtime.js";
import { openModule } from "../shell/module-routes.js";

export function initStudioComms() {
  const rail = node("aside",undefined,"matrix-comms");
  document.body.append(rail);
  const entries = new Map();
  const home = document.querySelector(".home-lower-grid");
  const legacyDock = () => document.body.classList.contains("chat-docked");
  const title = {chat:"Чат",notifications:"Уведомления"};
  const tr = key => I18n.t(key);
  const dialog = id => Modules.header(id)?.closest("dialog");
  const docked = id => dialog(id)?.classList.contains("matrix-comms-docked");
  let previous;
  function sync() {
    rail.setAttribute("aria-label",tr("Связь"));
    for (const [id,entry] of entries) {
      const legacy = id === "chat" && legacyDock();
      const open = (legacy && !home.hidden) || dialog(id)?.open;
      entry.toggle.setAttribute("aria-pressed",String(!!open));
      entry.toggle.setAttribute("aria-label",tr(title[id]));
      entry.toggle.title = tr(title[id]);
      entry.move.hidden = !open || legacy;
      const label = tr(docked(id) ? "В отдельное окно" : "В правую панель");
      entry.move.title = label;
      entry.move.setAttribute("aria-label",tr(title[id]) + " · " + label);
      if (entry.badge) {
        const counter = document.getElementById("notification-count");
        const count = counter && !counter.hidden ? counter.textContent : "";
        entry.badge.hidden = !count;
        entry.badge.textContent = count;
      }
    }
    const mode = (docked("chat") && dialog("chat").open) || (docked("notifications") && dialog("notifications").open) ||
      (legacyDock() && !home.hidden);
    if (mode !== previous) {
      previous = mode;
      window.dispatchEvent(new Event("matrixcommslayout"));
    }
  }

  for (const id of Object.keys(title)) {
    const row = node("div",undefined,"matrix-comms-controls");
    const toggle = node("button");
    toggle.type = "button";
    toggle.setAttribute("aria-controls",`window-${id}`);
    toggle.append(lineIcon(id));
    const badge = id === "notifications" ? node("small",undefined,"matrix-comms-count") : null;
    if (badge) {
      badge.hidden = true;
      badge.setAttribute("aria-hidden","true");
      toggle.append(badge);
    }
    toggle.onclick = () => {
      const pane = dialog(id);
      if (!pane) return;
      if (id === "chat" && legacyDock()) {
        home.hidden = !home.hidden;
        document.querySelector('#sidebar [data-open="chat"]')?.classList.toggle("module-open",!home.hidden);
        sync();
        return;
      }
      if (pane.open) {
        Modules.close(id);
      } else {
        pane.classList.add("matrix-comms-docked");
        openModule(id);
      }
      sync();
    };
    const move = node("button",undefined,"matrix-comms-mode");
    move.type = "button";
    move.append(lineIcon("studio"));
    move.onclick = () => {
      dialog(id)?.classList.toggle("matrix-comms-docked");
      sync();
      Modules.focus(id);
    };
    row.append(toggle,move);
    entries.set(id,{toggle,move,badge});
    rail.append(row);
  }
  window.addEventListener("moduleopen",({detail}) => {
    if (entries.has(detail)) sync();
  });
  window.addEventListener("moduleclose",({detail}) => {
    if (entries.has(detail)) {
      dialog(detail)?.classList.remove("matrix-comms-docked");
      queueMicrotask(sync);
    }
  });
  for (const event of ["languagechange","gamedata","planetmodechange","signedout"])
    window.addEventListener(event,sync);
  const observed = new MutationObserver(sync);
  observed.observe(home,{attributes:true,attributeFilter:["hidden"]});
  const counters = document.querySelectorAll("#notification-count");
  for (const counter of counters)
    observed.observe(counter,{attributes:true,attributeFilter:["hidden"],childList:true,characterData:true,subtree:true});
  sync();
  return rail;
}
