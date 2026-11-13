import { api } from "./api.js";
import { Session } from "./session.js";

let current = null,
  revision = 0;
export const world = {
  stars: new Map(),
  get data() {
    return current;
  },
  set data(data) {
    current = data;
    revision++;
    world.stars = new Map((data?.stars || []).map((star) => [star.id, star]));
  },
};
let inFlight;
export function acceptWorld(data) {
  world.data = data;
  return data;
}
// Explicitly opt in only the galaxy metadata endpoint, never all GETs.
export function loadWorld(force = false) {
  if (world.data && !force) return Promise.resolve(world.data);
  const epoch = Session.epoch;
  if (inFlight?.epoch === epoch) return inFlight.promise;
  const entry = { epoch, revision };
  entry.promise = api("galaxy")
    .then((data) => {
      if (epoch !== Session.epoch || !Session.signedIn) return null;
      if (entry.revision === revision) acceptWorld(data);
      return world.data || data;
    })
    .finally(() => {
      if (inFlight === entry) inFlight = null;
    });
  inFlight = entry;
  return entry.promise;
}
window.addEventListener("signedout", () => {
  acceptWorld(null);
  inFlight = null;
});

window.addEventListener("gamedata", () => {
  if (
    Session.signedIn &&
    world.data &&
    Number(Session.data.workbench?.universe_revision) >
      Number(world.data.revision || 0)
  )
    loadWorld(true)
      .then((data) => {
        if (data) window.dispatchEvent(new Event("universechange"));
      })
      .catch(() => {});
});
