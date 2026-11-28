import { Modules } from "../../ui/runtime.js";
import { openColonies } from "../planet/loader.js";

let workspaceOpener;

export function registerWorkspaceOpener(open) {
  workspaceOpener = open;
}

export function openIntent({module, scope = "global", target, instance, presentation = "window", reuse = "matching"}) {
  if (document.body.classList.contains("planet-mode") && module !== "chat") return;
  if (scope === "workspace") return workspaceOpener?.({module,target,instance,presentation,reuse});
  if (scope === "existing") return Modules.focus(module);
  if (module === "system" || module === "galaxy") {
    const opening = Modules.open("galaxy");
    if (target?.kind === "system") {
      window.dispatchEvent(new CustomEvent("showstar",{detail:target.id}));
    } else {
      window.dispatchEvent(new Event(module === "system" ? "showcurrentsystem" : "showgalaxy"));
    }
    return opening;
  }
  if (Modules.isOpen(module)) return Modules.focus(module);
  if (module === "colonies") return openColonies();
  return Modules.open(module);
}

export function openModule(module) {
  return openIntent({module});
}
