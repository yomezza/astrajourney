import { shipName } from "../../../features/fleet/presentation.js";
import { stationPicker } from "../../../features/navigation/pickers.js";

import { Session, RequestScope } from "../../../core/session.js";
import { I18n } from "../../../core/i18n.js";
import { api } from "../../../core/api.js";
import { refresh } from "../../../core/snapshot-store.js";
import { notify } from "../../../ui/feedback.js";
import { UI, Modules } from "../../../ui/runtime.js";
import { AdminResources } from "../../../features/runtime.js";
import { world } from "../../../core/world-store.js";

export function initAdminResources() {
  const n = UI.node,
    tr = (s) => I18n.t(s),
    button = UI.button;
  const stationKey = (p) => p.star + "|" + (p.station || p.object_id);
  const parseStation = (value) => {
    const [star, station] = String(value).split("|");
    return { star: Number(star), station };
  };
  const grantRoot = Modules.create(
    "resourcegrant",
    "Выдача ресурсов",
    760,
    670,
    { audience: "admin" },
  );
  const view = new RequestScope(
    () =>
      Session.signedIn &&
      Session.data.state?.user.role === "admin" &&
      Modules.isOpen("resourcegrant"),
  );
  let targetUser = 0,
    grantRequest = 0;
  Object.assign(AdminResources, {
    async render() {
      if (!Session.signedIn || Session.data.state?.user.role !== "admin") {
        Modules.close("resourcegrant");
        return;
      }
      const lease = view.begin({ abort: false });
      const epoch = Session.epoch,
        key = ++grantRequest;
      targetUser ||= Session.data.state.user.id;
      const info = await api("admin/resources?user=" + targetUser);
      if (
        epoch !== Session.epoch ||
        !lease.current() ||
        key !== grantRequest ||
        !Modules.isOpen("resourcegrant")
      )
        return;
      grantRoot.replaceChildren();
      const search = UI.field(
          grantRoot,
          "Поиск игрока",
          "grant-user-search",
          "",
          "search",
        ),
        users = n("div");
      grantRoot.append(users);
      let userSearchRequest = 0,
        page = 1;
      const drawUsers = async () => {
        const req = ++userSearchRequest;
        const result = await api(
          "admin/users?q=" + encodeURIComponent(search.value) + "&page=" + page,
        );
        if (
          req !== userSearchRequest ||
          epoch !== Session.epoch ||
          !lease.current() ||
          key !== grantRequest
        )
          return;
        users.replaceChildren();
        for (const u of result.users || result.items || [])
          users.append(
            button(u.username, () => {
              targetUser = u.id;
              AdminResources.render();
            }),
          );
      };
      search.oninput = () => {
        page = 1;
        drawUsers().catch((e) => {
          if (lease.current()) notify(e.message, "error");
        });
      };
      const userName = await api("admin/users?q=" + targetUser);
      if (epoch !== Session.epoch || key !== grantRequest || !lease.current())
        return;
      const selected = (userName.users || userName.items || []).find(
        (u) => u.id === targetUser,
      );
      grantRoot.append(
        n(
          "p",
          tr("Игрок") +
            ": " +
            (selected?.username || Session.data.state.user.username),
        ),
      );
      const form = n("form", undefined, "resource-grant-form");
      form.onsubmit = (e) => e.preventDefault();
      grantRoot.append(form);
      let target = "ship",
        ship = info.ships[0]?.id,
        company = info.companies[0]?.id,
        port = (Session.data.workbench.stations || [])[0],
        resource = Session.data.expedition.resources[0].id;
      const controls = n("div");
      UI.choice(
        form,
        "Место выдачи",
        [
          ["ship", "Трюм корабля"],
          ["warehouse", "Личный склад на станции"],
          ["company", "Склад корпорации"],
        ],
        target,
        (value) => {
          target = value;
          drawTargets();
        },
      );
      function drawTargets() {
        controls.replaceChildren();
        if (target === "ship")
          UI.choice(
            controls,
            "Корабль",
            info.ships.map((s) => [
              s.id,
              shipName(s) + " · " + (world.data?.stars[s.star - 1]?.name || ""),
            ]),
            ship,
            (value) => (ship = Number(value)),
          );
        else {
          if (target === "company")
            UI.choice(
              controls,
              "Корпорация",
              info.companies.map((c) => [c.id, "[" + c.tag + "] " + c.name]),
              company,
              (value) => (company = Number(value)),
            );
          if (port) {
            const label = n("label");
            label.append(n("span", tr("Станция")));
            stationPicker(
              label,
              Session.data.workbench.stations,
              stationKey(port),
              (value) => (port = parseStation(value)),
            );
            controls.append(label);
          }
        }
      }
      drawTargets();
      form.append(controls);
      UI.choice(
        form,
        "Ресурс",
        Session.data.expedition.resources.map((r) => [r.id, r.name]),
        resource,
        (value) => (resource = value),
      );
      const quantity = UI.field(
        form,
        "Количество",
        "grant-quantity",
        1,
        "number",
      );
      quantity.min = 1;
      quantity.max = 1000000;
      quantity.step = 1;
      form.append(
        button("Выдать ресурсы", async () => {
          if (!form.reportValidity()) return;
          await api("admin/resources", {
            user: targetUser,
            ship,
            company,
            target,
            star: port?.star,
            station: port?.station || port?.object_id,
            resource,
            quantity: Number(quantity.value),
          });
          if (!lease.current()) return;
          notify(tr("Ресурсы выданы"));
          await refresh(true);
        }),
      );
    },
    open(user) {
      targetUser = user || Session.data.state.user.id;
      Modules.open("resourcegrant");
    },
  });
  window.addEventListener("moduleopen", (e) => {
    if (e.detail === "resourcegrant")
      Modules.wait(e.detail, AdminResources.render()).catch((e) => {
        if (
          Session.signedIn &&
          Session.data.state?.user.role === "admin" &&
          Modules.isOpen("resourcegrant")
        )
          notify(e.message, "error");
      });
  });
  window.addEventListener("moduleclose", ({ detail }) => {
    if (detail === "resourcegrant") {
      view.invalidate();
      grantRequest++;
    }
  });
  window.addEventListener("signedout", () => {
    view.invalidate();

    grantRequest++;
    targetUser = 0;
    grantRoot.replaceChildren();
    Modules.close("resourcegrant");
  });
}
