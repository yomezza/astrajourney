import { I18n } from "../core/i18n.js";

// Auth and mode changes share the one existing startup-screen. Releasing an old
// token cannot close a later operation's overlay; auth has higher priority.
const entries = new Map();
let serial = 0;
function render() {
  const active = [...entries.values()].sort(
      (a, b) => b.priority - a.priority || b.serial - a.serial,
    )[0],
    root = document.getElementById("startup-screen");
  if (!root) return;
  root.hidden = !active;
  if (!active) return;
  root.classList.toggle("startup-failed", active.failed);
  const title = document.getElementById("startup-title"),
    message = document.getElementById("startup-message"),
    retry = document.getElementById("startup-retry");
  title.textContent = I18n.t(active.title);
  title.dataset.i18n = active.title;
  message.textContent = I18n.t(active.message);
  message.dataset.i18n = active.message;
  retry.hidden = !active.failed || !active.retry;
  retry.onclick = () => active.retry?.();
}
export const Startup = {
  acquire(owner, title, message = "", priority = 10) {
    const token = Symbol(owner),
      entry = {
        serial: ++serial,
        owner,
        title,
        message,
        priority,
        failed: false,
        retry: null,
      };
    entries.set(token, entry);
    render();
    return {
      update(title, message = "") {
        if (!entries.has(token)) return;
        Object.assign(entry, { title, message, failed: false });
        render();
      },
      fail(message, retry) {
        if (!entries.has(token)) return;
        Object.assign(entry, { failed: true, message, retry });
        render();
      },
      retry(callback) {
        entry.retry = callback;
        render();
      },
      release() {
        entries.delete(token);
        render();
      },
    };
  },
  clear(owner) {
    for (const [key, v] of entries) if (v.owner === owner) entries.delete(key);
    render();
  },
};
window.addEventListener("languagechange", render);
