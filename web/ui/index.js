import { UI } from "./runtime.js";
import { node } from "./dom.js";
import { Format } from "../core/format.js";
import { initActions } from "./actions.js";
import { initFields } from "./fields.js";
import { initTabs } from "./tabs.js";
import { initTable } from "./table.js";
import { initDisclosure } from "./disclosure.js";
import { initCombobox } from "./combobox.js";
export function initControls() {
  UI.node = node;
  UI.formatNumber = Format.number;
  UI.percent = Format.percent;
  initActions();
  initFields();
  initTabs();
  initTable();
  initDisclosure();
  initCombobox();
}
