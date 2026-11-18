// Material masks change presentation only; water/height come from saved scene v1.
const N = 256,
  RES = 8;
const clamp = (v) => Math.max(0, Math.min(1, v));
const smooth = (a, b, v) => {
  const t = clamp((v - a) / (b - a));
  return t * t * (3 - 2 * t);
};
const material = (b) =>
  b === "Сухие равнины"
    ? "dry"
    : b === "Холодные равнины" || b === "Ледяная"
      ? "snow"
      : ["Вулканическая", "Лавовая"].includes(b)
        ? "volcanic"
        : b === "Пустынная"
          ? "sand"
          : b === "Каменистая"
            ? "rock"
            : "grass";
export function groundKeys(scene) {
  return [
    ...new Set([
      material(scene.biome),
      ...(scene.materials || []).map((p) => material(p.biome)),
      "dry",
      "sand",
      "rock",
    ]),
  ].map((n) => "ground_" + n);
}
function distance(cells, wet) {
  const d = new Uint8Array(N * N);
  d.fill(30);
  for (let i = 0; i < d.length; i++)
    if (cells[i] & 1 && !!(cells[i] & 2) === wet) d[i] = 0;
  for (let y = 0; y < N; y++)
    for (let x = 0; x < N; x++) {
      const i = y * N + x;
      if (x) d[i] = Math.min(d[i], d[i - 1] + 1);
      if (y) d[i] = Math.min(d[i], d[i - N] + 1);
    }
  for (let y = N - 1; y >= 0; y--)
    for (let x = N - 1; x >= 0; x--) {
      const i = y * N + x;
      if (x < N - 1) d[i] = Math.min(d[i], d[i + 1] + 1);
      if (y < N - 1) d[i] = Math.min(d[i], d[i + N] + 1);
    }
  return d;
}
export function makeGround(scene, art) {
  const canvas = document.createElement("canvas");
  canvas.width = canvas.height = N * RES;
  const ctx = canvas.getContext("2d"),
    cells = Uint8Array.from(atob(scene.cells), (c) => c.charCodeAt(0));
  const raw = scene.heights
    ? Uint8Array.from(atob(scene.heights), (c) => c.charCodeAt(0))
    : null;
  const heights = new Float32Array(N * N);
  for (let i = 0; i < heights.length; i++)
    heights[i] = raw ? raw[i * 2] + 256 * raw[i * 2 + 1] : 650;
  const wetDistance = distance(cells, true),
    landDistance = distance(cells, false);
  const origin = scene.origin || { x: 0, y: 0 };
  const names = [...new Set(groundKeys(scene).map((n) => n.slice(7)))];
  const masks = Object.fromEntries(
    [...names, "water_deep", "water_shallow", "shore_wet"].map((n) => [
      n,
      new Uint8ClampedArray(N * N * 4),
    ]),
  );
  const points = (
    scene.materials?.length
      ? scene.materials
      : [{ x: origin.x + 1, y: origin.y + 1, biome: scene.biome }]
  ).map((p) => ({ ...p, material: material(p.biome) }));
  const nearPoints = new Array(3),
    nearDistances = new Float64Array(3),
    nearCount = Math.min(3, points.length),
    weights = Object.fromEntries(names.map((name) => [name, 0]));
  for (let y = 0; y < N; y++)
    for (let x = 0; x < N; x++) {
      const i = y * N + x,
        j = i * 4;
      if (!(cells[i] & 1)) continue;
      if (cells[i] & 2) {
        masks.water_deep[j + 3] = 255;
        masks.water_shallow[j + 3] = 255 * (1 - smooth(1, 10, landDistance[i]));
        continue;
      }
      const wx = origin.x + (x + 0.5) / 128,
        wy = origin.y + (y + 0.5) / 128;
      // Only three points contribute. Keep their stable distance order without
      // allocating and sorting the full materials list for every ground cell.
      nearDistances.fill(Infinity);
      for (const p of points) {
        const d = (p.x - wx) ** 2 + (p.y - wy) ** 2;
        for (let k = 0; k < nearCount; k++) {
          if (d >= nearDistances[k]) continue;
          for (let n = nearCount - 1; n > k; n--) {
            nearDistances[n] = nearDistances[n - 1];
            nearPoints[n] = nearPoints[n - 1];
          }
          nearDistances[k] = d;
          nearPoints[k] = p;
          break;
        }
      }
      for (const name of names) weights[name] = 0;
      let sum = 0;
      for (let k = 0; k < nearCount; k++) {
        const p = nearPoints[k],
          d = nearDistances[k];
        const w = 1 / (d + 0.06) ** 2;
        weights[p.material] = (weights[p.material] || 0) + w;
        sum += w;
      }
      for (const name of names) weights[name] = (weights[name] || 0) / sum;
      const h = heights[i];
      // Smooth global mottling has no seed or new biome/river generation.
      const patch =
        (Math.sin(wx * 17 + Math.sin(wy * 9)) * Math.cos(wy * 13 - wx * 3) +
          1) /
        2;
      const dry = (weights.grass || 0) * smooth(0.45, 0.85, patch) * 0.28;
      weights.grass = Math.max(0, (weights.grass || 0) - dry);
      weights.dry = (weights.dry || 0) + dry;
      const rock = smooth(760, 980, h) * 0.7;
      for (const name of names) weights[name] *= 1 - rock;
      weights.rock = (weights.rock || 0) + rock;
      const sand = (1 - smooth(0, 3, wetDistance[i])) * 0.7;
      for (const name of names) weights[name] *= 1 - sand;
      weights.sand = (weights.sand || 0) + sand;
      // Ordered alpha composition preserves the intended normalized blend.
      let remaining = 1;
      for (let k = names.length - 1; k >= 0; k--) {
        const n = names[k],
          w = weights[n] || 0;
        masks[n][j + 3] = Math.round(
          255 * Math.min(1, w / Math.max(0.00001, remaining)),
        );
        remaining -= w;
      }
      masks.shore_wet[j + 3] = Math.round(
        100 * (1 - smooth(0, 2, wetDistance[i])),
      );
    }
  const mask = document.createElement("canvas");
  mask.width = mask.height = N;
  ctx.imageSmoothingEnabled = false;
  const mc = mask.getContext("2d"),
    layer = document.createElement("canvas");
  layer.width = layer.height = canvas.width;
  const lc = layer.getContext("2d");
 lc.imageSmoothingEnabled = false;
  const transform = new DOMMatrix()
    .translate(-origin.x * 128 * RES, -origin.y * 128 * RES)
    .scale(RES / 16);
  const hex = new Path2D();
  for (let k = 0; k < 6; k++) {
    const angle = ((k * 60 - 90) * Math.PI) / 180;
    const x = (128 + 128 * Math.cos(angle)) * RES,
      y = (128 + 128 * Math.sin(angle)) * RES;
    if (k) hex.lineTo(x, y);
    else hex.moveTo(x, y);
  }
  hex.closePath();
  ctx.save();
  ctx.clip(hex);
  for (const name of [...names, "shore_wet", "water_deep", "water_shallow"]) {
    const image = art.get(names.includes(name) ? "ground_" + name : name);
    if (!image) continue;
    const pixels = masks[name];
    for (let i = 0; i < pixels.length; i += 4) {
      pixels[i] = pixels[i + 1] = pixels[i + 2] = 255;
    }
    mc.putImageData(new ImageData(pixels, N, N), 0, 0);
    lc.clearRect(0, 0, layer.width, layer.height);
    lc.globalCompositeOperation = "source-over";
    const pattern = lc.createPattern(image, "repeat");
    pattern.setTransform(transform);
    lc.fillStyle = pattern;
    lc.fillRect(0, 0, layer.width, layer.height);
    lc.globalCompositeOperation = "destination-in";
    lc.drawImage(mask, 0, 0, layer.width, layer.height);
    ctx.drawImage(layer, 0, 0);
  }
  ctx.restore();
  layer.width = layer.height = 0;
  mask.width = mask.height = 0;
  // A fixed water mask clips a moving, edge-faded ripple overlay; no shoreline moves.
  const water = document.createElement("canvas");
  water.width = water.height = N;
  const wc = water.getContext("2d"),
    data = new Uint8ClampedArray(N * N * 4);
  for (let i = 0; i < cells.length; i++) {
    data[i * 4] = data[i * 4 + 1] = data[i * 4 + 2] = 255;
    data[i * 4 + 3] = (cells[i] & 3) === 3 ? 255 : 0;
  }
  wc.putImageData(new ImageData(data, N, N), 0, 0);
  const ripples = [0,1,2,3].map(i => art.get("ripple_"+i));
  const hasWater = cells.some((v) => (v & 3) === 3);
  const fx = document.createElement("canvas"),
    fc = fx.getContext("2d");
  return {
    cells,
    canvas,
    wetDistance,
    draw(ctx, view, now, animate) {
      ctx.drawImage(canvas, 0, 0, N, N);
      const ripple = ripples[Math.floor(now/180)%4];
      if (!ripple || !animate || !hasWater) return;
      const width = Math.min(1600, Math.ceil(view.width)),
        height = Math.min(1000, Math.ceil(view.height));
      if (fx.width !== width || fx.height !== height) {
        fx.width = width;
        fx.height = height;
      }
      const sx = width / view.worldW,
        sy = height / view.worldH;
      fc.imageSmoothingEnabled = false;
      fc.setTransform(sx, 0, 0, sy, -view.x * sx, -view.y * sy);
      fc.clearRect(view.x, view.y, view.worldW, view.worldH);
      fc.globalCompositeOperation = "source-over";
      const p = fc.createPattern(ripple, "repeat");
      p.setTransform(
        new DOMMatrix()
          .translate(
            -origin.x * 128,
            -origin.y * 128,
          )
          .scale(1 / 16),
      );
      fc.fillStyle = p;
      fc.fillRect(view.x, view.y, view.worldW, view.worldH);
      fc.globalCompositeOperation = "destination-in";
      fc.drawImage(water, 0, 0, N, N);
      ctx.save();
      ctx.globalAlpha = 0.35;
      ctx.drawImage(fx, view.x, view.y, view.worldW, view.worldH);
      ctx.restore();
    },
    stop() {
      for (const c of [canvas, water, fx]) c.width = c.height = 0;
    },
  };
}
