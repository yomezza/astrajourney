import { UI } from "./runtime.js";
import { I18n } from "../core/i18n.js";

import { Session } from "../core/session.js";
import { Preferences } from "../core/preferences.js";

const n = (...args) => UI.node(...args),
  tr = (s) => I18n.t(s);
export function initDisclosure() {
  const folds = new Map();
  const saveFold = (d) => {
    folds.set(d.dataset.fold, d.open);
    if (d.dataset.foldStorage) {
      try {
        Preferences.write(d.dataset.foldStorage, d.open);
      } catch {}
    }
  };
  UI.rememberFolds = (root) =>
    root.querySelectorAll("details[data-fold]").forEach(saveFold);
  UI.fold = (key, opened = false, className, persistent = false) => {
    const details = n("details", undefined, className);
    details.dataset.fold = key;
    if (persistent) {
      details.dataset.foldStorage = `fold:${Session.data.state.user.id}:${key}`;
      if (!folds.has(key)) {
        try {
          const saved = Preferences.read(
            details.dataset.foldStorage,
            undefined,
            (v) => typeof v === "boolean",
          );
          if (typeof saved === "boolean") folds.set(key, saved);
        } catch {}
      }
    }
    details.open = folds.has(key) ? folds.get(key) : opened;
    details.addEventListener("toggle", () => {
      if (details.isConnected) saveFold(details);
    });
    return details;
  };
  window.addEventListener("signedout", () => folds.clear());
}
