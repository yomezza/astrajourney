import { UI } from "../../ui/runtime.js";
import { Format } from "../../core/format.js";
const n = (...args) => UI.node(...args),
  money = Format.credits;
export function ledgerAmount(value) {
  const amount = Number(value),
    span = n(
      "span",
      (amount > 0 ? "+" : "") + money(amount),
      amount > 0
        ? "ledger-income"
        : amount < 0
          ? "ledger-expense"
          : "ledger-neutral",
    );
  if (amount !== 0) span.dataset.help = amount > 0 ? "Доход" : "Расход";
  return span;
}
