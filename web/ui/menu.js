import { UI } from "./runtime.js";
import { node } from "./dom.js";
import { I18n } from "../core/i18n.js";
import { notify } from "./feedback.js";
import { placeFloating, trackFloating, overlayHost } from "./floating.js";
export function initMenu() {
  const tr = (s) => I18n.t(s);
  const context = node("div", undefined, "context-menu");
  context.hidden = true;
  context.setAttribute("role", "menu");
  document.body.append(context);
  let anchor;
  const menus = [context];
  function closeAfter(menu) {
    while (menus.length > menus.indexOf(menu) + 1) {
      const sub = menus.pop();
      sub.opener?.setAttribute("aria-expanded", "false");
      sub.remove();
    }
  }
  function hideMenus() {
    menuTracking?.dispose();
    menuTracking = null;
    closeAfter(context);
    context.hidden = true;
    context.replaceChildren();
  }
  let menuTracking;
  function positionMenu(menu, x, y, parent) {
    menu.style.overflowY = "auto";
    placeFloating(menu, { x, y, parent });
  }
  function populateMenu(menu, items) {
    menu.replaceChildren();
    for (const item of items) {
      if (item.separator) {
        const line = node("hr");
        line.setAttribute("role", "separator");
        menu.append(line);
        continue;
      }
      if (item.heading) {
        menu.append(node("p", tr(item.heading), "context-heading"));
        continue;
      }
      const b = node("button", tr(item.label));
      b.type = "button";
      b.setAttribute("role", "menuitem");
      b.disabled = !!item.disabled;
      b.classList.toggle("danger-action", !!item.danger);
      if (typeof item.checked === "boolean") {
        b.setAttribute("role", "menuitemradio");
        b.setAttribute("aria-checked", String(item.checked));
        const check = node(
          "span",
          item.checked ? "✓" : "",
          "context-choice-check",
        );
        check.setAttribute("aria-hidden", "true");
        b.prepend(check);
      }

      if (item.reason) b.dataset.help = item.reason;
      if (item.children) {
        b.classList.add("context-parent");
        b.setAttribute("aria-label", tr(item.label));
        b.setAttribute("aria-haspopup", "menu");
        b.setAttribute("aria-expanded", "false");
        const reveal = () => {
          if (b.disabled) return;
          if (menus[menus.indexOf(menu) + 1]?.opener === b) return;
          closeAfter(menu);
          menu
            .querySelectorAll("[aria-expanded]")
            .forEach((e) => e.setAttribute("aria-expanded", "false"));
          const sub = node(
            "div",
            undefined,
            "context-menu context-submenu" +
              (context.classList.contains("chat-compact")
                ? " chat-compact"
                : ""),
          );
          sub.opener = b;
          sub.setAttribute("role", "menu");
          sub.onkeydown = menuKeys;
          populateMenu(sub, item.children);
          overlayHost(anchor).append(sub);
          menus.push(sub);
          b.setAttribute("aria-expanded", "true");
          const r = b.getBoundingClientRect();
          positionMenu(sub, r.right, r.top, r);
        };
        b.onpointerenter = reveal;
        b.onclick = () => {
          reveal();
          menus.at(-1).querySelector("button:not(:disabled)")?.focus();
        };
      } else {
        b.onpointerenter = () => closeAfter(menu);
        b.onclick = () => {
          hideMenus();
          Promise.resolve()
            .then(() => item.action?.())
            .catch((error) => {
              if (error.name !== "AbortError") notify(error.message, "error");
            });
        };
      }
      menu.append(b);
    }
  }
  function menuKeys(e) {
    const menu = e.currentTarget;
    if (e.key === "Escape") {
      hideMenus();
      anchor?.focus();
    }
    if (
      e.key === "ArrowRight" &&
      document.activeElement?.hasAttribute("aria-haspopup")
    ) {
      e.preventDefault();
      document.activeElement.click();
    }
    if (e.key === "ArrowLeft" && menu !== context) {
      e.preventDefault();
      const opener = menu.opener;
      closeAfter(menus[menus.indexOf(menu) - 1]);
      opener.setAttribute("aria-expanded", "false");
      opener.focus();
    }
    if (["ArrowDown", "ArrowUp"].includes(e.key)) {
      e.preventDefault();
      const buttons = [...menu.querySelectorAll("button:not(:disabled)")],
        i = buttons.indexOf(document.activeElement);
      buttons[
        (i + (e.key === "ArrowDown" ? 1 : -1) + buttons.length) % buttons.length
      ]?.focus();
    }
  }
  document.addEventListener("pointerdown", (e) => {
    if (!menus.some((menu) => menu.contains(e.target))) hideMenus();
  });
  context.onkeydown = menuKeys;
  window.addEventListener("signedout", hideMenus);
  UI.menu = function menu(items, x, y, options = {}) {
    hideMenus();
    anchor = options.owner || document.activeElement;
    context.classList.toggle(
      "chat-compact",
      options.theme === "chat" ||
        (!options.theme && !!anchor?.closest(".chat-root,#window-chat-roster")),
    );
    populateMenu(context, items);
    overlayHost(anchor).append(context);
    context.hidden = false;
    const position = () => {
      positionMenu(context, x, y);
      for (const sub of menus.slice(1)) {
        const r = sub.opener.getBoundingClientRect();
        positionMenu(sub, r.right, r.top, r);
      }
    };
    position();
    menuTracking = trackFloating({
      element: context,
      owner: anchor,
      position,
      close: hideMenus,
    });
    context.querySelector("button:not(:disabled)")?.focus();
  };
}
