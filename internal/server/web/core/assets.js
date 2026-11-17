import { ASSET_URLS } from "./generated-assets.js";
let current = null;

// Public metadata only, once per page. A missing CDN leaves the compiled map
// available; no session state or private media enters the public cache.
export async function loadAssetMap() {
  try {
    const response = await fetch("/cdn/api/v1/asset-map", { signal: AbortSignal.timeout(2000), credentials: "omit" });
    if (!response.ok) return false;
    const data = await response.json();
    const valid = (value) => typeof value === "string" && /^\/assets\/sha256\/[a-f0-9]{64}\/[a-zA-Z0-9_./-]+$/.test(value);
    const required = Object.keys(ASSET_URLS).filter(key => key.startsWith("/") && !key.includes("?"));
    if (data.schema_version !== 1 || data.client_contract !== "astra-assets-v1" || !required.every(key => valid(data.urls?.[key]))) return false;
    current = Object.freeze({ ...data.urls });
    for (const node of document.querySelectorAll("[data-asset]")) {
      const suffix = node.localName === "use" ? "#icon" : "";
      node.setAttribute("href", assetURL(node.dataset.asset) + suffix);
    }
    return true;
  } catch { return false; }
}

// One immutable address per file. Unchanged images keep the same browser cache
// entry across game releases. User media and arbitrary external URLs are excluded.
export function assetURL(path) {
  const value = String(path || "");
  if (value.startsWith("/assets/sha256/")) return value;
  const logical = (value.startsWith("/") ? value : "/" + value).split("?")[0];
  if (current?.[logical]) return current[logical];
  return ASSET_URLS[value] || ASSET_URLS["/" + value] || value;
}
