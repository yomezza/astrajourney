import { stationRef } from "../stations/index.js";

import { Session } from "../../core/session.js";

import { I18n } from "../../core/i18n.js";

import { UI } from "../../ui/runtime.js";

import { d, w, res, num, money } from "../../features/selectors.js";

export function createOrdersMarket({
  state,
  field,
  renderMarket,
  tradeForm,
  act,
  command,
}) {
  const n = (...args) => UI.node(...args),
    tr = (s) => I18n.t(s),
    b = (...args) => UI.button(...args),
    choice = (...args) => UI.choice(...args);
  return function renderOrders(root, { port, globalMarket }) {
    const layout = n("div", undefined, "master-detail"),
      list = n("section"),
      side = n("aside", undefined, "detail-panel");
    layout.append(list, side);
    const orders = w().orders || [];
    list.append(
      UI.table(
        ["Ресурс", "Место", "Количество", "Цена"],
        orders.map((o) => [
          b(tr(res(o.resource).name), () => {
            state.order = o.id;
            renderMarket();
          }),
          stationRef(o.star, o.station),
          num(o.quantity),
          money(o.price),
        ]),
      ),
    );
    const selected = orders.find((o) => o.id === state.order);
    if (selected) {
      side.append(
        n("h3", tr(res(selected.resource).name)),
        n("p", selected.seller_name),
        stationRef(selected.star, selected.station),
      );
      const q = field(side, "Количество", "order-buy-quantity", 1, "number");
      q.min = 1;
      q.max = selected.quantity;
      const total = n("p");
      q.oninput = () =>
        (total.textContent =
          tr("Итого") + ": " + money(Number(q.value) * selected.price));
      q.oninput();
      side.append(
        total,
        b(
          "Купить на склад",
          () =>
            command({
              action: "order_buy",
              id: selected.id,
              quantity: Number(q.value),
            }),
          selected.seller === Session.data.state.user.id,
        ),
      );
      if (selected.seller === Session.data.state.user.id)
        side.append(
          b("Снять предложение", () =>
            command({ action: "order_cancel", id: selected.id }),
          ),
        );
    } else UI.empty(side, "Выберите предложение");
    side.append(n("h3", tr("Выставить со склада")));
    const resource = choice(
        side,
        "Ресурс",
        d().resources.map((r) => [r.id, r.name]),
        "iron",
      ),
      quantity = field(side, "Количество", "order-sell-quantity", 1, "number"),
      price = field(side, "Цена за единицу", "order-sell-price", 10, "number");
    side.append(
      b(
        "Выставить на продажу",
        () =>
          command({
            action: "order_sell",
            resource: resource.value,
            quantity: Number(quantity.value),
            price: Number(price.value),
          }),
        !d().docked,
        "Стыкуйтесь со станцией",
      ),
    );
    root.append(layout);
  };
}
