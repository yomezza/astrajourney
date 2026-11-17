import { createLocalMarket } from "./local.js";
import { createGlobalMarket } from "./global.js";
import { createOrdersMarket } from "./orders.js";
import { createDataMarket } from "./data.js";
import { createTradeForm } from "./trade-form.js";
import { createDeliveryMarket } from "./delivery.js";

import { loadWorld } from "../../core/world-store.js";
import { FormState } from "../../ui/form-state.js";
import { Session, RequestScope } from "../../core/session.js";
import { scopedView } from "../../core/scoped-view.js";
import { I18n } from "../../core/i18n.js";

import { api } from "../../core/api.js";

import { UI, Modules } from "../../ui/runtime.js";
import { notify } from "../../ui/feedback.js";

import { d, station } from "../../features/selectors.js";
import { reset } from "../../ui/view-state.js";

export function createMarkets({ roots, act, command, renderShipyards }) {
  const drafts = new FormState(
    () =>
      `${Session.data.state?.user.id}:${state.tab}:${state.resource}:${state.target || ""}`,
  );
  const n = UI.node,
    tr = (s) => I18n.t(s),
    b = UI.button,
    field = (...args) => drafts.field(UI.field, ...args);
  const state = { tab: "local", resource: "agni", order: null, target: null };
  let marketRequestID = 0;
  const { tradeForm } = createTradeForm({ field, act, command });
  const { renderDeliveries, deliveryDetails } = createDeliveryMarket({
    field,
    act,
    command,
  });
  const deps = {
    state,
    field,
    renderMarket: (...args) => renderMarket(...args),
    tradeForm,
    act,
    command,
  };
  const views = {
    local: createLocalMarket(deps),
    global: createGlobalMarket(deps),
    orders: createOrdersMarket(deps),
    data: createDataMarket(deps),
  };
  const renderMarketScope = new RequestScope(
    () => Session.signedIn && Modules.isOpen("market"),
  );

  async function drawMarket(lease) {
    const root = roots.market,
      key = ++marketRequestID,
      epoch = Session.epoch,
      tab = state.tab,
      current = () =>
        lease.current() &&
        key === marketRequestID &&
        epoch === Session.epoch &&
        Session.signedIn &&
        Modules.isOpen("market") &&
        tab === state.tab;
    if (tab === "global" || tab === "shipyards") {
      await loadWorld();
      if (!current()) return false;
    }
    let globalMarket;
    if (tab === "global") {
      try {
        globalMarket = await api("market/global");
      } catch (error) {
        if (!current()) return false;
        throw error;
      }
    }
    // Keep the current table while loading; only the latest render may replace it.
    if (!current()) return false;
    Modules.title("market", tr(tab === "shipyards" ? "Верфи" : "Рынок"));
    reset(root);
    const port = station();
    root.append(
      n(
        "div",
        (port?.name || d().system.name) + " · " + d().system.name,
        "location-strip",
      ),
    );
    const panel = n("section", undefined, "market-tab-panel");
    UI.tabs(
      root,
      [
        ["local", "Локальный рынок"],
        ["global", "Глобальный рынок"],
        ["orders", "Предложения игроков"],

        ["data", "Разведданные"],
        ["delivery", "Перевозки"],
        ["shipyards", "Верфи"],
      ],
      tab,
      (id) => {
        state.tab = id;
        return renderMarket();
      },
      { panel },
    );
    root.append(panel);
    if (tab === "shipyards") {
      renderShipyards(panel);
      return;
    }
    if (tab === "local") return views.local(panel, { port, globalMarket });
    if (tab === "global") return views.global(panel, { port, globalMarket });
    if (tab === "warehouse") {
      UI.empty(panel, "Управление запасами перенесено в логистику");
      panel.append(b("Открыть логистику", () => Modules.open("logistics")));
    }
    if (tab === "orders") return views.orders(panel, { port, globalMarket });
    if (tab === "data") return views.data(panel, { port, globalMarket });
    if (tab === "delivery") {
      renderDeliveries(panel);
    }
  }

  const renderMarket = scopedView(renderMarketScope, drawMarket, (error) =>
    notify(error.message, "error"),
  );
  function clear() {
    state.target = null;
    drafts.clear();
    state.resource = "agni";
    state.order = null;
    marketRequestID = 0;
    renderMarketScope.invalidate();
  }
  window.addEventListener("actionaccepted", () => drafts.clear());
  return {
    tradeForm,
    renderMarket,
    renderDeliveries,
    deliveryDetails,
    clear,
    show(tab) {
      state.tab = tab;
      marketRequestID++;
    },
    close() {
      marketRequestID++;
      renderMarket.invalidate();
    },
  };
}
