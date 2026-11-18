import { UI } from "./runtime.js";
import { Session } from "../core/session.js";
import { I18n } from "../core/i18n.js";

// One readiness boundary per window. Renderers return their work through
// Modules.prepare/Modules.wait; the shell also waits for eager DOM images.
export function moduleLoading(dialog, content) {
  const overlay = document.createElement("section");
  overlay.className = "module-loading";
  overlay.hidden = true;
  overlay.setAttribute("role", "status");
  overlay.setAttribute("aria-live", "polite");
  const card = document.createElement("div");
  card.className = "startup-card";
  const mark = document.createElement("div");
  mark.className = "startup-mark";
  mark.setAttribute("aria-hidden", "true");
  const label = document.createElement("p");
  label.className = "startup-label";
  label.textContent = "ASTRA JOURNEY";
  const title = document.createElement("h3");
  const message = document.createElement("p");
  message.className = "module-loading-message";
  const retry = document.createElement("button");
  retry.type = "button";
  card.append(mark, label, title, message, retry);
  overlay.append(card);
  dialog.append(overlay);
  let current, background, serial = 0, errorText = "";
  function paint() {
    title.textContent = I18n.t(errorText ? "Не удалось загрузить модуль" : "Загрузка модуля");
    message.textContent = I18n.t(errorText || "Загружаем данные и оформление…");
    retry.textContent = I18n.t("Повторить попытку");
    retry.hidden = !errorText;
    overlay.classList.toggle("startup-failed", !!errorText);
  }
  function visible(value) {
    overlay.hidden = !value;
    dialog.classList.toggle("module-pending", value);
    content.inert = value;
    content.setAttribute("aria-busy", String(value));
  }
  function cancel() {
    current?.controller.abort();
    background?.controller.abort();
    background = null;
    current = null;
    visible(false);
  }
  function wait(task) {
    const promise = Promise.resolve(task);
    // Also consume rejected tasks if a close cancels the readiness boundary.
    promise.catch(() => {});
    current?.pending.push(promise);
    return promise;
  }
  async function images(signal) {
    for (const img of content.querySelectorAll("img")) {
      if (signal.aborted) throw new DOMException("Closed", "AbortError");
      if (img.loading === "lazy") continue;
      if (!img.complete) await new Promise((resolve, reject) => {
        const finish = () => { cleanup(); resolve(); };
        const abort = () => { cleanup(); reject(new DOMException("Closed", "AbortError")); };
        const cleanup = () => {
          img.removeEventListener("load", finish);
          img.removeEventListener("error", finish);
          signal.removeEventListener("abort", abort);
        };
        img.addEventListener("load", finish, { once: true });
        img.addEventListener("error", finish, { once: true });
        signal.addEventListener("abort", abort, { once: true });
      });
      // Give a renderer's error handler time to assign its fallback URL.
      if (img.src && typeof img.decode === "function") await img.decode();
    }
  }
  async function run(prepare) {
    cancel();
    const state = { controller: new AbortController(), pending: [], epoch: Session.epoch, serial: ++serial };
    current = state;
    errorText = "";
    paint();
    visible(true);
    retry.onclick = () => run(prepare);
    const signal = state.controller.signal;
    const timer = setTimeout(() => state.controller.abort(new Error(I18n.t("Превышено время ожидания загрузки"))), 30000);
    try {
      const aborted = new Promise((_, reject) => signal.addEventListener("abort", () => reject(signal.reason), { once: true }));
      const work = async () => {
        await prepare(signal);
        for (let i = 0; i < state.pending.length;) {
          const batch = state.pending.slice(i);
          i = state.pending.length;
          await Promise.all(batch);
        }
        await UI.moduleAssets?.(content);
        await images(signal);
      };
      await Promise.race([work(), aborted]);
      if (current === state && state.epoch === Session.epoch && !signal.aborted) {
        visible(false);
        current = null;
        dialog.dispatchEvent(new CustomEvent("moduleready", { bubbles: true }));
      }
    } catch (error) {
      if (current !== state || state.epoch !== Session.epoch || error?.name === "AbortError") return;
      errorText = error?.message || "Не удалось загрузить модуль";
      paint();
    } finally { clearTimeout(timer); }
  }
  function refresh(prepare) {
    if (!dialog.open || current) return Promise.resolve(false);
    if (background) {
      background.prepare = prepare;
      background.again = true;
      return background.promise;
    }
    const state = {controller:new AbortController(), epoch:Session.epoch, prepare, again:true};
    background = state;
    const signal = state.controller.signal;
    const timer = setTimeout(() => state.controller.abort(), 30000);
    const work = Promise.resolve().then(async () => {
      let accepted;
      do {
        state.again = false;
        if (signal.aborted || state.epoch !== Session.epoch || !dialog.open) return false;
        accepted = await state.prepare(signal);
        if (signal.aborted || state.epoch !== Session.epoch || !dialog.open) return false;
        if (accepted !== false) {
          await UI.moduleAssets?.(content);
          await images(signal);
        }
      } while (state.again && !signal.aborted);
      return !signal.aborted && accepted !== false;
    });
    let abort;
    const aborted = new Promise(resolve => {
      abort = () => resolve(false);
      signal.addEventListener("abort", abort, {once:true});
    });
    state.promise = Promise.race([work, aborted]).catch(error => {
      if (signal.aborted || state.epoch !== Session.epoch || error?.name === "AbortError") return false;
      throw error;
    }).finally(() => {
      clearTimeout(timer);
      signal.removeEventListener("abort", abort);
      if (background === state) background = null;
    });
    return state.promise;
  }
  const language = () => paint();
  window.addEventListener("languagechange", language);
  return { run, refresh, wait, cancel, get pending() { return !!current; }, get signal() { return current?.controller.signal; }, dispose() { cancel(); window.removeEventListener("languagechange", language); } };
}
