import { UI } from "../../ui/runtime.js";
import { I18n } from "../../core/i18n.js";
import { lineIcon } from "../../ui/icons.js";

export function initIcons() {
  for (const b of document.querySelectorAll("#sidebar [data-open]")) {
    const icon = b.querySelector(".nav-icon");
    const graphic = lineIcon(b.dataset.open);
    if (icon && graphic) icon.replaceChildren(graphic);
    b.dataset.help = b.dataset.nav || b.textContent.trim();
  }
  const bell = document.getElementById("notifications-button"),
    count = document.getElementById("notification-count");
  bell.replaceChildren(lineIcon("notifications"));
  bell.append(count);
  for (const host of document.querySelectorAll("[data-matrix-icon]"))
    host.replaceChildren(lineIcon(host.dataset.matrixIcon));
  const pass = document.querySelector("#auth-form input[name=password]"),
    wrap = UI.node("div", undefined, "password-wrap"),
    show = UI.button("◉", () => {
      pass.type = pass.type === "password" ? "text" : "password";
      show.setAttribute("aria-pressed", String(pass.type === "text"));
    });
  show.className = "password-toggle";
  show.replaceChildren(lineIcon("eye"));
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
