import { UI } from "./runtime.js";
import { I18n } from "../core/i18n.js";
import { node } from "./dom.js";

export function createTable(headers, rows, { className = "table-wrap", label } = {}) {
  const wrap = node("div", undefined, className);
  const table = node("table");
  const head = node("thead"), heading = node("tr");
  const columns = headers.map(header => Array.isArray(header) ? header : [header]);
  if (label) {
    wrap.classList.add("data-table");
    wrap.tabIndex = 0;
    wrap.setAttribute("role", "region");
    wrap.setAttribute("aria-label", I18n.t(label));
  }
  for (const [title, alignment] of columns) {
    const cell = node("th", I18n.t(title), alignment);
    cell.scope = "col";
    heading.append(cell);
  }
  head.append(heading);
  const body = node("tbody");
  for (const row of rows) {
    const line = node("tr");
    for (const [index, value] of row.entries()) {
      const cell = node("td", undefined, columns[index]?.[1]);
      cell.append(value instanceof Node ? value : String(value ?? "—"));
      line.append(cell);
    }
    body.append(line);
  }
  table.append(head, body);
  wrap.append(table);
  return wrap;
}

export function initTable() {
  UI.table = createTable;
}
