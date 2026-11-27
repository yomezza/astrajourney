import { shipName } from "../../features/fleet/presentation.js";
import { stationPicker } from "../../features/navigation/pickers.js";
import { shipActivity } from "../../features/fleet/activity.js";
import { Session } from "../../core/session.js";
import { I18n } from "../../core/i18n.js";
import { api } from "../../core/api.js";
import { refresh } from "../../core/snapshot-store.js";
import { notify } from "../../ui/feedback.js";
import { UI } from "../../ui/runtime.js";
import { Scheduler } from "../../features/runtime.js";

export function initScheduler() {
  const n = UI.node,
    tr = (s) => I18n.t(s),
    button = UI.button;
  const stationKey = (p) => p.star + "|" + (p.station || p.object_id);
  const parseStation = (value) => {
    const [star, station] = String(value).split("|");
    return { star: Number(star), station };
  };
  const requests = new WeakMap();
  Object.assign(Scheduler, {
    async render(root, scope) {
      const shipsForView = () => Session.data.fleet.ships.filter(ship => !scope || ship.id === scope.shipID);
      const visibleTask = task => !scope || task.ship_id === scope.shipID;
      const namespaced = key => scope ? scope.namespace + ":" + key : key;
      UI.rememberFolds(root);
      let host = root.querySelector(".planner");
      if (!host || host.dataset.language !== I18n.language || host.shipContext !== scope?.shipID) {
        host?.remove();
        host = n("section", undefined, "planner");
        host.dataset.language = I18n.language;
        host.shipContext = scope?.shipID;
        root.prepend(host);
        const heading = n("div", undefined, "planner-heading"), title = n("div");
        title.append(n("h3", tr("Центр задач")), n("p", tr("Задачи выполняются сервером при закрытом браузере. На каждом корабле — последовательно. Пауза не отменяет уже начатый полёт или добычу."), "muted"));
        heading.append(n("span", "◷", "planner-emblem"), title, n("span", tr("Серверное выполнение"), "planner-server-badge"));
        const stats = n("div", undefined, "planner-stats");
        for (const [status, label] of [["running", "Выполняются"], ["queued", "В очереди"], ["paused", "На паузе"]]) {
          const card = n("div");
          card.dataset.status = status;
          card.append(n("strong", "0"), n("span", tr(label)));
          stats.append(card);
        }
        host.append(heading, stats);
        const form = n("form"),
          body = n("div"),
          list = n("div", undefined, "planner-list");
        form.onsubmit = (e) => e.preventDefault();
        let shipID = scope?.shipID ?? Session.data.expedition.ship.id,
          kind = "mine",
          source,
          dest,
          resource,
          quantity,
          site;
        const shipsPicker = UI.choice(
          form,
          "Корабль",
          shipsForView().map((s) => [s.id, shipName(s)]),
          shipID,
          (value) => {
            shipID = Number(value);
            return build();
          },
        );
        host.updateShips = () => {
          const ships = shipsForView(),
            signature = JSON.stringify(ships.map((s) => [s.id, shipName(s)]));
          if (host.dataset.fleet === signature) return;
          host.dataset.fleet = signature;
          shipsPicker.setOptions(ships.map((s) => [s.id, shipName(s)]));
          if (!ships.some((s) => s.id === shipID)) {
            shipID = Number(shipsPicker.value);
            return build();
          }
        };
        UI.choice(
          form,
          "Тип задачи",
          [
            ["mine", "Добыча"],
            ["haul", "Перевозка между складами"],
          ],
          kind,
          (value) => {
            kind = value;
            return build();
          },
        );
        const layout = n("div", undefined, "planner-workspace"),
          compose = n("section", undefined, "planner-compose"),
          side = n("aside", undefined, "planner-sidebar"),
          queueTitle = n(
            "h3",
            tr("Очередь и выполнение"),
            "planner-queue-title",
          ),
          history = n("details", undefined, "planner-history"),
          historyList = n("div", undefined, "planner-history-list");
        compose.append(n("h3", tr("Новая задача")), form, body);
        const filters = n("div", undefined, "planner-filters");
        const filter = UI.choice(filters, "Показать задачи", [["all", "Все задачи"], ["running", "Выполняются"], ["queued", "В очереди"], ["paused", "На паузе"]], scope?.filter || "all", value => {
          scope?.onFilter(value);
          host.filterTasks();
        });
        host.filterTasks = () => {
          for (const card of list.querySelectorAll(".planner-item")) card.hidden = filter.value !== "all" && card.dataset.status !== filter.value;
          for (const group of list.querySelectorAll(".planner-ship")) group.hidden = filter.value !== "all" && !group.querySelector(".planner-item:not([hidden])");
        };
        history.append(
          n("summary", tr("История завершённых задач")),
          historyList,
        );
        side.append(queueTitle, filters, list, history);
        layout.append(compose, side);
        host.append(layout);
        let historyRequest = 0;
        async function loadHistory() {
          const serial = ++historyRequest,
            epoch = Session.epoch;
          try {
            const tasks = (await api("planner?view=history")).filter(visibleTask);
            if (
              serial !== historyRequest ||
              epoch !== Session.epoch ||
              !host.isConnected ||
              !history.open
            )
              return;
            historyList.replaceChildren();
            for (const task of tasks) {
              const res = Session.data.expedition.resources.find(
                (r) => r.id === task.resource,
              );
              const line = n(
                "p",
                task.ship_name +
                  " · " +
                  tr(res?.name || task.resource) +
                  " · " +
                  task.progress +
                  " / " +
                  task.quantity +
                  " · " +
                  tr(task.status),
              );
              historyList.append(line);
            }
            if (!tasks.length)
              UI.empty(historyList, "Завершённых задач пока нет");
          } catch (e) {
            if (epoch === Session.epoch && Session.signedIn)
              notify(e.message, "error");
          }
        }
        history.ontoggle = () => {
          if (history.open) loadHistory();
        };
        let generation = 0;
        async function build() {
          const key = ++generation,
            epoch = Session.epoch;
          body.replaceChildren();
          const inputs = n("form");
          inputs.onsubmit = (e) => e.preventDefault();
          body.append(inputs);
          const ship = Session.data.fleet.ships.find((s) => s.id === shipID);
          if (!ship) return;
          const ports = Session.data.workbench.stations || [];
          if (kind === "mine") {
            const data = await api("system?id=" + ship.star);
            if (
              key !== generation ||
              epoch !== Session.epoch ||
              !host.isConnected
            )
              return;
            const sites = data.sites.filter((s) => s.resources?.length);
            if (!sites.length) {
              UI.empty(
                inputs,
                "Исследуйте объекты системы для автоматической добычи",
              );
              return;
            }
            site = sites[0];
            source = { star: ship.star, station: site.id };
            const resources = n("div");
            function chooseResources() {
              resources.replaceChildren();
              resource = site.resources[0];
              UI.choice(
                resources,
                "Ресурс",
                site.resources.map((id) => [
                  id,
                  Session.data.expedition.resources.find((r) => r.id === id)
                    ?.name || id,
                ]),
                resource,
                (value) => (resource = value),
              );
            }
            UI.choice(
              inputs,
              "Объект добычи",
              sites.map((s) => [s.id, s.name]),
              site.id,
              (value) => {
                site = sites.find((s) => s.id === value);
                source.station = value;
                chooseResources();
              },
            );
            chooseResources();
            inputs.append(resources);
            dest = { star: ship.star, station: "" };
            UI.choice(
              inputs,
              "Выгрузка добытого",
              [
                ["", "Оставлять в трюме"],
                ...ports
                  .filter((p) => p.star === ship.star)
                  .map((p) => ({
                    id: p.object_id,
                    label: p.name,
                    station: { star: p.star, id: p.object_id },
                  })),
              ],
              "",
              (value) => (dest.station = value),
            );
          } else {
            if (!ports.length) {
              UI.empty(inputs, "Нет доступных станций");
              return;
            }
            source = parseStation(
              stationKey(
                ports.find(
                  (p) => p.star === ship.star && p.object_id === ship.object_id,
                ) || ports[0],
              ),
            );
            dest = parseStation(
              stationKey(
                ports.find(
                  (p) => stationKey(p) !== source.star + "|" + source.station,
                ) || ports[0],
              ),
            );
            for (const [label, value, change] of [
              ["Склад отправления", source, (v) => (source = parseStation(v))],
              ["Склад назначения", dest, (v) => (dest = parseStation(v))],
            ]) {
              const field = n("label");
              field.append(n("span", tr(label)));
              stationPicker(
                field,
                ports,
                value.star + "|" + value.station,
                change,
              );
              inputs.append(field);
            }
            resource = Session.data.expedition.resources[0].id;
            UI.choice(
              inputs,
              "Ресурс",
              Session.data.expedition.resources.map((r) => [r.id, r.name]),
              resource,
              (value) => (resource = value),
            );
          }
          quantity = UI.field(
            inputs,
            "Количество",
            namespaced("planner-quantity"),
            kind === "mine" ? 30 : 10,
            "number",
          );
          quantity.min = 1;
          quantity.max = 100000;
          quantity.step = 1;
          inputs.append(
            button("Добавить задачу", async () => {
              if (!inputs.reportValidity()) return;
              const owner = Session.epoch;
              await api("planner", {
                action: "create",
                ship: shipID,
                kind,
                source_star: source.star,
                source_object: source.station,
                dest_star: dest.star,
                dest_station: dest.station,
                resource,
                quantity: Number(quantity.value),
              });
              if (owner !== Session.epoch || !Session.signedIn) return;
              notify(tr("Задача добавлена"));
              await refresh(true);
              await Scheduler.render(root,scope);
            }),
          );
        }
        await build();
      }
      await host.updateShips?.();
      const key = {};
      requests.set(root,key);
      const epoch = Session.epoch, result = await api("planner");
      if (key !== requests.get(root) || epoch !== Session.epoch || !host.isConnected)
        return;
      const list = host.querySelector(".planner-list");
      UI.rememberFolds(list);
      list.replaceChildren();
      const active = result
        .filter(t => visibleTask(t) && ["queued", "running", "paused"].includes(t.status))
        .sort(
          (a, b) =>
            Number(b.carried > 0 || b.pending > 0) -
              Number(a.carried > 0 || a.pending > 0) ||
            b.priority - a.priority ||
            a.id - b.id,
        );
      host.querySelector(".planner-queue-title").textContent =
        tr("Очередь и выполнение") + " · " + active.length;
      host.querySelector(".planner-queue-title").dataset.help =
        "Приоритет меняется в пределах корабля. Текущий цикл добычи или разгрузка уже принятого груза завершается перед переключением.";
      for (const card of host.querySelectorAll(".planner-stats > div")) card.querySelector("strong").textContent = active.filter((task) => task.status === card.dataset.status).length;
      const groups = new Map(),
        counts = new Map();
      for (const task of active)
        counts.set(task.ship_id, (counts.get(task.ship_id) || 0) + 1);
      for (const ship of shipsForView()) {
        const group = UI.fold(
            namespaced(`planner:ship:v2:${ship.id}`),
            false,
            "planner-ship",
            true,
          ),
          heading = n("summary", undefined, "planner-ship-heading"),
          tasks = n("div", undefined, "planner-ship-tasks");
        const model = UI.hologram(
          ship.spec.model,
          shipName(ship),
          "planner-ship-model",
        );
        UI.updateHologram(
          model,
          ship.spec.model,
          shipName(ship),
          !!ship.destination || !!ship.local_target,
        );
        const count = counts.get(ship.id) || 0,
          badge = n("span", tr("Задания") + ": " + count, "planner-ship-count");
        badge.dataset.help = "В очереди, выполняются или приостановлены";
        heading.append(model, n("h4", shipName(ship)), badge);
        group.append(heading, tasks);
        shipActivity(tasks, ship, count > 0);
        if (!count) UI.empty(tasks, "Нет запланированных задач");
        groups.set(ship.id, tasks);
        list.append(group);
      }
      for (const task of active) {
        const card = UI.fold(
            namespaced(`planner:task:${task.id}`),
            true,
            "planner-item",
            true,
          ),
          taskHeading = n("summary"),
          resource = Session.data.expedition.resources.find(
            (r) => r.id === task.resource,
          ),
          ship = Session.data.fleet.ships.find((s) => s.id === task.ship_id);
        card.dataset.task = String(task.id);
        card.dataset.status = task.status;
        taskHeading.append(n("span", tr({ running: "Выполняются", queued: "В очереди", paused: "На паузе" }[task.status] || task.status), "planner-status"));
        taskHeading.append(
          n(
            "b",
            (ship ? shipName(ship) : task.ship_name) +
              " · " +
              tr(
                {
                  mine: "Добыча",
                  haul: "Перевозка между складами",
                  load: "Получение со склада",
                  unload: "Доставка на склад",
                }[task.kind] || task.kind,
              ),
          ),
        );
        card.append(
          taskHeading,
          n(
            "p",
            tr(resource?.name || task.resource) +
              " · " +
              task.progress +
              " / " +
              task.quantity +
              " · " +
              tr(task.status),
          ),
          UI.bar(task.progress, task.quantity, "Выполнено"),
        );
        if (task.error) card.append(n("p", tr(task.error), "planner-message"));
        if (["queued", "running", "paused"].includes(task.status)) {
          const tools = n("div", undefined, "inline-controls");
          for (const [action, label] of [
            [
              task.status === "paused" ? "resume" : "pause",
              task.status === "paused" ? "Продолжить" : "Приостановить",
            ],
            ["raise", "Выше в очереди"],
            ["lower", "Ниже в очереди"],
            ["activate", "Сделать активной"],
            ["cancel", "Отменить задачу"],
          ])
            tools.append(
              button(label, async () => {
                const owner = Session.epoch;
                await api("planner", { action, id: task.id });
                if (owner !== Session.epoch || !Session.signedIn) return;
                await refresh(true);
                await Scheduler.render(root,scope);
              }),
            );
          card.append(tools);
        }
        groups.get(task.ship_id)?.append(card);
      }
      host.filterTasks();
    },
  });

}
