import { UI } from "../../ui/runtime.js";
import { I18n } from "../../core/i18n.js";
const n = (...args) => UI.node(...args),
  tr = (s) => I18n.t(s);
export const serverStatus = (info) => {
  if (typeof info.version === "string" && /^\d+\.\d+\.\d+$/.test(info.version))
    for (const node of document.querySelectorAll("[data-game-version]"))
      node.textContent = "v" + info.version;

  const target = document.getElementById("server-status");
  target.dataset.help = "Сервер";
  target.tabIndex = 0;
  target.dataset.helpRaw =
    I18n.t("Сервер") +
    ": " +
    I18n.t(info.name || "Иггдрасиль") +
    "\n" +
    I18n.t("Игроки онлайн") +
    ": " +
    info.players +
    "\n" +
    I18n.t("Боты") +
    ": " +
    info.bots;
};
export function initServerStatus() {
  I18n.ready
    .then(() => fetch("/api/server-status"))
    .then((r) => (r.ok ? r.json() : Promise.reject()))
    .then(serverStatus)
    .catch(() => {});
}
