import { ledgerAmount } from "./amount.js";

import { FormState } from "../../ui/form-state.js";
import { Session } from "../../core/session.js";

import { I18n } from "../../core/i18n.js";

import { UI } from "../../ui/runtime.js";

import { w, fmtTime, money } from "../../features/selectors.js";
import { reset } from "../../ui/view-state.js";

export function createFinance({ roots, act, command }) {
  const drafts = new FormState(() => Session.data.state?.user.id);
  const tr = (s) => I18n.t(s);
  function renderFinance() {
    const root = roots.finance;
    reset(root);
    root.append(
      UI.table(
        ["Дата", "Операция", "Изменение", "Баланс"],
        (w().ledger || []).map((item) => [
          fmtTime(item.created_at),
          tr(item.reason),
          ledgerAmount(item.amount),
          money(item.balance),
        ]),
      ),
    );
    if (!w().ledger?.length) UI.empty(root, "Операций пока нет");
  }
  function clear() {
    drafts.clear();
  }
  window.addEventListener("actionaccepted", () => drafts.clear());
  return { ledgerAmount, renderFinance, clear };
}
