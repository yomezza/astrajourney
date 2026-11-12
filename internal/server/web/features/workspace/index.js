import { createFleet } from "../fleet/index.js";
import { createMining } from "../mining/index.js";
import { createMarkets } from "../markets/index.js";
import { createCorporations } from "../corporations/index.js";
import { createProfile } from "../profile/index.js";
import { createAdministrationServices } from "../administration/services/index.js";
import { createAdministrationUsers } from "../administration/users/index.js";
import { createAdministrationGameplay } from "../administration/gameplay/index.js";
import { createAdministrationBenchmark } from "../administration/benchmark/index.js";
import { createNotifications } from "../notifications/index.js";
import { createGuide } from "../guide/index.js";
import { createAutomation } from "../automation/index.js";
import { createFinance } from "../finance/index.js";
import { createPortal } from "../portal/index.js";
import { Session } from "../../core/session.js";

import { I18n } from "../../core/i18n.js";

import { api } from "../../core/api.js";
import { refresh } from "../../core/snapshot-store.js";
import { UI, Modules } from "../../ui/runtime.js";
import { notify } from "../../ui/feedback.js";
import { Game, GalaxyAdmin } from "../runtime.js";
import { world, loadWorld } from "../../core/world-store.js";

import { d, w, num, money, known, fly } from "../selectors.js";

