import { UI } from "./runtime.js";
import { Format } from "../core/format.js";
export function pager({
  page,
  pages,
  total,
  change,
  className = "pagination",
  label,
}) {
  const root = UI.node("div", undefined, className);
  root.append(
    UI.button("‹", () => change(page - 1), page <= 1),
    UI.node(
      "span",
      label ??
        `${page} / ${pages}${total === undefined ? "" : " · " + Format.quantity(total)}`,
    ),
    UI.button("›", () => change(page + 1), page >= pages),
  );
  return root;
}
// Cursor pagination remains a different API contract from numbered pages.
export const cursorMore = (label, cursor, load) =>
  UI.button(label, () => load(cursor), !cursor);
