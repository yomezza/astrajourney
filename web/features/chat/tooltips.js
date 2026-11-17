import { registerTooltip } from "../../ui/tooltip.js";
import { node as n } from "../../ui/dom.js";
import { I18n } from "../../core/i18n.js";
const tr = (s) => I18n.t(s);
// Reuse the terminal's positioning/lifecycle; v13 supplies only the chat content.
export function initChatTooltips(health) {
  registerTooltip(".chat-signal", () => {
    const state = health(),
      panel = n("div", undefined, "chat-status-tooltip"),
      list = n("div", undefined, "chat-status-tooltip-list");
    panel.append(n("div", tr("Статус служб"), "chat-status-tooltip-title"));
    for (const [label, available] of [
      ["Cosmos", state.cosmic],
      ["Planet", state.planet],
    ]) {
      const row = n("div", undefined, "chat-status-tooltip-row"),
        square = n(
          "span",
          undefined,
          "chat-service-square " + (available ? "online" : "offline"),
        );
      square.setAttribute("aria-hidden", "true");
      row.append(
        square,
        n("span", label, "chat-service-name"),
        n(
          "span",
          tr(available ? "Онлайн" : "Недоступна"),
          "chat-service-state" + (available ? "" : " offline"),
        ),
      );
      list.append(row);
    }
    const credit = n("div", undefined, "chat-status-credit"),
      brand = n("span", undefined, "chat-status-credit-flame"),
      flames = n("span", undefined, "chat-status-flames"),
      heart = n("span", "♥︎", "chat-status-credit-heart");
    flames.setAttribute("aria-hidden", "true");
    for (let i = 0; i < 6; i++)
      flames.append(n("span", undefined, "chat-status-flame"));
    heart.setAttribute("aria-hidden", "true");
    brand.append(
      flames,
      n("span", "Mirana", "chat-status-credit-brand"),
      heart,
    );
    credit.append(n("span", "Powered by"), brand);
    panel.append(list, n("hr", undefined, "chat-status-divider"), credit);
    return panel;
  });
  registerTooltip(".chat-read-dot", (target) => {
    const read = target.classList.contains("read"),
      panel = n("div", undefined, "chat-read-tooltip"),
      title = n("div", undefined, "chat-read-tooltip-title"),
      dot = n(
        "span",
        undefined,
        "chat-read-tooltip-indicator" + (read ? " read" : ""),
      );
    dot.setAttribute("aria-hidden", "true");
    title.append(
      dot,
      n(
        "span",
        tr(read ? "Прочитано хотя бы одним участником" : "Ещё не прочитано"),
      ),
    );
    panel.append(title);
    return panel;
  });
}
