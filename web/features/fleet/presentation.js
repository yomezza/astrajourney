import { Session } from "../../core/session.js";

import { UI } from "../../ui/runtime.js";
import { I18n } from "../../core/i18n.js";
const n = (...args) => UI.node(...args),
  tr = (s) => I18n.t(s);
export const shipName = (s) => {
  const name =
    s.spec?.name ||
    Session.data.workbench?.ship_classes?.find((c) => c.id === s.class)?.name;
  return s.name === name ? tr(s.name) : s.name;
};
