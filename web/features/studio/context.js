import { api } from "../../core/api.js";
import { Session } from "../../core/session.js";
import { refresh } from "../../core/snapshot-store.js";
import { I18n } from "../../core/i18n.js";
import { notify } from "../../ui/feedback.js";

// Share one contextual read per ship and accepted game snapshot across cards and windows.
export function createStudioContext(onRead) {
  const reads = new Map();
  let controller = new AbortController();
  function load(id) {
    if (!id || !Session.signedIn) return;
    const accepted = Session.snapshot;
    const existing = reads.get(id);
    if (existing?.accepted === accepted) return;
    const ticket = { accepted, data:existing?.data, pending:true };
    const epoch = Session.epoch;
    const signal = controller.signal;
    reads.set(id,ticket);
    api(`expedition?ship=${id}`,undefined,signal).then(data => {
      if (signal.aborted || epoch !== Session.epoch || reads.get(id) !== ticket) return;
      ticket.data = data.ship?.id === id ? data : undefined;
      ticket.pending = false;
      onRead();
    }).catch(error => {
      if (signal.aborted || epoch !== Session.epoch || reads.get(id) !== ticket) return;
      ticket.pending = false;
      ticket.data = undefined;
      ticket.error = error.message;
      onRead();
    });
  }
  return {
    get(id) {
      if (!id) return {};
      const active = Session.data.expedition;
      if (active?.ship?.id === id) return {data:active};
      load(id);
      return reads.get(id) || {};
    },
    async run(data, body) {
      const ship = data.ship;
      const epoch = Session.epoch;
      if (!Session.data.fleet?.ships.some(({id}) => id === ship.id)) return false;
      try {
        await api(`expedition?ship=${ship.id}`, {
          ...body, ship:ship.id, expected_star:ship.star, expected_object:ship.object_id,
        });
        if (Session.epoch !== epoch) return false;
        notify(I18n.t("Действие выполнено"));
        await refresh(true);
        return true;
      } catch (error) {
        if (Session.epoch !== epoch) return false;
        notify(error.message,"error");
        if (error.status === 409 || error.status === 404) await refresh(true);
        return false;
      }
    },
    clear() {
      controller.abort();
      controller = new AbortController();
      reads.clear();
    },
  };
}
