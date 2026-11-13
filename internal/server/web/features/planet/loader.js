import { Session } from "../../core/session.js";
import { api } from "../../core/api.js";
import { notify } from "../../ui/feedback.js";

import { scenes } from "../../features/runtime.js";

let loading;
export const loadPlanet = () =>
  (loading ||= import("./index.js")
    .then((module) => {
      module.initPlanet();
      return scenes.planet;
    })
    .catch((error) => {
      loading = null;
      throw error;
    }));
export const openPlanet = (star, planet) =>
  loadPlanet()
    .then((p) => p.open(star, planet))
    .catch((e) => notify(e.message, "error"));
export const adminPlanetSurface = (star, planet, action) =>
  loadPlanet()
    .then((p) => p.admin(star, planet, action))
    .catch((e) => notify(e.message, "error"));
export function initPlanetLoader() {
  let restoredEpoch = -1;
  window.addEventListener("gamedata", async () => {
    if (!Session.signedIn || restoredEpoch === Session.epoch) return;
    const epoch = Session.epoch;
    restoredEpoch = epoch;
    try {
      const state = await api("planet/visit");
      if (epoch !== Session.epoch || !Session.signedIn) return;
      if (
        [
          "active",
          "disconnected",
          "pending",
          "return_pending",
          "legacy",
        ].includes(state.state)
      ) {
        const module = await import("./visit.js");
        if (
          epoch === Session.epoch &&
          Session.signedIn &&
          !document.body.classList.contains("planet-mode")
        )
          await module.openVisit();
      }
    } catch (error) {
      if (epoch === Session.epoch && Session.signedIn)
        console.warn("Planet mode restoration:", error.message);
    }
  });

  document.addEventListener(
    "click",
    (event) => {
      if (
        event.target.closest(
          '#sidebar [data-open="galaxy"],#current-system-map',
        )
      )
        scenes.planet?.dismiss(false);
    },
    true,
  );
  document.getElementById("nav-colonies").onclick = () =>
    loadPlanet()
      .then((p) => p.manager())
      .catch((e) => notify(e.message, "error"));
}
