import { UI } from "../../ui/runtime.js";
import { I18n } from "../../core/i18n.js";
const n = (...args) => UI.node(...args),
  tr = (s) => I18n.t(s);
export const paintFlag = (badge, company) => {
  badge.replaceChildren();
  badge.style.color = company.color;
  const flag = n("span", undefined, "flag-mark"),
    design = company.flag_design;
  if (design?.background)
    flag.style.background = `linear-gradient(135deg,${design.background} 35%,${design.stripe} 36%,${design.stripe} 48%,${design.background} 49%)`;
  flag.append(n("span", company.flag, "flag-symbol"));
  badge.append(flag, n("span", "[" + company.tag + "]"));
};
