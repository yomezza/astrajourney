import { I18n } from "../../core/i18n.js";

import { UI } from "../../ui/runtime.js";

import { Game } from "../../features/runtime.js";
import { world } from "../../core/world-store.js";

import { w, res, fmtTime, fly } from "../../features/selectors.js";

const n = (...args) => UI.node(...args),
  tr = (s) => I18n.t(s),
  b = (...args) => UI.button(...args);
export const shipActivity = (card, s, hasQueue = false) => {
  if (s.destination)
    card.append(
      n(
        "p",
        `${world.data?.stars[s.star - 1]?.name || s.star} → ${world.data?.stars[s.destination - 1]?.name || s.destination} · ${fmtTime(s.arrival)}`,
      ),
    );
  if (s.local_target)
    card.append(
      n(
        "p",
        tr("Манёвр") +
          " → " +
          s.local_target +
          " · " +
          fmtTime(s.local_arrival),
      ),
    );
  if (s.waypoints?.length)
    card.append(
      n(
        "p",
        tr("Маршрут") +
          ": " +
          s.waypoints
            .map((id) => world.data?.stars[id - 1]?.name || id)
            .join(" → "),
      ),
      b("Отменить оставшийся маршрут", () =>
        Game.command({ action: "cancel_route", ship: s.id }),
      ),
    );
  const research = w().research?.find((j) => j.ship === s.id);
  if (research) {
    card.append(
      UI.bar(research.scanned, research.total, "Объекты"),
      n("p", tr(research.paused ? "Приостановлено" : "Сканирование")),
      b(research.paused ? "Продолжить" : "Приостановить", () =>
        Game.command({
          action: research.paused ? "resume_research" : "pause_research",
          ship: s.id,
        }),
      ),
    );
  }
  for (const job of (w().mining || []).filter((j) => j.ship_id === s.id))
    card.append(
      n(
        "p",
        tr("Добыча") +
          " · " +
          tr(res(job.resource).name) +
          " × " +
          job.quantity,
      ),
    );
  if (
    !hasQueue &&
    !fly(s) &&
    !research &&
    !w().mining?.some((j) => j.ship_id === s.id)
  )
    card.append(n("p", tr("Ожидает задания"), "muted"));
};
