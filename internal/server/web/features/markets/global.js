import { stationPicker, inPlayerGalaxy } from "../navigation/pickers.js";
import { stationRef } from "../stations/index.js";

import { I18n } from "../../core/i18n.js";

import { UI } from "../../ui/runtime.js";

import { d, res, num, fmtTime, money } from "../../features/selectors.js";

import { resourceName } from "../../features/inventory/resource-label.js";

export function createGlobalMarket({
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
  return function renderGlobal(root, { port, globalMarket }) {
    const result = globalMarket;
    const eligible = result.ports.filter((p) => inPlayerGalaxy(p.star)).sort((a, b) => a.star - b.star);
    const portKey = (p) => p.star + "|" + p.station;
    let target = state.target || d().ship.star + "|" + d().ship.object_id;
    if (!eligible.some((p) => portKey(p) === target))
      target = eligible[0] && portKey(eligible[0]);
    if (!target) {
      UI.empty(root, "Нет доступных станций");
      return;
    }
    const stationLabel = n("label");
    stationLabel.append(n("span", tr("Станция получения")));
    root.append(stationLabel);
    stationPicker(stationLabel, eligible, target, (value) => {
      state.target = value;
      renderMarket();
    });
    const targetPort = eligible.find((p) => portKey(p) === target);
    root.append(
      n(
        "p",
        tr(targetPort.specialization) +
          " · " +
          tr("Ассортимент зависит от станции"),
        "muted",
      ),
    );
    const layout = n("div", undefined, "master-detail"),
      list = n("section");
    layout.append(list);
    const offers = targetPort.offers || [],
      offer = offers.find((o) => o.resource === state.resource) || offers[0];
    list.append(
      UI.table(
        ["Ресурс", "В наличии", "Цена"],
        offers.map((o) => [
          b(tr(res(o.resource).name), () => {
            state.resource = o.resource;
            renderMarket();
          }),
          num(o.stock),
          money(o.buy),
        ]),
      ),
    );
    if (offer)
      tradeForm(layout, offer, true, targetPort.star, targetPort.station);
    root.append(layout, n("h3", tr("Мои бронирования")));
    const rows = result.reservations.map((r) => {
      const controls = n("div", undefined, "inline-controls");
      if (r.status === "reserved")
        controls.append(
          b(
            "Получить груз",
            () => act("market/global", { action: "collect", id: r.id }),
            !d().docked ||
              d().ship.star !== r.star ||
              d().ship.object_id !== r.station,
            "Прибудьте на станцию получения",
          ),
          b("Отменить бронь", () =>
            act("market/global", { action: "cancel", id: r.id }),
          ),
        );
      return [
        "#" + r.id,
        stationRef(r.star, r.station),
        resourceName(r.resource),
        "× " + r.quantity,
        money(r.cost),
        n("span", tr(r.status), "reservation-status status-" + r.status),
        r.expires_at ? fmtTime(r.expires_at) : "—",
        controls,
      ];
    });
    root.append(
      UI.table(
        [
          "ID",
          "Станция",
          "Ресурс",
          "Количество",
          "Стоимость",
          "Статус",
          "Хранение до",
          "Действие",
        ],
        rows,
      ),
    );
  };
}
