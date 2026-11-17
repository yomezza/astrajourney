import { I18n } from "../core/i18n.js";
import { placeFloating, trackFloating, overlayHost } from "./floating.js";

const providers = [
  {
    selector: "[data-help]",
    render: (target) => target.dataset.helpRaw || I18n.t(target.dataset.help),
  },
];
let active, tracking;
export function registerTooltip(selector, render) {
  providers.unshift({ selector, render });
}
export function initTooltip() {
  const tip = document.getElementById("tooltip");
  function hide() {
    tracking?.dispose();
    tracking = null;
    active = null;
    tip.hidden = true;
    tip.replaceChildren();
  }
  function show(event) {
    for (const provider of providers) {
      const target = event.target.closest?.(provider.selector);
      if (!target) continue;
      const content = provider.render(target);
      if (content === undefined || content === null) continue;
      hide();
      active = target;
      tip.replaceChildren(
        content instanceof Node ? content : document.createTextNode(content),
      );
      overlayHost(target).append(tip);
      tip.hidden = false;
      const rect = target.getBoundingClientRect();
      const point =
        event.type === "focusin"
          ? { x: rect.x + 12, y: rect.bottom + 14 }
          : { x: event.clientX + 12, y: event.clientY + 14 };
      const position = () => placeFloating(tip, point);
      position();
      tracking = trackFloating({
        element: tip,
        owner: target,
        position,
        close: hide,
      });
      return;
    }
  }
  document.addEventListener("pointerover", show);
  document.addEventListener("focusin", show);
  document.addEventListener("pointerout", (event) => {
    if (active?.contains(event.target) && !active.contains(event.relatedTarget))
      hide();
  });
  document.addEventListener("focusout", hide);
  window.addEventListener("signedout", hide);
  window.addEventListener("languagechange", hide);
}
