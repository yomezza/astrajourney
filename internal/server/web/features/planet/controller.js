import { I18n } from "../../core/i18n.js";
import { acquireArt, drawSprite } from "./scene-art.js";
import { makeGround, groundKeys } from "./scene-ground.js";
import { createVisual, createSceneSprites } from "./scene-actors.js";
// This controller only interpolates server positions. Camera scale never changes
// physical speed, and pointer input sends a selected cell, never a new position.
export async function createController(
  canvas,
  scene,
  w,
  { current, target, signal, assets, onHover, onInteract },
) {
  const peers = new Map();
  const interpolate = (previous, latest, now) => {
    const expected = latest.time + Math.max(0, now - latest.received) - 100;
    const span = latest.time - previous.time;
    const t =
      span > 0
        ? Math.max(0, Math.min(1, (expected - previous.time) / span))
        : 1;
    return {
      x: previous.x + (latest.x - previous.x) * t,
      y: previous.y + (latest.y - previous.y) * t,
    };
  };
  const pose = (a) => ({
    x: a.x_q / 1024,
    y: a.y_q / 1024,
    time: a.server_time,
    received: performance.now(),
    facing: a.facing || 0,
    moving: !!a.moving,
  });
  const colour = (id) => {
    let hash = 0;
    for (const c of id) hash = (hash * 31 + c.charCodeAt(0)) >>> 0;
    return `hsl(${hash % 360} 65% 75%)`;
  };
  const art = await acquireArt(
    [
      ...new Set(
        scene.objects.map((o) => o.asset.split("/").pop().replace(".png", "")),
      ),
      ...groundKeys(scene),
      "atlas",
      ...(scene.landing ? ["landing"] : []),
      "water_deep",
      "water_shallow",
      "shore_wet",
      "water_ripple_overlay",
      "move_target",
      "move_blocked",
      "selection_ring",
      "sector_boundary",
      "footstep_dust",
    ],
    signal,
  );
  if (!current()) {
    art.release();
    return null;
  }
  assets?.(art.missing);
  const ground = makeGround(scene, art),
    sprites = createSceneSprites(scene, art),
    selfVisual = createVisual();
  const ctx = canvas.getContext("2d");
  const reduced = matchMedia("(prefers-reduced-motion: reduce)");
  let zoom = 16,
    width = 1,
    height = 1,
    ratio = 1,
    stopped = false,
    raf,
    follow = true,
    drag,
    route = [],
    goal = null,
    goalSeq = 0,
    goalBlocked = false,
    goalTime = 0;
  let latest = {
      x: w.x_q / 1024,
      y: w.y_q / 1024,
      time: w.server_time || Date.now(),
      received: performance.now(),
      moving: false,
      facing: 0,
    },
    previous = latest,
    cam = { x: latest.x, y: latest.y },
    offset = { x: 0, y: 0 };
  const resize = () => {
    const r = canvas.getBoundingClientRect();
    width = Math.max(1, r.width);
    height = Math.max(1, r.height);
    ratio = Math.min(2, devicePixelRatio || 1);
    canvas.width = Math.round(width * ratio);
    canvas.height = Math.round(height * ratio);
  };
  const observer = new ResizeObserver(resize);
  observer.observe(canvas);
  resize();
  const world = (e) => {
    const r = canvas.getBoundingClientRect();
    return {
      x: cam.x + (e.clientX - r.left - width / 2) / zoom,
      y: cam.y + (e.clientY - r.top - height / 2) / zoom,
    };
  };
  function setZoom(value, anchor) {
    const next = Math.max(0.25, Math.min(32, value));
    if (anchor) {
      const before = world(anchor),
        r = canvas.getBoundingClientRect();
      cam = {
        x: before.x - (anchor.clientX - r.left - width / 2) / next,
        y: before.y - (anchor.clientY - r.top - height / 2) / next,
      };
      offset = { x: cam.x - latest.x, y: cam.y - latest.y };
    }
    zoom = next;
  }
  const wheel = (e) => {
    e.preventDefault();
    setZoom(zoom * Math.exp(-e.deltaY * 0.0015), e);
  };
  const down = (e) => {
    if (e.button === 0) {
      const p = world(e);
      if (onInteract?.(sprites.hit(p), e)) {
        e.preventDefault();
        e.stopPropagation();
        return;
      }
      const edge = (scene.exits || []).find(
        (e) =>
          Math.hypot(p.x - e.x_q / 1024, p.y - e.y_q / 1024) <
          Math.max(2, 12 / zoom),
      );
      goal = edge
        ? { x: edge.x_q / 1024, y: edge.y_q / 1024 }
        : { x: Math.floor(p.x) + 0.5, y: Math.floor(p.y) + 0.5 };
      goalSeq = target(
        Math.floor(goal.x) * 1024 + 512,
        Math.floor(goal.y) * 1024 + 512,
      );
      goalTime = performance.now();
      goalBlocked = false;
    } else if (e.button === 2 || e.button === 1) {
      e.preventDefault();
      drag = { x: e.clientX, y: e.clientY, cx: cam.x, cy: cam.y };
      follow = false;
      canvas.setPointerCapture(e.pointerId);
    }
  };
  const move = (e) => {
    if (!drag) onHover?.(sprites.hit(world(e)));
    if (drag)
      cam = {
        x: drag.cx - (e.clientX - drag.x) / zoom,
        y: drag.cy - (e.clientY - drag.y) / zoom,
      };
  };
  const up = () => {
    drag = null;
  };
  const context = (e) => e.preventDefault();
  const out = () => onHover?.(null);
  canvas.addEventListener("wheel", wheel, { passive: false });
  canvas.addEventListener("pointerdown", down);
  canvas.addEventListener("pointermove", move);
  canvas.addEventListener("pointerup", up);
  canvas.addEventListener("pointercancel", up);
  canvas.addEventListener("contextmenu", context);
  canvas.addEventListener("pointerleave", out);
  canvas.dataset.label =
    "Поверхность сектора: ЛКМ — идти, колесо — масштаб, ПКМ — обзор";
  canvas.setAttribute(
    "aria-label",
    I18n.t("Поверхность сектора: ЛКМ — идти, колесо — масштаб, ПКМ — обзор"),
  );
  function draw(now) {
    if (stopped || !current()) return;
    if (document.hidden) {
      raf = requestAnimationFrame(draw);
      return;
    }
    const player = interpolate(previous, latest, now);
    if (follow && !drag) {
      cam.x += (player.x + offset.x - cam.x) * 0.2;
      cam.y += (player.y + offset.y - cam.y) * 0.2;
    }
    ctx.setTransform(ratio, 0, 0, ratio, 0, 0);
    ctx.clearRect(0, 0, width, height);
    ctx.translate(width / 2 - cam.x * zoom, height / 2 - cam.y * zoom);
    ctx.scale(zoom, zoom);
    const view = {
      cx: cam.x,
      cy: cam.y,
      x: cam.x - width / zoom / 2,
      y: cam.y - height / zoom / 2,
      worldW: width / zoom,
      worldH: height / zoom,
      width,
      height,
      zoom,
    };
    ground.draw(ctx, view, now, !reduced.matches && zoom >= 4);
    if (route.length) {
      ctx.strokeStyle = "#d9b970";
      ctx.lineWidth = 1.2 / zoom;
      ctx.setLineDash([4 / zoom, 4 / zoom]);
      ctx.beginPath();
      ctx.moveTo(player.x, player.y);
      for (const p of route) ctx.lineTo(p.x, p.y);
      ctx.stroke();
      ctx.setLineDash([]);
    }
    const people = [{ ...player, visual: selfVisual, latest }];
    for (const [id, p] of peers)
      people.push({
        ...interpolate(p.previous, p.latest, now),
        visual: p.visual,
        latest: p.latest,
        nickname: p.nickname,
        colour: colour(id),
      });
    const frame = sprites.draw(ctx, view, people, now, reduced.matches);
    for (const edge of scene.exits || []) {
      const x = edge.x_q / 1024,
        y = edge.y_q / 1024;
      if (!drawSprite(ctx, art, "sector_boundary", x, y, 0.8)) {
        ctx.strokeStyle = "#d6bd85";
        ctx.lineWidth = 1 / zoom;
        ctx.strokeRect(x - 1, y - 1, 2, 2);
      }
      ctx.fillStyle = "#e5c47f";
      ctx.font = `${11 / zoom}px sans-serif`;
      ctx.textAlign = "center";
      ctx.fillText(I18n.t("Сектор") + " " + edge.neighbor, x, y - 3);
    }
    if (goal) {
      const age = now - goalTime;
      if (
        (goalBlocked && age > 1400) ||
        (!latest.moving &&
          age > 1200 &&
          Math.hypot(player.x - goal.x, player.y - goal.y) < 0.5)
      )
        goal = null;
      else {
        if (
          !drawSprite(
            ctx,
            art,
            goalBlocked ? "move_blocked" : "move_target",
            goal.x,
            goal.y,
            reduced.matches ? 1 : 0.8 + Math.sin(now * 0.005) * 0.15,
          )
        ) {
          ctx.strokeStyle = goalBlocked ? "#db9490" : "#dfc892";
          ctx.lineWidth = 1.3 / zoom;
          ctx.beginPath();
          ctx.arc(goal.x, goal.y, 0.7, 0, Math.PI * 2);
          ctx.stroke();
        }
      }
    }
    canvas.dataset.animation = String(frame?.col || 0);
    canvas.dataset.facing = String(frame?.facing || 0);
    canvas.dataset.art = art.missing.length ? "partial" : "ready";
    canvas.dataset.ground = "textured";
    canvas.dataset.landing = String(!!scene.landing);
    canvas.dataset.ready = "true";
    canvas.dataset.xq = String(Math.round(player.x * 1024));
    canvas.dataset.yq = String(Math.round(player.y * 1024));
    canvas.dataset.zoom = String(zoom);
    ctx.setTransform(1, 0, 0, 1, 0, 0);
    raf = requestAnimationFrame(draw);
  }
  raf = requestAnimationFrame(draw);
  function syncPeers() {
    canvas.dataset.actors = String(peers.size + 1);
    canvas.dataset.peers = JSON.stringify(
      [...peers].map(([id, p]) => ({
        actor: id,
        x_q: Math.round(p.latest.x * 1024),
        y_q: Math.round(p.latest.y * 1024),
      })),
    );
  }
  return {
    actors(list, replace = false) {
      if (replace) peers.clear();
      for (const a of list) {
        if (a.actor === w.actor) continue;
        const latest = pose(a),
          old = peers.get(a.actor);
        if (old && latest.time < old.latest.time) continue;
        peers.set(a.actor, {
          nickname: a.nickname,
          previous: old?.latest || latest,
          visual: old?.visual || createVisual(),
          latest,
        });
      }
      syncPeers();
    },
    remove(id) {
      peers.delete(id);
      syncPeers();
    },
    update(state) {
      canvas.dataset.moving = String(state.moving);
      if (
        state.server_time < latest.time &&
        latest.received !== previous.received
      )
        return;
      previous = latest;
      latest = {
        x: state.x_q / 1024,
        y: state.y_q / 1024,
        time: state.server_time,
        received: performance.now(),
        moving: !!state.moving,
        facing: state.facing || 0,
      };
      if (Number.isInteger(state.remaining))
        route = state.remaining ? route.slice(-state.remaining) : [];
    },
    error(code, seq) {
      if (
        goal &&
        (!seq || seq === goalSeq) &&
        [
          "target_blocked",
          "target_unreachable",
          "path_budget_exceeded",
          "invalid_target",
        ].includes(code)
      ) {
        goalBlocked = true;
        goalTime = performance.now();
        route = [];
      }
    },
    accepted(state) {
      route = state.path || [];
      if (route.length) {
        goal = route.at(-1);
        goalBlocked = false;
        goalTime = performance.now();
      }
    },
    zoom(factor) {
      setZoom(zoom * factor);
    },
    centre() {
      follow = true;
      offset = { x: 0, y: 0 };
      cam = { x: latest.x, y: latest.y };
    },
    fit() {
      follow = false;
      zoom = Math.max(0.25, Math.min(width, height) / 272);
      cam = { x: 128, y: 128 };
    },
    stop() {
      if (stopped) return;
      stopped = true;
      cancelAnimationFrame(raf);
      observer.disconnect();
      canvas.removeEventListener("wheel", wheel);
      canvas.removeEventListener("pointerdown", down);
      canvas.removeEventListener("pointermove", move);
      canvas.removeEventListener("pointerup", up);
      canvas.removeEventListener("pointercancel", up);
      canvas.removeEventListener("contextmenu", context);
      canvas.removeEventListener("pointerleave", out);
      peers.clear();
      sprites.stop();
      ground.stop();
      art.release();
      onHover?.(null);
    },
  };
}
