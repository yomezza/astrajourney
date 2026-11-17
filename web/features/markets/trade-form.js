import { Session } from "../../core/session.js";

import { I18n } from "../../core/i18n.js";

import { UI } from "../../ui/runtime.js";

import { performExpedition } from "../../features/commands.js";
import { d, res, num, money, cargoCapacity } from "../../features/selectors.js";

export function createTradeForm({ field, act, command }) {
  const n = (...args) => UI.node(...args),
    tr = (s) => I18n.t(s),
    b = (...args) => UI.button(...args);
  function tradeForm(root, offer, global = false, starID, stationID) {
    const r = res(offer.resource),
      panel = n("aside", undefined, "detail-panel");
    panel.append(n("h3", tr(r.name)), n("p", tr(r.description), "muted"));
    const q = field(panel, "Количество", "trade-quantity", 1, "number");
    q.min = 1;
    q.max = Math.min(global ? 240 : cargoCapacity(), offer.stock);
    const quote = n("p", undefined, "trade-total"),
      capacity = n("p", undefined, "muted");
    const buy = b(global ? "Забронировать на станции" : "Купить в трюм", () =>
      global
        ? act("market/global", {
            action: "reserve",
            star: starID,
            station: stationID,
            resource: r.id,
            quantity: Number(q.value),
            expected_price: offer.buy,
          })
        : performExpedition({
            action: "buy",
            resource: r.id,
            quantity: Number(q.value),
            expected_price: offer.buy,
          }),
    );
    buy.classList.add("primary-action");
    const sell = b("Продать из трюма", () =>
      performExpedition({
        action: "sell",
        resource: r.id,
        quantity: Number(q.value),
        expected_price: offer.sell,
      }),
    );
    const update = () => {
      const count = Number(q.value),
        cost = offer.buy * count,
        used =
          Object.values(d().ship.cargo).reduce((a, b) => a + b, 0) +
          (d().job?.quantity || 0);
      quote.textContent = `${money(offer.buy)} × ${num(count)} = ${money(cost)}`;
      capacity.textContent = global
        ? tr(
            "Выдача на станции указанной системы. Полный возврат до получения; комиссия хранения — 0 CR.",
          )
        : tr("Свободно в трюме") + ": " + (cargoCapacity() - used);
      buy.disabled =
        !Number.isInteger(count) ||
        count < 1 ||
        count > offer.stock ||
        count > (global ? 240 : cargoCapacity()) ||
        cost > d().credits ||
        (!global && (!d().docked || count + used > cargoCapacity()));
      sell.disabled =
        global ||
        !d().docked ||
        count < 1 ||
        count > (d().ship.cargo[r.id] || 0);
    };
    q.oninput = update;
    panel.append(quote, capacity, buy);
    if (!global) panel.append(sell);
    else
      panel.append(
        n(
          "p",
          tr("Срок хранения") +
            ": " +
            Session.data.clock.settings.reservation_hours +
            " h",
          "muted",
        ),
      );
    root.append(panel);
    update();
  }
  return { tradeForm };
}
