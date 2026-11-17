import { UI } from "./runtime.js";
let serial = 0;
export function initTabs() {
  UI.tabs = (root, choices, value, change, { panel } = {}) => {
    const tabs = UI.node("div", undefined, "tabs");
    tabs.setAttribute("role", "tablist");
    const prefix = "tabs-" + ++serial;
    if (panel) {
      panel.id ||= prefix + "-panel";
      panel.setAttribute("role", "tabpanel");
      panel.tabIndex = 0;
    }
    for (const [id, label] of choices) {
      const selected = value === id;
      const button = UI.button(label, () => change(id));
      button.id = `${prefix}-${id}`;
      button.dataset.tab = id;
      button.classList.toggle("active", selected);
      button.setAttribute("role", "tab");
      button.setAttribute("aria-selected", String(selected));
      button.tabIndex = selected ? 0 : -1;
      if (panel) {
        button.setAttribute("aria-controls", panel.id);
        if (selected) panel.setAttribute("aria-labelledby", button.id);
      }
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
        requestAnimationFrame(() =>
          root
            .querySelector(
              `[data-tab="${CSS.escape(String(choices[next][0]))}"]`,
            )
            ?.focus(),
        );
      };
      tabs.append(button);
    }
    root.append(tabs);
    return tabs;
  };
}
