import { createModulesMarket } from "./modules.js";
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
import { selectTab } from "../../ui/tabs.js";

const marketTabs = [
  ["local", "Локальный рынок"],
  ["global", "Глобальный рынок"],
  ["orders", "Предложения игроков"],
  ["data", "Разведданные"],
  ["delivery", "Перевозки"],
  ["shipyards", "Верфи"],
  ["modules", "Модули корабля"],
];

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
  let marketRequestID = 0, shell;
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
    modules: createModulesMarket(deps),
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
    const previous = shell?.panel.isConnected ? shell : null;
    const pending = previous && previous.tab !== tab;
    if (pending) previous.panel.setAttribute("aria-busy","true");
    let globalMarket;
    try {
      if (tab === "global" || tab === "shipyards") {
        await loadWorld();
        if (!current()) return false;
      }
      if (tab === "global")
        globalMarket = await api("market/global");
    } catch (error) {
      if (!current()) return false;
      previous?.panel.removeAttribute("aria-busy");
      if (pending) state.tab = previous.tab;
      throw error;
    }
    // Keep the current table while loading; only the latest render may replace it.
    if (!current()) return false;
    Modules.title("market", tr(tab === "shipyards" ? "Верфи" : "Рынок"));
    const port = station();
    const system = d().system.name;
    const location = port?.name && port.name !== system ? `${port.name} · ${system}` : system;
    if (!previous) {
      reset(root);
      const navigation = n("div", undefined, "market-navigation");
      const strip = n("div", undefined, "location-strip");
      const panel = n("section", undefined, "market-tab-panel");
      navigation.append(strip);
      const tabs = UI.tabs(navigation, marketTabs, tab, id => {
        if (id === state.tab) return;
        state.tab = id;
        return renderMarket();
      }, { panel, label: "Рынок" });
      root.append(navigation,panel);
      shell = {strip, panel, tabs};
    }
    const {strip,panel,tabs} = shell;
    if (strip.textContent !== location) strip.textContent = location;
    panel.removeAttribute("aria-busy");
    selectTab(tabs,tab,panel);
    if (tab === "modules" && shell.tab === tab && panel.dataset.language === I18n.language)
      return views.modules(panel, current);
    shell.tab = tab;
    reset(panel);
    delete panel.dataset.modulesMarket;
    delete panel.dataset.language;
    if (tab === "modules") {
      panel.dataset.modulesMarket = "";
      panel.dataset.language = I18n.language;
      return views.modules(panel, current);
    }
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
    state.moduleLocation = "";
    state.moduleKind = "";
    state.moduleRating = "";
    drafts.clear();
    state.resource = "agni";
    state.order = null;
    state.tab = "local";
    shell = null;
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
      shell?.panel.removeAttribute("aria-busy");
      renderMarket.invalidate();
    },
  };
}
