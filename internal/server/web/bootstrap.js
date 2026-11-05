import { initTooltip } from "./ui/tooltip.js";
import { initI18n, I18n } from "./core/i18n.js";
import { Session } from "./core/session.js";
import { api } from "./core/api.js";
import { refresh } from "./core/snapshot-store.js";
import { UI, Modules } from "./ui/runtime.js";
import { Game, scenes } from "./features/runtime.js";
import { notify } from "./ui/feedback.js";
import { initWindows } from "./ui/windows.js";
import { initMenu } from "./ui/menu.js";
import { initConfirm } from "./ui/confirm.js";
import { initObjectCards } from "./features/objects/cards.js";
import { initVisuals } from "./features/objects/visuals.js";
import { initPlanetLoader } from "./features/planet/loader.js";
import { initControls } from "./ui/index.js";
import { initHolograms } from "./ui/holograms.js";
import { initStationLabels } from "./features/stations/index.js";
import { initIcons } from "./features/shell/icons.js";
import { initServerStatus } from "./features/shell/server-status.js";
import { initApp } from "./features/shell/app.js";
import { initGalaxyAdmin } from "./features/administration/galaxies/index.js";
import { initScheduler } from "./features/automation/scheduler.js";
import { initAdminResources } from "./features/administration/resources/index.js";
import { initNavigation } from "./features/shell/navigation.js";
import { initLogistics } from "./features/inventory/logistics.js";
import { initIdeas } from "./features/feedback/ideas.js";
import { initExpedition } from "./features/workspace/index.js";
import { initStudies } from "./features/research/index.js";
import { initGalaxy } from "./features/maps/galaxy.js";

import { initChat } from "./features/chat/index.js";

initI18n();
initTooltip();
initControls();
initWindows();
initNavigation();
initMenu();
initConfirm();
initObjectCards();
initVisuals();
initPlanetLoader();
initIcons();
initServerStatus();
initHolograms();
initStationLabels();
const app = initApp();
initGalaxyAdmin();
initScheduler();
initAdminResources();
initLogistics();
initIdeas();
initExpedition();
initStudies();
initGalaxy();
initChat();

export const application = {
  session: Session,
  I18n,
  UI,
  Modules,
  Game,
  scenes,
  api,
  refresh,
  notify,
  ...app,
};

document.addEventListener("keydown", (event) => {
  const menu = event.target.closest?.(".dropdown");
  if (event.key === "Escape" && menu?.open) {
    event.stopPropagation();
    menu.open = false;
    menu.querySelector("summary")?.focus();
  }
});
