import { Session } from "../../core/session.js";
import { Startup } from "../../ui/startup.js";

let overlay = null,
  host = null,
  focus = null;
const inert = new Map();
export const PlanetMode = {
  get active() {
    return document.body.classList.contains("planet-mode");
  },
  host() {
    if (!host) {
      host = document.createElement("section");
      host.id = "planet-host";
      host.setAttribute("aria-label", "Planet Mode");
      host.hidden = true;
      document.body.append(host);
    }
    return host;
  },
  enter() {
    if (!Session.signedIn) return;
    this.host();
    if (!this.active) {
      focus = document.activeElement;
      for (const element of document.querySelectorAll(
        "body > header,#game,.module-window",
      )) {
        if (element.id === "window-chat") continue;
        inert.set(element, element.inert);
        element.inert = true;
      }
      document.body.classList.add("planet-mode");
      window.dispatchEvent(
        new CustomEvent("planetmodechange", { detail: true }),
      );
    }
    host.hidden = false;
  },
  loading(title, retry) {
    this.enter();
    overlay?.release();
    overlay = Startup.acquire(
      "planet",
      title,
      "Подтверждаем состояние на сервере…",
    );
    overlay.retry(retry);
  },
  failed(message, retry) {
    if (!this.active) return;
    overlay?.release();
    overlay = Startup.acquire("planet", "Планетарный режим", message);
    overlay.fail(message, retry);
  },
  ready() {
    overlay?.release();
    overlay = null;
  },
  leave() {
    const wasActive = this.active;
    this.ready();
    host && (host.hidden = true);
    document.body.classList.remove("planet-mode");
    for (const [element, value] of inert) {
      if (element.isConnected) element.inert = value;
    }
    inert.clear();
    if (wasActive)
      window.dispatchEvent(
        new CustomEvent("planetmodechange", { detail: false }),
      );
    if (focus?.isConnected) focus.focus({ preventScroll: true });
    focus = null;
    window.dispatchEvent(new Event("gamedata"));
  },
};
document.addEventListener(
  "keydown",
  (event) => {
    if (
      PlanetMode.active &&
      !event.target.closest?.(
        "#planet-host,#window-chat,#startup-screen,.context-menu",
      )
    )
      event.stopImmediatePropagation();
  },
  true,
);
window.addEventListener("signedout", () => {
  Startup.clear("planet");
  PlanetMode.leave();
  host?.replaceChildren();
});
