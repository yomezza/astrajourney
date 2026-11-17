import { I18n } from "../../core/i18n.js";

import { UI } from "../../ui/runtime.js";

import { d, num, money } from "../../features/selectors.js";

import { resourceName } from "../../features/inventory/resource-label.js";

export function createLocalMarket({
  state,
  field,
  renderMarket,
  tradeForm,
  act,
  command,
}) {
  const n = (...args) => UI.node(...args),
    tr = (s) => I18n.t(s),
    b = (...args) => UI.button(...args);
  return function renderLocal(root, { port, globalMarket }) {
    if (!d().docked) {
      UI.empty(root, "Для локальной торговли пристыкуйтесь к порту.");
      return;
    }
    root.append(
      n(
        "p",
        tr(port?.specialization || "general") +
          " · " +
          tr("Тариф") +
          ": " +
          (port?.fee_percent || 0) +
          "% · " +
          tr("Бюджет станции") +
          ": " +
          money(d().market_credits),
        "muted",
      ),
    );
    const layout = n("div", undefined, "master-detail"),
      list = n("section");
    layout.append(list);
    let offer =
      d().offers.find((o) => o.resource === state.resource) || d().offers[0];
    const rows = d().offers.map((o) => {
      const link = b("", () => {
        state.resource = o.resource;
        renderMarket();
      });
      link.append(resourceName(o.resource));
      link.classList.toggle("selected", offer?.resource === o.resource);
      return [link, num(o.stock), money(o.buy), money(o.sell)];
    });
    list.append(UI.table(["Ресурс", "В наличии", "Купить", "Продать"], rows));
    if (offer) tradeForm(layout, offer);
    root.append(layout);
  };
}
