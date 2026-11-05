import { UI } from "../../ui/runtime.js";
import { I18n } from "../../core/i18n.js";
const n = (...args) => UI.node(...args),
  tr = (s) => I18n.t(s);
export function initIcons() {
  const paths = {
    chat: "M5 5h14v11H9l-4 3V5zM8 9h8M8 13h5",
    galaxy: "M12 3v18M3 12h18M5 5l14 14M5 19L19 5",
    ship: "M3 17L21 3l-6 18-4-8-8 4z",
    research: "M4 20l9-9m-6-7c5-2 10 1 13 6l-4 3c-1-4-3-6-7-6z",
    market: "M3 7h16l-4-4m6 14H5l4 4",
    corporations: "M12 2l9 5v10l-9 5-9-5V7zM8 9h8v7H8z",
    tasks: "M8 6h13M8 12h13M8 18h13M3 6h1M3 12h1M3 18h1",
    studies:
      "M12 2v4M12 18v4M2 12h4M18 12h4M7 5l2 3M17 5l-2 3M7 19l2-3M17 19l-2-3M7 12a5 5 0 1 0 10 0a5 5 0 1 0-10 0",
    ideas: "M12 3a6 6 0 0 1 4 10l-1 2H9l-1-2a6 6 0 0 1 4-10zM9 18h6M10 21h4",
    guide: "M3 4h7l2 2 2-2h7v15h-7l-2 2-2-2H3zM12 6v15",
    portal:
      "M12 4c5 0 9 4 9 8s-4 8-9 8-9-4-9-8 4-8 9-8zM8 12c0-3 2-5 4-5s4 2 4 5-2 5-4 5-4-2-4-5z",
  };
  const svg = (path) => {
    const s = document.createElementNS("http://www.w3.org/2000/svg", "svg");
    s.setAttribute("viewBox", "0 0 24 24");
    s.setAttribute("aria-hidden", "true");
    const p = document.createElementNS(s.namespaceURI, "path");
    p.setAttribute("d", path);
    s.append(p);
    return s;
  };
  for (const b of document.querySelectorAll("#sidebar [data-open]")) {
    const icon = b.querySelector(".nav-icon");
    if (icon && paths[b.dataset.open])
      icon.replaceChildren(svg(paths[b.dataset.open]));
    b.dataset.help = b.dataset.nav || b.textContent.trim();
  }
  const bell = document.getElementById("notifications-button"),
    count = document.getElementById("notification-count");
  bell.replaceChildren(svg("M6 8a6 6 0 0 1 12 0v6l3 3H3l3-3V8M9 21h6"));
  bell.append(count);
  const pass = document.querySelector("#auth-form input[name=password]"),
    wrap = UI.node("div", undefined, "password-wrap"),
    show = UI.button("◉", () => {
      pass.type = pass.type === "password" ? "text" : "password";
      show.setAttribute("aria-pressed", String(pass.type === "text"));
    });
  show.className = "password-toggle";
  const passwordLabel = () => {
    show.setAttribute(
      "aria-label",
      I18n.t(pass.type === "password" ? "Показать пароль" : "Скрыть пароль"),
    );
  };
  passwordLabel();
  show.addEventListener("click", passwordLabel);
  window.addEventListener("languagechange", passwordLabel);
  I18n.ready.then(passwordLabel);
  show.setAttribute("aria-pressed", "false");
  pass.replaceWith(wrap);
  wrap.append(pass, show);
  I18n.ready.then(() => {
    for (const select of document.querySelectorAll("#window-settings select")) {
      const picker = UI.picker(
        [...select.options].map((o) => [o.value, o.textContent]),
        select.value,
        (value) => {
          select.value = value;
          select.onchange?.();
        },
      );
      select.hidden = true;
      select.after(picker);
    }
  });
}
