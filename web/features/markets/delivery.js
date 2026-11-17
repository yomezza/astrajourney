import { stationRef } from "../stations/index.js";

import { Session } from "../../core/session.js";

import { I18n } from "../../core/i18n.js";

import { UI, Modules } from "../../ui/runtime.js";

import { d, w, res, fmtTime, money } from "../../features/selectors.js";
import { reset } from "../../ui/view-state.js";

export function createDeliveryMarket({ field, act, command }) {
  const n = (...args) => UI.node(...args),
    tr = (s) => I18n.t(s),
    b = (...args) => UI.button(...args),
    row = (...args) => UI.row(...args),
    choice = (...args) => UI.choice(...args);
  function renderDeliveries(root) {
    const list = n("section");
    list.append(
      UI.table(
        ["ID", "Маршрут", "Груз", "Награда", "Статус", "Действие"],
        (w().deliveries || []).map((job) => {
          const actions = n("div", undefined, "inline-controls"),
            own = job.owner === Session.data.state.user.id;
          actions.append(b("Информация", () => deliveryDetails(job)));
          if (job.status === "open") {
            actions.append(
              b(own ? "Нанять бота" : "Принять", () =>
                command({
                  action: own ? "delivery_bot" : "delivery_accept",
                  id: job.id,
                }),
              ),
            );
            if (own)
              actions.append(
                b("Отменить", () =>
                  command({ action: "delivery_cancel", id: job.id }),
                ),
              );
          }
          if (
            job.status === "carrying" &&
            job.carrier === Session.data.state.user.id
          )
            actions.append(
              b("Сдать груз", () =>
                command({
                  action: "delivery_finish",
                  id: job.id,
                  ship: job.ship_id,
                }),
              ),
            );
          return [
            "#" + job.id,
            job.from_star + " → " + job.to_star,
            tr(res(job.resource).name) + " × " + job.quantity,
            money(job.reward),
            tr(job.status),
            actions,
          ];
        }),
      ),
    );
    root.append(list);
    const form = n("details");
    form.append(n("summary", tr("Создать доставку")));
    const fields = n("div", undefined, "settings-fields"),
      resource = choice(
        fields,
        "Ресурс",
        d().resources.map((r) => [r.id, r.name]),
        "iron",
      ),
      quantity = field(fields, "Количество", "delivery-quantity", 1, "number"),
      target = choice(
        fields,
        "Станция назначения",
        (w().stations || []).map((s) => [s.star + "|" + s.object_id, s.name]),
        w().stations?.[0]
          ? w().stations[0].star + "|" + w().stations[0].object_id
          : "",
      ),
      reward = field(
        fields,
        "Вознаграждение",
        "delivery-reward",
        100,
        "number",
      ),
      collateral = field(
        fields,
        "Обеспечение",
        "delivery-collateral",
        100,
        "number",
      ),
      hours = field(fields, "Срок, часов", "delivery-hours", 24, "number");
    form.append(
      fields,
      b(
        "Создать заказ",
        () => {
          const [to, station] = target.value.split("|");
          return command({
            action: "delivery_create",
            resource: resource.value,
            quantity: Number(quantity.value),
            to: Number(to),
            station,
            amount: Number(reward.value),
            collateral: Number(collateral.value),
            hours: Number(hours.value),
          });
        },
        !d().docked || !w().stations?.length,
        "Стыкуйтесь со станцией",
      ),
    );
    root.append(form);
  }
  function deliveryDetails(job) {
    const root = Modules.create("delivery-info", "Доставка", 520, 430);
    reset(root);
    row(root, "Отправление", stationRef(job.from_star, job.from_station));
    row(root, "Назначение", stationRef(job.to_star, job.to_station));
    row(root, "Груз", tr(res(job.resource).name) + " × " + job.quantity);
    row(root, "Вознаграждение", money(job.reward));
    row(root, "Обеспечение", money(job.collateral));
    row(root, "Срок", fmtTime(job.deadline));
    row(root, "Исполнитель", job.carrier_name || tr("Не назначен"));
    if (job.arrival) row(root, "Прибытие", fmtTime(job.arrival));
    Modules.open("delivery-info");
  }
  return { renderDeliveries, deliveryDetails };
}
