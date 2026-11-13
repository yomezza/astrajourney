import { drawSprite, spriteSpecs } from "./scene-art.js";
import { I18n } from "../../core/i18n.js";
const titles = {
  tree_old: "Старое дерево",
  tree_young: "Молодое дерево",
  bush_sparse: "Кустарник",
  grass_tuft: "Трава",
  grass_dry: "Сухая трава",
  rock_flat: "Камень",
  rock_large_cracked: "Скальная глыба",
  landing: "Посадочный модуль",
};
export function createVisual() {
  return { phase: 0, last: null, dust: 0 };
}
function character(ctx, art, p, visual, latest, now, dust, reduced) {
  const before = visual.last,
    delta = before ? Math.hypot(p.x - before.x, p.y - before.y) : 0;
  visual.last = p;
  const moving = latest.moving && now - latest.received < 300;
  if (moving && delta < 2) {
    visual.phase += delta / 0.54;
    visual.dust += delta;
    if (!reduced && visual.dust > 0.8) {
      dust.push({ x: p.x, y: p.y, born: now });
      visual.dust = 0;
    }
  }
  const atlas = art.get("atlas");
  if (!atlas) {
    ctx.fillStyle = "#a9bdb4";
    ctx.beginPath();
    ctx.arc(p.x, p.y, 0.55, 0, Math.PI * 2);
    ctx.fill();
    return;
  }
  const facing = Number.isInteger(latest.facing) ? latest.facing : 0,
    col = moving ? 1 + (Math.floor(visual.phase) % 6) : 0;
  const s = spriteSpecs.atlas,
    size = s[0] / 16;
  ctx.drawImage(
    atlas,
    col * 128,
    facing * 128,
    128,
    128,
    p.x - size * s[1],
    p.y - size * s[2],
    size,
    size,
  );
  return { facing, col };
}
export function createSceneSprites(scene, art) {
  const objects = scene.objects.map((o) => ({
    name: o.asset.split("/").pop().replace(".png", ""),
    x: o.position.X / 1024,
    y: o.position.Y / 1024,
    id: o.id,
  }));
  if (scene.landing)
    objects.push({
      name: "landing",
      x: scene.landing.X / 1024,
      y: scene.landing.Y / 1024,
      id: "landing",
    });
  objects.sort((a, b) => a.y - b.y);
  const dust = [];
  let hover = null;
  return {
    hit(p) {
      hover = null;
      for (let i = objects.length - 1; i >= 0; i--) {
        const o = objects[i],
          s = spriteSpecs[o.name];
        if (!s) continue;
        // Select the ground contact, so an invisible PNG margin cannot intercept.
        if (
          Math.hypot(p.x - o.x, (p.y - o.y) * 1.7) <
          (o.name === "landing" ? 7 : 2.2)
        ) {
          hover = o;
          break;
        }
      }
      return hover
        ? {
            id: hover.id,
            x: hover.x,
            y: hover.y,
            name: I18n.t(titles[hover.name] || hover.name),
            text: I18n.t(
              hover.name === "landing"
                ? "Площадка высадки и возврата к кораблю"
                : "Природный объект · препятствия учитывает сервер",
            ),
          }
        : null;
    },
    draw(ctx, view, people, now, reduced) {
      const visible = (o) =>
        Math.abs(o.x - view.cx) < view.worldW / 2 + 25 &&
        Math.abs(o.y - view.cy) < view.worldH / 2 + 25;
      const own = people[0],
        render = objects.filter(visible);
      render.push(...people.filter(visible));
      render.sort((a, b) => a.y - b.y);
      for (const step of dust) {
        const age = (now - step.born) / 420;
        if (age >= 1) continue;
        drawSprite(ctx, art, "footstep_dust", step.x, step.y, 0.25 * (1 - age));
      }
      while (dust.length && (now - dust[0].born > 420 || dust.length > 96))
        dust.shift();
      let selfFrame;
      for (const o of render) {
        if (o.visual) {
          drawSprite(
            ctx,
            art,
            "selection_ring",
            o.x,
            o.y,
            o === own ? 0.7 : 0.35,
          );
          const frame = character(
            ctx,
            art,
            o,
            o.visual,
            o.latest,
            now,
            dust,
            reduced || view.zoom < 8,
          );
          if (o === own) selfFrame = frame;
          if (o.nickname) {
            ctx.save();
            ctx.font = `${11 / view.zoom}px sans-serif`;
            ctx.textAlign = "center";
            ctx.fillStyle = o.colour;
            ctx.strokeStyle = "#121b1c";
            ctx.lineWidth = 3 / view.zoom;
            ctx.strokeText(o.nickname, o.x, o.y - 5, 140 / view.zoom);
            ctx.fillText(o.nickname, o.x, o.y - 5, 140 / view.zoom);
            ctx.restore();
          }
        } else {
          const s = spriteSpecs[o.name],
            size = (s?.[0] || 0) / 16;
          const occludes =
            o.name.startsWith("tree_") &&
            own.y < o.y &&
            own.y > o.y - size * 0.7 &&
            Math.abs(own.x - o.x) < size * 0.27;
          drawSprite(ctx, art, o.name, o.x, o.y, occludes ? 0.43 : 1);
          if (o === hover) {
            ctx.save();
            ctx.strokeStyle = "#d8c797";
            ctx.lineWidth = 1 / view.zoom;
            ctx.beginPath();
            ctx.ellipse(
              o.x,
              o.y,
              o.name === "landing" ? 7 : 1.8,
              o.name === "landing" ? 4 : 1,
              0,
              0,
              Math.PI * 2,
            );
            ctx.stroke();
            ctx.restore();
          }
        }
      }
      return selfFrame;
    },
    stop() {
      dust.length = 0;
      hover = null;
    },
  };
}
