import { api } from "../../core/api.js";
import { I18n } from "../../core/i18n.js";
import { Session } from "../../core/session.js";
import { refresh } from "../../core/snapshot-store.js";
import { uuid } from "../../core/uuid.js";
import { notify } from "../../ui/feedback.js";

const confirmedRejections = new Set([
  "INVALID_ACTION_ID", "ACTION_ID_REUSED", "SHIP_NOT_OWNED", "OBJECT_UNAVAILABLE",
  "SHIP_CONTEXT_CHANGED", "SHIP_RESERVED", "SHIP_BUSY", "SHIP_IN_TRANSIT",
  "ACTION_REJECTED", "MARKET_PRICE_CHANGED",
]);

export function createTradeCommands(onChange) {
  const storageKey = account => "astra:pending-station-trade:" + account;
  let account, pending, busy = false;

  function restore() {
    const owner = Session.signedIn ? Session.data.state?.user?.id : null;
    if (account === owner) return;
    account = owner;
    busy = false;
    pending = null;
    if (!account) return;
    try {
      pending = JSON.parse(sessionStorage.getItem(storageKey(account)));
    } catch {
      // A browser may disable session storage. The in-memory request remains usable.
    }
  }

  function setPending(command) {
    pending = command;
    try {
      if (command) sessionStorage.setItem(storageKey(account), JSON.stringify(command));
      else sessionStorage.removeItem(storageKey(account));
    } catch {
      // Keep the current tab usable when session storage is unavailable.
    }
    onChange();
  }

  async function resolve(checkOnly) {
    restore();
    if (!pending || busy) return;
    const command = pending;
    const epoch = Session.epoch;
    const current = () => Session.epoch === epoch && Session.signedIn;
    busy = true;
    onChange();
    try {
      const signal = AbortSignal.timeout(12000);
      const receipt = checkOnly
        ? await api("expedition/commands/" + command.request_id, undefined, signal)
        : await api("expedition?ship=" + command.ship, command, signal);
      if (!current()) return;
      if (receipt.request_id !== command.request_id || receipt.ship !== command.ship || receipt.action !== command.action) {
        notify(I18n.t("Результат сделки пока неизвестен"), "error");
        return;
      }
      setPending(null);
      notify(I18n.t("Сделка подтверждена"));
      await refresh(true);
    } catch (error) {
      if (!current()) return;
      if (!checkOnly && confirmedRejections.has(error.code)) {
        await refresh(true);
        if (!current()) return;
        setPending(null);
        notify(error.message, "error");
      } else if (error.code === "ACTION_UNCONFIRMED") {
        notify(error.message, "error");
      } else {
        notify(I18n.t("Результат сделки пока неизвестен"), "error");
      }
    } finally {
      if (current()) {
        busy = false;
        onChange();
      }
    }
  }

  return {
    state() {
      restore();
      return {pending, busy};
    },
    async submit(data, body) {
      restore();
      const ship = data.ship;
      if (!account || Session.data.state?.station_trade_receipts !== true || pending || busy || !Session.data.fleet?.ships.some(({id}) => id === ship.id)) return false;
      setPending({
        ...body, request_id: uuid(), ship: ship.id,
        expected_star: ship.star, expected_object: ship.object_id,
      });
      await resolve(false);
      return Session.signedIn;
    },
    check: () => resolve(true),
    retry: () => resolve(false),
    clear() {
      account = undefined;
      pending = null;
      busy = false;
    },
  };
}
