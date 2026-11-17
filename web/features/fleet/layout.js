import { Modules } from "../../ui/runtime.js";
function entry(id) {
  const dialog = document.getElementById("window-" + id);
  return dialog
    ? {
        dialog,
        pinned: dialog.classList.contains("pinned"),
        maximized: dialog.classList.contains("maximized"),
      }
    : undefined;
}
export function arrangeShip() {
  const ship = entry("ship");
  if (!ship?.dialog.open) return;
  const members = ["ship", "cargo"]
    .filter(
      (id) =>
        id === "ship" || (entry(id)?.dialog.open && Modules.linked(id, "ship")),
    )
    .map((id) => ({ id, w: entry(id) }));
  if (members.some(({ w }) => w.pinned || w.maximized)) {
    for (const { id } of members)
      if (id !== "ship") Modules.adjacent(id, "ship", "right");
    return;
  }
  const sidebar = document.getElementById("sidebar"),
    left =
      innerWidth >= 900 && !sidebar.hidden
        ? sidebar.getBoundingClientRect().right + 12
        : 6,
    available = innerWidth - left - 8,
    gap = 8,
    rects = members.map(({ w }) => w.dialog.getBoundingClientRect());
  if (
    rects.every(
      (r, i) =>
        r.left >= left &&
        r.right <= innerWidth - 8 &&
        (i === 0 || r.left >= rects[i - 1].right + gap - 1),
    )
  )
    return;
  const minima = members.map(({ id }) => (id === "ship" ? 620 : 350)),
    widths = rects.map((r, i) => Math.max(minima[i], r.width)),
    total = widths.reduce((a, b) => a + b, 0),
    minimum = minima.reduce((a, b) => a + b, 0),
    budget = available - gap * (members.length - 1),
    fits = budget >= minimum;
  if (fits && total > budget) {
    const extra = total - minimum;
    widths.forEach(
      (v, i) =>
        (widths[i] =
          minima[i] + ((v - minima[i]) * (budget - minimum)) / extra),
    );
  } else if (!fits) {
    widths.forEach((v, i) => (widths[i] = Math.min(minima[i], available)));
  }
  const span = widths.reduce((a, b) => a + b, 0) + gap * (members.length - 1),
    maxHeight = Math.max(...rects.map((r) => r.height)),
    y = Math.max(
      64,
      Math.min(ship.dialog.offsetTop, innerHeight - maxHeight - 36),
    );
  let x = left + Math.max(0, (available - span) / 2);
  members.forEach(({ id, w }, i) => {
    if (!fits)
      x =
        left +
        (available - widths[i]) *
          (members.length === 1 ? 0.5 : i / (members.length - 1));
    w.dialog.style.width = widths[i] + "px";
    w.dialog.style.left = x + "px";
    w.dialog.style.top = y + "px";
    Modules.place(id);
    x += widths[i] + gap;
  });
}
