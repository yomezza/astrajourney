import { Lifetime } from "../core/lifetime.js";

export function overlayHost(owner) {
  return owner?.closest?.("dialog:modal") || document.body;
}
export function placeFloating(
  element,
  { anchor, x, y, parent, gap = 5, minTop = 8, minWidth, maxBottom = 8 } = {},
) {
  const rect =
    typeof anchor === "function"
      ? anchor()
      : anchor?.getBoundingClientRect?.() || anchor;
  if (minWidth)
    element.style.width =
      Math.min(Math.max(rect?.width || 0, minWidth), innerWidth - 16) + "px";
  element.style.maxHeight =
    Math.max(60, innerHeight - minTop - maxBottom) + "px";
  let left = parent ? parent.right : rect ? rect.left : (x ?? 8);
  if (parent && left + element.offsetWidth > innerWidth - 8)
    left = parent.left - element.offsetWidth;
  element.style.left =
    Math.max(8, Math.min(left, innerWidth - element.offsetWidth - 8)) + "px";
  const top = rect ? rect.bottom + gap : (y ?? 8);
  element.style.top =
    Math.max(
      minTop,
      Math.min(top, innerHeight - element.offsetHeight - maxBottom),
    ) + "px";
}
export function trackFloating({ element, owner, position, close }) {
  const life = new Lifetime();
  const update = () => {
    if (!element.isConnected || (owner && !owner.isConnected)) {
      close();
      return;
    }
    position();
  };
  life.listen(window, "resize", update);
  if (window.visualViewport) {
    life.listen(window.visualViewport, "resize", update);
    life.listen(window.visualViewport, "scroll", update);
  }
  life.listen(document, "scroll", update, true);
  life.listen(window, "moduleclose", ({ detail }) => {
    if (owner?.closest?.(".module-window")?.id === "window-" + detail) close();
  });
  life.listen(window, "signedout", close);
  return life;
}
