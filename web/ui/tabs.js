import { UI } from "./runtime.js";
import { I18n } from "../core/i18n.js";

let serial = 0;

export function selectTab(tabs, value, panel) {
  for (const button of tabs.children) {
    const selected = button.dataset.tab === value;
    button.classList.toggle("active", selected);
    button.setAttribute("aria-selected", String(selected));
    button.tabIndex = selected ? 0 : -1;
    if (panel && selected) panel.setAttribute("aria-labelledby", button.id);
  }
}

export function initTabs() {
  UI.tabs = (root, choices, value, change, { panel, label } = {}) => {
    const tabs = UI.node("div", undefined, "tabs");
    tabs.setAttribute("role", "tablist");
    if (label) {
      tabs.dataset.label = label;
      tabs.setAttribute("aria-label", I18n.t(label));
    }
    const prefix = "tabs-" + ++serial;
    if (panel) {
      panel.id ||= prefix + "-panel";
      panel.setAttribute("role", "tabpanel");
      panel.tabIndex = 0;
    }
    for (const [id, label] of choices) {
      const button = UI.node("button", I18n.t(label), "terminal-action");
      button.type = "button";
      button.dataset.i18n = label;
      button.onclick = () => change(id);
      button.id = `${prefix}-${id}`;
      button.dataset.tab = id;
      button.setAttribute("role", "tab");
      if (panel) button.setAttribute("aria-controls", panel.id);
      button.onkeydown = (event) => {
        const at = choices.findIndex(([key]) => key === id);
        let next;
        if (event.key === "ArrowRight") next = (at + 1) % choices.length;
        if (event.key === "ArrowLeft")
          next = (at + choices.length - 1) % choices.length;
        if (event.key === "Home") next = 0;
        if (event.key === "End") next = choices.length - 1;
        if (next === undefined) return;
        event.preventDefault();
        const target = tabs.querySelectorAll('[role="tab"]')[next];
        target.focus();
        target.click();
        if (!tabs.isConnected)
          root
            .querySelector(
              `[data-tab="${CSS.escape(String(choices[next][0]))}"]`,
            )
            ?.focus();
      };
      tabs.append(button);
    }
    selectTab(tabs, value, panel);
    root.append(tabs);
    return tabs;
  };
}
