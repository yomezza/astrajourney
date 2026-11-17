import { UI } from "./runtime.js";
import { I18n } from "../core/i18n.js";

const n = (...args) => UI.node(...args),
  tr = (s) => I18n.t(s);
export function initTable() {
  UI.table = (headers, rows) => {
    const wrap = n("div", undefined, "table-wrap"),
      table = n("table"),
      head = n("thead"),
      trh = n("tr");
    headers.forEach((h) => trh.append(n("th", tr(h))));
    head.append(trh);
    const body = n("tbody");
    for (const row of rows) {
      const trr = n("tr");
      for (const value of row) {
        const td = n("td");
        td.append(
          value instanceof Node
            ? value
            : document.createTextNode(String(value ?? "—")),
        );
        trr.append(td);
      }
      body.append(trr);
    }
    table.append(head, body);
    wrap.append(table);
    return wrap;
  };
}
