import { Modules } from "../../ui/runtime.js";
import { openColonies } from "../planet/loader.js";

export function openModule(id) {
  if (document.body.classList.contains("planet-mode") && id !== "chat") return;
  if (id === "system" || id === "galaxy") {
    const opening = Modules.open("galaxy");
    window.dispatchEvent(new Event(id === "system" ? "showcurrentsystem" : "showgalaxy"));
    return opening;
  }
  if (Modules.isOpen(id)) return Modules.focus(id);
  if (id === "colonies") return openColonies();
  return Modules.open(id);
}
