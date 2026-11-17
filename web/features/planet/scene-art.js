import { assetURL } from "../../core/assets.js";
// Public images only. Decoded bitmaps are leased by renderers; idle entries are
// evicted in LRU order. A closed renderer never retains a decoding request.
import { ART_PATHS as paths } from "./art-pack.js";
const entries = new Map(),
  MAX_BYTES = 24 * 1024 * 1024,
  MAX_IDLE = 20;
let counter = 0;
function trim() {
  let bytes = 0;
  for (const e of entries.values()) bytes += e.bytes;
  const idle = [...entries]
    .filter(([, e]) => !e.refs)
    .sort((a, b) => a[1].used - b[1].used);
  for (const [key, e] of idle) {
    if (bytes <= MAX_BYTES && entries.size <= MAX_IDLE) break;
    entries.delete(key);
    e.abort.abort();
    e.bitmap?.close();
    bytes -= e.bytes;
  }
}
function entry(key) {
  let e = entries.get(key);
  if (e) {
    e.used = ++counter;
    return e;
  }
  e = {
    refs: 0,
    used: ++counter,
    bytes: 0,
    bitmap: null,
    abort: new AbortController(),
  };
  entries.set(key, e);
  e.promise = (async () => {
    const response = await fetch(assetURL(paths[key]), {
      signal: AbortSignal.any([e.abort.signal, AbortSignal.timeout(8000)]),
      cache: "force-cache",
    });
    if (!response.ok) throw Error("image unavailable");
    const bitmap = await createImageBitmap(await response.blob());
    if (e.abort.signal.aborted) {
      bitmap.close();
      throw new DOMException("Aborted", "AbortError");
    }
    if (bitmap.width > 1024 || bitmap.height > 1024) {
      bitmap.close();
      throw Error("image dimensions");
    }
    e.bitmap = bitmap;
    e.bytes = bitmap.width * bitmap.height * 4;
    trim();
    return bitmap;
  })().catch((error) => {
    if (entries.get(key) === e) entries.delete(key);
    throw error;
  });
  return e;
}
export async function acquireArt(keys, signal) {
  const held = new Map();
  let released = false;
  const release = () => {
    if (released) return;
    released = true;
    signal?.removeEventListener("abort", release);
    for (const [key, e] of held) {
      e.refs--;
      e.used = ++counter;
      if (!e.refs && !e.bitmap) {
        e.abort.abort();
        if (entries.get(key) === e) entries.delete(key);
      }
    }
    trim();
  };
  if (signal?.aborted) return { get: () => null, missing: [], release };
  for (const key of new Set(keys))
    if (paths[key]) {
      const e = entry(key);
      e.refs++;
      held.set(key, e);
    }
  signal?.addEventListener("abort", release, { once: true });
  const missing = [];
  await Promise.all(
    [...held].map(async ([key, e]) => {
      try {
        await e.promise;
      } catch {
        missing.push(key);
      }
    }),
  );
  return {
    get: (key) => (released ? null : held.get(key)?.bitmap),
    missing,
    release,
  };
}
export function artCacheStats() {
  return {
    entries: entries.size,
    bytes: [...entries.values()].reduce((n, e) => n + e.bytes, 0),
    pinned: [...entries.values()].filter((e) => e.refs).length,
  };
}
export const spriteSpecs = {
  tree_old: [280, 0.5, 0.94],
  tree_young: [190, 0.5, 0.94],
  bush_sparse: [88, 0.5, 0.76],
  grass_tuft: [57, 0.5, 0.76],
  rock_flat: [98, 0.5, 0.85],
  rock_large_cracked: [168, 0.5, 0.87],
  grass_dry: [63, 0.5, 0.75],
  // Platform footprint is centred on its apron, not on empty PNG padding.
  landing: [320, 0.5, 0.76],
  landing_pad: [320, 0.5, 0.76],
  atlas: [96, 0.5, 115 / 128],
  move_target: [80, 0.5, 84 / 128],
  move_blocked: [80, 0.5, 84 / 128],
  selection_ring: [64, 0.5, 84 / 128],
  sector_boundary: [72, 80 / 128, 100 / 128],
  hover_highlight: [100, 0.5, 0.7],
  footstep_dust: [48, 0.5, 70 / 128],
};
export function drawSprite(ctx, art, name, x, y, alpha = 1) {
  const image = art.get(name),
    s = spriteSpecs[name];
  if (!image || !s) return false;
  ctx.save();
  ctx.globalAlpha *= alpha;
  ctx.imageSmoothingEnabled = false;
  const size = s[0] / 16;
  ctx.drawImage(image, x - size * s[1], y - size * (image.height / image.width) * s[2], size, size * image.height / image.width);
  ctx.restore();
  return true;
}
