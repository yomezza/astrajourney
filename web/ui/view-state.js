import { UI } from "./runtime.js";
export function reset(root) {
  UI.rememberFolds(root);
  const scroll = root.scrollTop,
    folds = new Map(
      [...root.querySelectorAll("details")].map((el, i) => [
        el.dataset.fold ||
          (el.querySelector("summary")?.textContent || "") + "|" + i,
        el.open,
      ]),
    );
  root.replaceChildren();
  queueMicrotask(() => {
    root.querySelectorAll("details").forEach((el, i) => {
      const key =
        el.dataset.fold ||
        (el.querySelector("summary")?.textContent || "") + "|" + i;
      if (folds.has(key)) el.open = folds.get(key);
    });
    root.scrollTop = scroll;
  });
}
