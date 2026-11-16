import { ART_PATHS } from "./art-pack.js";
import { AssetCache } from "../../core/asset-cache.js";
const textureCache = new AssetCache(6);
export function texture(name) {
  return textureCache.load(
    name,
    () =>
      new Promise((resolve, reject) => {
        const image = new Image();
        image.onload = () => {
          try {
            if (image.naturalWidth !== 32 || image.naturalHeight !== 32)
              throw Error("Invalid surface texture dimensions");
            const canvas = document.createElement("canvas");
            canvas.width = canvas.height = 32;
            const ctx = canvas.getContext("2d", { willReadFrequently: true });
            ctx.drawImage(image, 0, 0);
            resolve(ctx.getImageData(0, 0, 32, 32).data);
          } catch (error) {
            reject(error);
          }
        };
        image.onerror = () => reject(Error("Surface texture unavailable"));
        image.src = ART_PATHS["ground_" + name];
      }),
  );
}
export function materials(kind, g) {
  if (kind === "Садовый мир")
    return [
      "grass",
      "dry",
      "sand",
      "rock",
      ...(g.temperature < 0 ||
      g.sectors.some((s) => s.biome === "Холодные равнины")
        ? ["snow"]
        : []),
    ];
  if (kind === "Ледяная") return ["snow", "rock"];
  if (["Вулканическая", "Лавовая"].includes(kind)) return ["volcanic", "rock"];
  if (kind === "Пустынная") return ["sand", "dry", "rock"];
  return ["rock"];
}
const smooth = (from, to, value) => {
  const t = Math.max(0, Math.min(1, (value - from) / (to - from)));
  return t * t * (3 - 2 * t);
};
function biomeMasks(g) {
  const dry = new Float32Array(g.grid_w * g.grid_h),
    cold = new Float32Array(dry.length);
  // Blend the nearest saved sector biomes on the canonical world lattice.
  // This changes material presentation, never terrain, water or sector data.
  for (let row = 0; row < g.grid_h; row++)
    for (let col = 0; col < g.grid_w; col++) {
      const x = (col / (g.grid_w - 1)) * g.width,
        y = (row / (g.grid_h - 1)) * g.height;
      let a,
        b,
        c,
        da = Infinity,
        db = Infinity,
        dc = Infinity;
      for (const s of g.sectors) {
        const d = (s.center.x - x) ** 2 + (s.center.y - y) ** 2;
        if (d < da) {
          c = b;
          dc = db;
          b = a;
          db = da;
          a = s;
          da = d;
        } else if (d < db) {
          c = b;
          dc = db;
          b = s;
          db = d;
        } else if (d < dc) {
          c = s;
          dc = d;
        }
      }
      let total = 0,
        wd = 0,
        wc = 0;
      for (const [sector, distance] of [
        [a, da],
        [b, db],
        [c, dc],
      ]) {
        if (!sector) continue;
        const w = 1 / (distance + 0.12) ** 2;
        total += w;
        if (sector.biome === "Сухие равнины") wd += w;
        if (sector.biome === "Холодные равнины") wc += w;
      }
      const i = row * g.grid_w + col;
      dry[i] = wd / total;
      cold[i] = wc / total;
    }
  return { dry, cold };
}
function sampleMaterial(pixels, x, y, rgb, weight) {
  if (!pixels || weight <= 0) return 0;
  const ix = Math.floor(((x / 4 % 1 + 1) % 1) * 32),
    iy = Math.floor(((y / 4 % 1 + 1) % 1) * 32), at = (iy * 32 + ix) * 4;
  for(let c=0;c<3;c++) rgb[c] += weight * pixels[at+c];
  return weight;
}
export function terrain(g, kind, textures = null) {
  const canvas = document.createElement("canvas"),
    resolution = textures ? 8 : 4;
  canvas.width = (g.grid_w - 1) * resolution + 1;
  canvas.height = (g.grid_h - 1) * resolution + 1;
  const ctx = canvas.getContext("2d"),
    image = ctx.createImageData(canvas.width, canvas.height),
    sea = g.sea_level ?? (["Садовый мир", "Ледяная"].includes(kind) ? 460 : -1),
    masks = textures && kind === "Садовый мир" ? biomeMasks(g) : null,
    rgb = [0, 0, 0],
    mixed = [0, 0, 0];
  for (let y = 0; y < canvas.height; y++)
    for (let x = 0; x < canvas.width; x++) {
      const gx = x / resolution,
        gy = y / resolution,
        ix = Math.floor(gx),
        iy = Math.floor(gy),
        fx = gx - ix,
        fy = gy - iy,
        a = iy * g.grid_w + ix,
        b = iy * g.grid_w + Math.min(ix + 1, g.grid_w - 1),
        c = Math.min(iy + 1, g.grid_h - 1) * g.grid_w + ix,
        d =
          Math.min(iy + 1, g.grid_h - 1) * g.grid_w +
          Math.min(ix + 1, g.grid_w - 1),
        wa = (1 - fx) * (1 - fy),
        wb = fx * (1 - fy),
        wc = (1 - fx) * fy,
        wd = fx * fy,
        h =
          g.heights[a] * wa +
          g.heights[b] * wb +
          g.heights[c] * wc +
          g.heights[d] * wd,
        fresh =
          (g.water[a] >= 2 ? wa : 0) +
          (g.water[b] >= 2 ? wb : 0) +
          (g.water[c] >= 2 ? wc : 0) +
          (g.water[d] >= 2 ? wd : 0),
        water = h < sea ? 1 : fresh > 0.5 ? 2 : 0;
      if (water && kind === "Ледяная") {
        rgb[0] = 156;
        rgb[1] = 192;
        rgb[2] = 210;
      } else if (water === 1) {
        rgb[0] = 22;
        rgb[1] = 57 + Math.min(25, h / 20);
        rgb[2] = 78 + Math.min(30, h / 15);
      } else if (water === 2) {
        rgb[0] = 48;
        rgb[1] = 111;
        rgb[2] = 133;
      } else if (kind === "Ледяная") {
        rgb[0] = 190 + h / 30;
        rgb[1] = 208 + h / 40;
        rgb[2] = 220 + h / 50;
      } else if (h < 485 && sea > 0) {
        rgb[0] = 151;
        rgb[1] = 142;
        rgb[2] = 102;
      } else if (h > 700) {
        rgb[0] = 151;
        rgb[1] = 157;
        rgb[2] = 147;
      } else if (["Вулканическая", "Лавовая"].includes(kind)) {
        rgb[0] = 105 + h / 20;
        rgb[1] = 65;
        rgb[2] = 50;
      } else if (kind === "Пустынная") {
        rgb[0] = 176 + h / 40;
        rgb[1] = 145 + h / 50;
        rgb[2] = 98 + h / 60;
      } else if (kind === "Каменистая") {
        rgb[0] = 75 + h / 12;
        rgb[1] = 77 + h / 13;
        rgb[2] = 80 + h / 15;
      } else {
        rgb[0] = 61 + (h - 460) / 9;
        rgb[1] = 91 + (h - 460) / 10;
        rgb[2] = 65 + (h - 460) / 12;
      }
      const dx =
          (g.heights[b] - g.heights[a]) * (1 - fy) +
          (g.heights[d] - g.heights[c]) * fy,
        dy =
          (g.heights[c] - g.heights[a]) * (1 - fx) +
          (g.heights[d] - g.heights[b]) * fx,
        shade = water
          ? 1
          : Math.max(0.78, Math.min(1.16, 1 - (dx + dy) * 0.012));
      if (textures && !water) {
        const wx = (x / (canvas.width - 1)) * g.width,
          wy = (y / (canvas.height - 1)) * g.height,
          rock = smooth(625, 745, h),
          shore = sea >= 0 ? 1 - smooth(sea + 8, sea + 36, h) : 0;
        mixed[0] = mixed[1] = mixed[2] = 0;
        let weight = 0;
        const add = (name, w) => {
          weight += sampleMaterial(textures[name], wx, wy, mixed, w);
        };
        if (kind === "Садовый мир") {
          const dry =
              masks.dry[a] * wa +
              masks.dry[b] * wb +
              masks.dry[c] * wc +
              masks.dry[d] * wd,
            cold =
              masks.cold[a] * wa +
              masks.cold[b] * wb +
              masks.cold[c] * wc +
              masks.cold[d] * wd,
            base = (1 - shore) * (1 - rock);
          add("sand", shore);
          add("rock", (1 - shore) * rock);
          add("grass", base * Math.max(0, 1 - dry - cold));
          add("dry", base * dry);
          add("snow", base * cold);
        } else if (kind === "Ледяная") {
          add("snow", 1 - rock * 0.3);
          add("rock", rock * 0.3);
        } else if (["Вулканическая", "Лавовая"].includes(kind)) {
          add("volcanic", 1 - rock * 0.5);
          add("rock", rock * 0.5);
        } else if (kind === "Пустынная") {
          add("sand", (1 - rock) * 0.75);
          add("dry", (1 - rock) * 0.25);
          add("rock", rock);
        } else add("rock", 1);
        // Missing materials fall back locally; available layers are normalized
        // so a failed image never creates black holes or transparent land.
        if (weight > 0) for (let c = 0; c < 3; c++) rgb[c] = mixed[c] / weight;
      }
      const i = (y * canvas.width + x) * 4;
      for (let channel = 0; channel < 3; channel++)
        image.data[i + channel] = rgb[channel] * shade;
      image.data[i + 3] = 255;
    }
  ctx.putImageData(image, 0, 0);
  return canvas;
}
