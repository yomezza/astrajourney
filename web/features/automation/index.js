import { FormState } from "../../ui/form-state.js";
import { Session } from "../../core/session.js";

import { I18n } from "../../core/i18n.js";

import { UI } from "../../ui/runtime.js";
import { Scheduler } from "../../features/runtime.js";

import { w } from "../../features/selectors.js";
import { reset } from "../../ui/view-state.js";

export function createAutomation({ roots, act, command, deliveryDetails }) {
  const drafts = new FormState(() => Session.data.state?.user.id);
  const tr = (s) => I18n.t(s),
    b = UI.button;
  function renderTasks() {
    const container = roots.tasks;
    if (!container.querySelector(".planner")) reset(container);
    [...container.children]
      .filter((e) => !e.classList.contains("planner"))
      .forEach((e) => e.remove());
    const ready = Scheduler.render(container);
    for (const delivery of (w().deliveries || []).filter(
      (j) =>
        (j.owner === Session.data.state.user.id ||
          j.carrier === Session.data.state.user.id) &&
        ["bot", "carrying", "open"].includes(j.status),
    ))
      container.append(
        b(
          "#" +
            delivery.id +
            " · " +
            tr(delivery.status) +
            " · " +
            delivery.from_star +
            " → " +
            delivery.to_star,
          () => deliveryDetails(delivery),
        ),
      );
    return ready;
  }
  function clear() {
    drafts.clear();
  }
  window.addEventListener("actionaccepted", () => drafts.clear());
  return { renderTasks, clear };
}
