import { Session, RequestScope } from "../../core/session.js";
import { I18n } from "../../core/i18n.js";
import { api } from "../../core/api.js";
import { uuid } from "../../core/uuid.js";
import { loadWorld } from "../../core/world-store.js";
import { UI } from "../../ui/runtime.js";
import { notify } from "../../ui/feedback.js";

export function discoveryPanel(star) {
  const n = UI.node,
    tr = (s) => I18n.t(s),
    root = n("section", undefined, "discovery-names"),
    scope = new RequestScope();
  root.dataset.star = String(star);
  let pending = false,
    previous = "";
  const labels = {
    claimed: "Первое открытие",
    unclaimed: "Ещё не открыто",
    protected: "Территория основателя",
    legacy_unattributed: "Историческое исследование",
    retired: "Объект удалён",
  };
  async function refresh() {
    if (
      pending ||
      !Session.signedIn ||
      (root.contains(document.activeElement) &&
        document.activeElement.matches("input,textarea"))
    )
      return;
    const task = scope.begin();
    try {
      const data = await api("system?id=" + star, undefined, task.signal);
      if (!task.current()) return;
      const key = JSON.stringify([I18n.language, data.discoveries]);
      if (key === previous) return;
      previous = key;
      root.replaceChildren(n("h3", tr("Первооткрыватели и названия")));
      for (const item of data.discoveries || []) {
        const card = n("div", undefined, "discovery-record"),
          tag = n(
            "span",
            tr(
              labels[
                item.protected_founder_key ? "protected" : item.claim_state
              ] || "Первое открытие",
            ),
            "study-source",
          );
        card.append(
          n("strong", item.current_name),
          n("small", item.catalog_code, "muted"),
          tag,
        );
        if (item.discoverer)
          card.append(n("p", tr("Первооткрыватель") + ": " + item.discoverer));
        const admin = Session.data.state?.user.role === "admin";
        if (item.rename_available || admin) {
          const form = n("div", undefined, "discovery-name-form"),
            input = n("input"),
            reason = n("input");
          input.value = item.current_name;
          input.maxLength = 120;
          input.setAttribute("aria-label", tr("Новое название"));
          reason.placeholder = tr("Причина исправления");
          let operation = uuid();
          input.oninput = reason.oninput = () => {
            operation = uuid();
          };
          const submit = async (correction) => {
            if (pending) return;
            if (
              !correction &&
              !(await UI.confirm("Это однократное переименование. Продолжить?"))
            )
              return;
            pending = true;
            const epoch = Session.epoch;
            for (const control of form.querySelectorAll("button,input"))
              control.disabled = true;
            try {
              await api(
                correction ? "admin/discovery/name" : "discovery/name",
                {
                  operation,
                  object: {
                    kind: item.kind,
                    star_id: item.star_id,
                    planet_id: item.planet_id,
                  },
                  name: input.value,
                  ...(correction ? { reason: reason.value } : {}),
                },
              );
              if (epoch !== Session.epoch || !Session.signedIn) return;
              await loadWorld(true);
              if (epoch !== Session.epoch) return;
              window.dispatchEvent(new Event("universechange"));
              notify("Название сохранено");
              pending = false;
              await refresh();
            } catch (error) {
              if (epoch === Session.epoch) notify(error.message, "error");
            } finally {
              if (epoch === Session.epoch) {
                pending = false;
                for (const control of form.querySelectorAll("button,input"))
                  control.disabled = false;
              }
            }
          };
          form.append(input);
          if (item.rename_available)
            form.append(UI.button("Назвать объект", () => submit(false)));
          if (admin) {
            form.append(
              reason,
              UI.button("Исправить название", () => submit(true)),
            );
          }
          card.append(form);
        }
        root.append(card);
      }
    } catch (error) {
      if (task.current())
        root.replaceChildren(
          n("p", error.message, "muted"),
          UI.button("Повторить", refresh),
        );
    }
  }
  refresh();
  return {
    element: root,
    refresh,
    close() {
      scope.invalidate();
      root.remove();
    },
  };
}
