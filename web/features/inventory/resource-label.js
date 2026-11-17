import { UI } from "../../ui/runtime.js";
import { I18n } from "../../core/i18n.js";
import { res } from "../selectors.js";
const n = (...args) => UI.node(...args),
  tr = (s) => I18n.t(s);
export function resourceName(id) {
  const r = res(id),
    span = n("span", tr(r?.name || id));
  span.dataset.resource = id;
  return span;
}
