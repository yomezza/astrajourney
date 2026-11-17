import { trace } from "./diagnostics.js";
// One authoritative client session boundary. Domain values remain server-owned.
export const Session = {
  signedIn: false,
  epoch: 0,
  data: {},
  snapshot: null,
  lastPoll: 0,
};
export function invalidateSession() {
 trace("session_invalidated", { sessionEpoch: Session.epoch });
  Session.epoch++;
  Session.signedIn = false;
  Session.data = {};
  Session.snapshot = null;
}

export class RequestScope {
  #generation = 0;
  #controller;
  constructor(active = () => Session.signedIn) {
    this.active = active;
  }
  begin({ abort = true } = {}) {
    this.invalidate();
    const generation = this.#generation,
      epoch = Session.epoch;
    this.#controller = abort ? new AbortController() : null;
    return {
      signal: this.#controller?.signal,
      current: () =>
        generation === this.#generation &&
        epoch === Session.epoch &&
        this.active(),
    };
  }
  invalidate() {
    this.#generation++;
    this.#controller?.abort();
    this.#controller = null;
  }
  dispose() {
    this.invalidate();
  }
}
