import { UI } from "./runtime.js";
import { I18n } from "../core/i18n.js";

import { Session } from "../core/session.js";

import { notify } from "./feedback.js";

const n = (...args) => UI.node(...args),
  tr = (s) => I18n.t(s);
export function bindAction(
  b,
  run,
  { disabled = b.disabled, reason = "" } = {},
) {
  let wanted = !!disabled,
    pending = false;
  const native = Object.getOwnPropertyDescriptor(
    HTMLButtonElement.prototype,
    "disabled",
  );
  const apply = () => native.set.call(b, wanted || pending);
  Object.defineProperty(b, "disabled", {
    configurable: true,
    get: () => native.get.call(b),
    set: (value) => {
      wanted = !!value;
      apply();
    },
  });
  apply();
  if (reason) b.dataset.help = reason;
  b.onclick = async (e) => {
    if (pending || wanted) return;
    const epoch = Session.epoch;
    const owner = b.actionOwner?.closest("dialog") || b.closest("dialog");
    pending = true;
    apply();
    b.classList.add("is-busy");
    b.setAttribute("aria-busy", "true");
    try {
      await run?.(e);
    } catch (error) {
      if (
        epoch === Session.epoch &&
        (!owner || (owner.open && owner.isConnected)) &&
        error.name !== "AbortError"
      )
        notify(error.message, "error");
    } finally {
      pending = false;
      apply();
      b.classList.remove("is-busy");
      b.removeAttribute("aria-busy");
    }
  };

  return b.onclick;
}
export function initActions() {
  UI.button = (label, run, disabled = false, reason = "") => {
    const b = n("button", tr(label));
    b.type = "button";
    if (typeof label === "string" && label.length > 1)
      b.classList.add("terminal-action");
    bindAction(b, run, { disabled, reason });
    return b;
  };
}
