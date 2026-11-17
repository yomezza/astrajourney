import { FormState } from "../../ui/form-state.js";
import { Session } from "../../core/session.js";

import { I18n } from "../../core/i18n.js";

import { UI, Modules } from "../../ui/runtime.js";

import { reset } from "../../ui/view-state.js";

export function createProfile({ roots, act, command }) {
  const drafts = new FormState(() => Session.data.state?.user.id);
  const tr = (s) => I18n.t(s),
    b = UI.button,
    row = UI.row;
  function renderProfile() {
    const root = roots.profile;
    reset(root);
    root.classList.add("pilot-profile");
    const n = UI.node, user = Session.data.state.user;
    const hero = n("div", undefined, "pilot-profile-hero");
    const avatar = n("span", Array.from(user.username).slice(0, 2).join("").toLocaleUpperCase(), "pilot-profile-avatar");
    avatar.setAttribute("aria-hidden", "true");
    const identity = n("div", undefined, "pilot-profile-identity");
    identity.append(n("p", tr("Позывной"), "pilot-profile-eyebrow"), n("h2", user.username), n("span", tr(user.role === "admin" ? "АДМИНИСТРАТОР" : "ИГРОК"), "pilot-profile-role"));
    hero.append(avatar, identity);
    const stats = n("div", undefined, "pilot-profile-stats");
    for (const [label, value] of [["Стаж игрока", Session.data.profile.level], ["Кораблей", Session.data.fleet.ships.length]]) {
      const card = n("div", undefined, "pilot-profile-stat");
      card.append(n("strong", String(value ?? "—")), n("span", tr(label)));
      stats.append(card);
    }
    const company = n("div", undefined, "pilot-profile-company");
    company.append(n("span", tr("Корпорация")), n("strong", Session.data.profile.company || tr("Без компании")));
    const actions = n("div", undefined, "pilot-profile-actions");
    actions.append(b("История финансов", () => Modules.open("finance")), b("Настройки", () => Modules.open("settings")));
    root.append(hero, stats, company, actions);
  }
  function clear() {
    drafts.clear();
  }
  window.addEventListener("actionaccepted", () => drafts.clear());
  return { renderProfile, clear };
}