export function initExpedition() {
  const tr = (s) => I18n.t(s);
  const ids = [
      "ship",
      "cargo",
      "research",
      "market",
      "corporations",
      "profile",
      "users",
      "gameplay",
      "galaxies",
      "adminchanges",
      "notifications",
      "guide",
      "tasks",
      "finance",
      "benchmark",
      "services",
      "portal",
    ],
    roots = Object.fromEntries(
      ids.map((id) => [
        id,
        document.querySelector("#window-" + id + " .module-content"),
      ]),
    );

  async function act(path, body, message = "Действие выполнено") {
    if (busy) return false;
    const epoch = Session.epoch;
    busy = true;
    try {
      const result = await api(path, body);
      if (epoch !== Session.epoch || !Session.signedIn) return false;
      window.dispatchEvent(new Event("actionaccepted"));
      notify(
        result?.status === "deleting"
          ? "Корпорация удаляется. Ожидается очистка чата."
          : message,
      );
      await refresh(true);
      return true;
    } catch (error) {
      if (epoch !== Session.epoch) return false;
      notify(error.message, "error");
      if (error.status === 409) await refresh(true);
      return false;
    } finally {
      if (epoch === Session.epoch) {
        busy = false;
        renderVisible(true);
      }
    }
  }
  let busy = false;
  const command = (body, message) => act("workbench", body, message);
  const fleetFeature = createFleet({
    roots: { ship: roots.ship, cargo: roots.cargo },
    act,
    command,
  });
  const miningFeature = createMining({
    roots: { research: roots.research },
    act,
    command,
  });
  const marketsFeature = createMarkets({
    roots: { market: roots.market },
    act,
    command,
    renderShipyards: fleetFeature.renderShipyards,
  });
  const corporationsFeature = createCorporations({
    roots: { corporations: roots.corporations },
    act,
    command,
  });
  const profileFeature = createProfile({
    roots: { profile: roots.profile },
    act,
    command,
  });
  const usersFeature = createAdministrationUsers({
    roots: { users: roots.users },
    act,
    command,
  });
  const gameplayFeature = createAdministrationGameplay({
    roots: { gameplay: roots.gameplay },
    act,
    command,
  });
  const servicesFeature = createAdministrationServices({
    root: roots.services,
  });
  const benchmarkFeature = createAdministrationBenchmark({
    roots: { benchmark: roots.benchmark },
    act,
    command,
  });
  const notificationsFeature = createNotifications({
    roots: { notifications: roots.notifications },
    act,
    command,
  });
  const guideFeature = createGuide({
    roots: { guide: roots.guide },
    act,
    command,
    shipStats: fleetFeature.shipStats,
    filterShipyards: fleetFeature.filterShipyards,
  });
  const automationFeature = createAutomation({
    roots: { tasks: roots.tasks },
    act,
    command,
    deliveryDetails: marketsFeature.deliveryDetails,
  });
  const financeFeature = createFinance({
    roots: { finance: roots.finance },
    act,
    command,
  });
  const portalFeature = createPortal({
    roots: { portal: roots.portal },
    act,
    command,
  });

  const renderCargo = (...args) => fleetFeature.renderCargo(...args);

  const renderShip = (...args) => fleetFeature.renderShip(...args);

  const renderResearch = (...args) => miningFeature.renderResearch(...args);

  const renderMarket = (...args) => marketsFeature.renderMarket(...args);

  const renderCompanies = (...args) =>
    corporationsFeature.renderCompanies(...args);

  const renderProfile = (...args) => profileFeature.renderProfile(...args);
  const renderUsers = (...args) => usersFeature.renderUsers(...args);

  const renderGameplay = (...args) => gameplayFeature.renderGameplay(...args);
  const renderBenchmark = (...args) =>
    benchmarkFeature.renderBenchmark(...args);

  const loadNotifications = (...args) =>
    notificationsFeature.loadNotifications(...args);
  const notificationBadge = (...args) =>
    notificationsFeature.notificationBadge(...args);
  const renderNotifications = (...args) =>
    notificationsFeature.renderNotifications(...args);
  const renderGuide = (...args) => guideFeature.renderGuide(...args);
  const renderTasks = (...args) => automationFeature.renderTasks(...args);

  const renderFinance = (...args) => financeFeature.renderFinance(...args);
  const renderPortal = (...args) => portalFeature.renderPortal(...args);

  Object.assign(Game, {
    command,
    teleport(to) {
      return act(
        "admin/teleport",
        { to, ship: d().ship.id },
        "Телепортация выполнена",
      );
    },
    async explore(to) {
      const plan = await api("estimate?to=" + to);
      const text =
        (plan.path
          ?.map((id) => world.data?.stars.find((s) => s.id === id)?.name || id)
          .join(" → ") || tr("Нет маршрута")) +
        " · " +
        tr("Объекты") +
        ": " +
        plan.remaining +
        "/" +
        plan.objects +
        " · " +
        tr("Время") +
        ": " +
        (plan.travel_seconds + plan.scan_seconds) +
        " s · " +
        tr("Топливо") +
        ": " +
        plan.fuel +
        " · " +
        money(plan.cost) +
        (d().ship.fuel < plan.return_fuel
          ? " · " + tr("Топлива на обратный путь может не хватить")
          : "");
      UI.confirm("План экспедиции", text, () =>
        command({ action: "explore", to }),
      );
      if (!plan.can_depart) {
        document.querySelector(
          "#window-confirm .module-content button",
        ).disabled = true;
      }
    },
    known,
    openGuide(id) {
      guideFeature.open(id);
    },
    openShipyards(star, stationID) {
      marketsFeature.show("shipyards");
      if (star) fleetFeature.setYardLocation(star + "|" + stationID);
      Modules.open("market");
    },
    openTasks() {
      Modules.open("tasks");
    },
  });
  document.getElementById("nav-shipyards").onclick = () => Game.openShipyards();
  Game.performExpedition = (body) =>
    act(
      "expedition",
      { ship: d().ship.id, ...body },
      body.action === "mine"
        ? "Добыча запущена: груз поступит на следующем тике"
        : "Действие выполнено",
    );
  async function ensureMap() {
    if (!world.data) await loadWorld();
  }

  const renderers = {
    ship: renderShip,
    cargo: renderCargo,
    research: renderResearch,
    market: renderMarket,
    corporations: renderCompanies,
    profile: renderProfile,
    users: renderUsers,
    gameplay: renderGameplay,
    galaxies: () => GalaxyAdmin.render(roots.galaxies),
    adminchanges: () => GalaxyAdmin.logs(roots.adminchanges),
    notifications: renderNotifications,
    guide: renderGuide,
    tasks: renderTasks,
    finance: renderFinance,
    services: () => servicesFeature.render(),
    benchmark: renderBenchmark,
    portal: renderPortal,
  };
  const onDemand = new Set([
    "users",
    "gameplay",
    "galaxies",
    "adminchanges",
    "guide",
    "corporations",
    "services",
  ]);
  function renderVisible(force = false) {
    if (!Session.signedIn || !d()) return;
    for (const [id, render] of Object.entries(renderers)) {
      if (!Modules.isOpen(id) || (!force && onDemand.has(id))) continue;
      const root = roots[id];
      if (id === "tasks") {
        render();
        continue;
      }
      if (
        root.querySelector('[data-dirty="true"]') ||
        (!force &&
          root.contains(document.activeElement) &&
          document.activeElement.matches("input,select,.picker-button")) ||
        document.querySelector(".picker-popover")
      )
        continue;
      const scroll = root.scrollTop,
        epoch = Session.epoch;
      Promise.resolve(render())
        .then((accepted) => {
          if (accepted === false || epoch !== Session.epoch) return;
          root.scrollTop = scroll;
        })
        .catch((error) => {
          if (
            epoch === Session.epoch &&
            Session.signedIn &&
            Modules.isOpen(id) &&
            error.name !== "AbortError"
          )
            notify(error.message, "error");
        });
    }
  }
  window.addEventListener("moduleopen", async (e) => {
    if (!Session.signedIn) return;
    if (
      [
        "users",
        "gameplay",
        "benchmark",
        "galaxies",
        "adminchanges",
        "services",
      ].includes(e.detail) &&
      Session.data.state?.user.role !== "admin"
    ) {
      Modules.close(e.detail);
      return;
    }
    const epoch = Session.epoch;
    try {
      await ensureMap();
      if (
        epoch !== Session.epoch ||
        !Session.signedIn ||
        !Modules.isOpen(e.detail)
      )
        return;
      await renderers[e.detail]?.();
      if (
        epoch !== Session.epoch ||
        !Session.signedIn ||
        !Modules.isOpen(e.detail)
      )
        return;
    } catch (error) {
      if (
        epoch === Session.epoch &&
        Session.signedIn &&
        Modules.isOpen(e.detail) &&
        error.name !== "AbortError"
      )
        notify(error.message, "error");
    }
  });
  window.addEventListener("gamedata", () => {
    const model = roots.ship.querySelector(".ship-inline-model"),
      vessel =
        model &&
        (Session.data.fleet?.ships || []).find(
          (s) => s.id === Number(model.dataset.ship),
        );
    if (vessel)
      UI.updateHologram(
        model,
        vessel.spec.model,
        tr("Голограмма корабля") + " · " + tr(vessel.spec.name),
        fly(vessel),
      );
    notificationBadge();
    renderVisible();
    const population = w().population?.[0];
    document.getElementById("server-status").dataset.help = "Сервер";
    document.getElementById("server-status").tabIndex = 0;
    document.getElementById("server-status").dataset.helpRaw =
      tr("Сервер") +
      ": " +
      tr(w().server_name || "Иггдрасиль") +
      "\n" +
      tr("Игроки онлайн") +
      ": " +
      (population?.players || 0) +
      "\n" +
      tr("Боты") +
      ": " +
      (population?.bots || 0);
  });
  window.addEventListener("notificationchange", () =>
    loadNotifications().catch(() => {}),
  );
  window.addEventListener("languagechange", () => renderVisible(true));
  window.addEventListener("moduleclose", (e) => {
    if (e.detail === "market") marketsFeature.close();
    ({
      users: usersFeature,
      gameplay: gameplayFeature,
      benchmark: benchmarkFeature,
      services: servicesFeature,
      corporations: corporationsFeature,
    })[e.detail]?.close?.();
  });
  window.addEventListener("signedout", () => {
    busy = false;
    fleetFeature.clear();
    miningFeature.clear();
    marketsFeature.clear();
    corporationsFeature.clear();
    profileFeature.clear();
    usersFeature.clear();
    gameplayFeature.clear();
    benchmarkFeature.clear();
    servicesFeature.clear();
    notificationsFeature.clear();
    guideFeature.clear();
    automationFeature.clear();
    financeFeature.clear();
    portalFeature.clear();
  });
  window.addEventListener("stationmarket", () => marketsFeature.show("local"));
  setInterval(() => {
    document.querySelectorAll("[data-reset-at]").forEach((el) => {
      const sec = Math.max(
        0,
        Math.ceil(
          (new Date(el.dataset.resetAt).getTime() -
            Date.now() -
            (Session.data.serverOffset || 0)) /
            1000,
        ),
      );
      el.textContent =
        (el.dataset.remaining !== undefined && el.dataset.capacity !== undefined
          ? num(el.dataset.remaining) + " / " + num(el.dataset.capacity) + " · "
          : "") +
        [Math.floor(sec / 3600), Math.floor(sec / 60) % 60, sec % 60]
          .map((v) => String(v).padStart(2, "0"))
          .join(":");
    });
    document
      .querySelectorAll("[data-job-started]")
      .forEach(
        (el) =>
          (el.value = Math.min(
            99,
            Math.max(
              1,
              ((Date.now() +
                (Session.data.serverOffset || 0) -
                new Date(el.dataset.jobStarted).getTime()) /
                Math.max(
                  1,
                  new Date(el.dataset.jobEnd).getTime() -
                    new Date(el.dataset.jobStarted).getTime(),
                )) *
                100,
            ),
          )),
      );
  }, 500);
}
