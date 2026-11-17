import { api } from "./api.js";
import { Session } from "./session.js";

const fields = [
  "state",
  "profile",
  "expedition",
  "fleet",
  "gameplay",
  "workbench",
  "notifications",
];
let ongoing,
  present = (data) => data;
export function configureSnapshot(presentation) {
  present = presentation;
}
export function refresh(force = false) {
  const epoch = Session.epoch;
  if (ongoing?.epoch === epoch)
    return force
      ? ongoing.promise.then(() => Session.epoch === epoch && refresh(false))
      : ongoing.promise;
  const entry = { epoch };
  entry.promise = load(epoch).finally(() => {
    if (ongoing === entry) ongoing = null;
  });
  ongoing = entry;
  return entry.promise;
}
async function load(epoch) {
  Session.lastPoll = Date.now();
  const controller = new AbortController(),
    timer = setTimeout(() => controller.abort(), 15000);
  try {
    const values = await Promise.all(
      fields.map((path) => api(path, undefined, controller.signal)),
    );
    if (epoch !== Session.epoch) return false;
    const [state, profile, expedition, fleet, clock, workbench, notifications] =
      values;
    const data = {
      state,
      profile,
      expedition,
      fleet,
      clock,
      workbench,
      notifications,
      serverOffset: new Date(clock.server_time).getTime() - Date.now(),
    };
    Session.snapshot = data;
    Session.data = present(data);
    Session.signedIn = true;
    window.dispatchEvent(new Event("snapshotaccepted"));
    return true;
  } catch (error) {
    if (epoch !== Session.epoch) return false;
    window.dispatchEvent(new CustomEvent("connectionerror", { detail: error }));
    return false;
  } finally {
    clearTimeout(timer);
    controller.abort();
  }
}
