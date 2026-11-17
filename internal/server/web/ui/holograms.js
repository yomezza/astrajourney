import { assetURL } from "../core/assets.js";
import { AssetCache } from "../core/asset-cache.js";

import { I18n } from "../core/i18n.js";
import { UI } from "./runtime.js";

export function initHolograms() {
  const templates = new AssetCache(64),
    states = new WeakMap();
  let serial = 0;
  function template(source) {
    return templates.load(source, async () => {
      const response = await fetch(assetURL(source), { cache: "force-cache" });
      if (!response.ok) throw Error("Hologram unavailable");
      const svg = new DOMParser().parseFromString(
        await response.text(),
        "image/svg+xml",
      ).documentElement;
      if (svg.localName !== "svg") throw Error("Invalid hologram");
      return svg;
    });
  }
  function thrust(state) {
    const viewport = state.svg?.getAttribute(
      state.moving ? "data-moving-viewbox" : "data-idle-viewbox",
    );
    if (viewport) state.svg.setAttribute("viewBox", viewport);
    state.svg
      ?.querySelectorAll(".engine-thrust")
      .forEach((group) =>
        group.setAttribute("visibility", state.moving ? "visible" : "hidden"),
      );
  }
  const motion = matchMedia("(prefers-reduced-motion: reduce)");
  const updateMotion = () => {
    for (const svg of document.querySelectorAll(".hologram-surface svg")) {
      if (motion.matches || document.hidden) svg.pauseAnimations?.();
      else {
        svg.setCurrentTime?.(performance.now() / 1000);
        svg.unpauseAnimations?.();
      }
    }
  };
  motion.addEventListener("change", updateMotion);
  document.addEventListener("visibilitychange", updateMotion);
  UI.updateHologram = (holder, source, label, moving = false) => {
    holder.setAttribute("role", "img");
    holder.setAttribute("aria-label", label);
    let state = states.get(holder);
    if (state?.source === source && !state.failed) {
      state.moving = moving;
      thrust(state);
      return;
    }
    state = { source, moving };
    states.set(holder, state);
    holder.replaceChildren();
    holder.dataset.model = source;
    if (!/^[a-z][a-z0-9-]*\.svg$/.test(source) && !/^\/?assets\/sha256\/[a-f0-9]{64}\/space\/(ships|objects)\/[a-z0-9-]+\.svg$/.test(source)) return;
    if (source.startsWith("assets/sha256/")) source = "/" + source;
    template(source)
      .then((original) => {
        if (states.get(holder) !== state || !holder.isConnected) return;
        const svg = document.importNode(original, true),
          prefix = "holo-" + ++serial + "-";
        // SVG fragments share the HTML document: each instance needs private IDs.
        for (const element of svg.querySelectorAll("[id]"))
          element.id = prefix + element.id;
        for (const element of [svg, ...svg.querySelectorAll("*")]) {
          for (const attribute of [...element.attributes]) {
            if (
              attribute.localName === "href" &&
              attribute.value.startsWith("#")
            )
              element.setAttribute(
                attribute.name,
                "#" + prefix + attribute.value.slice(1),
              );
            else if (
              ["aria-labelledby", "aria-describedby"].includes(attribute.name)
            )
              element.removeAttribute(attribute.name);
            else if (attribute.value.includes("url(#"))
              element.setAttribute(
                attribute.name,
                attribute.value.replace(/url\(#/g, "url(#" + prefix),
              );
          }
        }
        svg.setAttribute("aria-hidden", "true");
        state.svg = svg;
        thrust(state);
        holder.append(svg);
        const synchronize = () => {
          if (states.get(holder) !== state || !svg.isConnected) return;
          // New previews join the page's shared clock instead of restarting a turn.
          svg.setCurrentTime(performance.now() / 1000);
          if (matchMedia("(prefers-reduced-motion: reduce)").matches)
            svg.pauseAnimations();
        };
        synchronize();
        requestAnimationFrame(synchronize);
      })
      .catch(() => {
        if (states.get(holder) === state) {
          state.failed = true;
          holder.textContent = I18n.t("Голограмма недоступна");
        }
      });
  };
  UI.hologram = (source, label, className = "") => {
    const holder = UI.node("div", undefined, "hologram-surface " + className);
    UI.updateHologram(holder, source, label);
    return holder;
  };
}
