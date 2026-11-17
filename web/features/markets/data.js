import { Session } from "../../core/session.js";

import { I18n } from "../../core/i18n.js";

import { UI } from "../../ui/runtime.js";

import { world } from "../../core/world-store.js";

import { d, w, num, fmtTime, money } from "../../features/selectors.js";

export function createDataMarket({
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
  return function renderData(root, { port, globalMarket }) {
    const list = n("section");
    for (const offer of w().data_offers || []) {
      const card = n("article", undefined, "data-offer");
      card.append(
        n(
          "h3",
          tr(
            world.data?.regions.find((r) => r.id === offer.region)?.name ||
              offer.region,
          ),
        ),
        n(
          "p",
          offer.seller_name +
            " · " +
            (w().stations.find(
              (p) => p.star === offer.star && p.object_id === offer.station,
            )?.name || offer.star + " / " + offer.station),
        ),
      );
      card.append(
        n("small", fmtTime(offer.created_at)),
        n(
          "p",
          tr(
            "Пакет содержит сведения об объектах. Рыночные цены и квоты обновляются отдельно.",
          ),
          "muted",
        ),
      );
      const details = n("details");
      details.append(
        n(
          "summary",
          num(offer.systems.length) +
            " " +
            tr("Системы") +
            " · " +
            num(offer.new_systems) +
            " " +
            tr("Новые сведения"),
        ),
        n(
          "p",
          offer.systems
            .map((id) => world.data?.stars.find((s) => s.id === id)?.name || id)
            .join(" · "),
        ),
      );
      card.append(
        details,
        n("p", money(offer.price)),
        b(
          offer.seller === Session.data.state.user.id
            ? "Снять предложение"
            : "Купить разведданные",
          () =>
            command({
              action:
                offer.seller === Session.data.state.user.id
                  ? "data_remove"
                  : "data_buy",
              id: offer.id,
            }),
          offer.seller !== Session.data.state.user.id &&
            (!offer.new_systems ||
              !d().docked ||
              d().ship.star !== offer.star ||
              d().ship.object_id !== offer.station),
          offer.new_systems
            ? "Прибудьте на станцию продажи данных"
            : "Все сведения уже известны",
        ),
      );
      list.append(card);
    }
    if (!list.children.length)
      UI.empty(list, "Игроки ещё не выставили разведданные");
    const side = n("aside", undefined, "detail-panel");
    side.append(
      n("h3", tr("Продать свои исследования")),
      n(
        "p",
        tr(
          "В пакет попадут только исследованные вами системы выбранного региона.",
        ),
        "muted",
      ),
    );
    const region = choice(
        side,
        "Регион",
        (world.data?.regions || []).map((r) => [r.id, r.name]),
        d().system.region,
      ),
      price = field(side, "Цена пакета", "data-price", 100, "number");
    side.append(
      b(
        "Выставить пакет",
        () =>
          command({
            action: "data_sell",
            region: Number(region.value),
            price: Number(price.value),
          }),
        !d().docked,
        "Стыкуйтесь со станцией",
      ),
    );
    const layout = n("div", undefined, "master-detail");
    layout.append(list, side);
    root.append(layout);
  };
}
