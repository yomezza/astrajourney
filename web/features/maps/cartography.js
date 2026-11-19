// Presentation resources only. No world generation, requests or hit testing.
const spectral = { O: '#9abaff', B: '#c4d8ff', A: '#f4f6ff', F: '#fff1c7', G: '#ffd987', K: '#f4ab71', M: '#ee8f87' };
export function starColor(kind) { return spectral[kind] || '#f3c98a'; }

export function createCartography() {
  const backdrop = document.createElement('canvas');
  backdrop.width = 1024;
  backdrop.height = 768;
  const b = backdrop.getContext('2d');
  b.fillStyle = '#050b14'; b.fillRect(0, 0, 1024, 768);
  for (const [x, y, radius, color] of [[220, 380, 520, '#204c663c'], [760, 260, 420, '#41416d32'], [580, 680, 400, '#15606924']]) {
    const mist = b.createRadialGradient(x, y, 0, x, y, radius);
    mist.addColorStop(0, color); mist.addColorStop(1, '#050b1400');
    b.fillStyle = mist; b.fillRect(0, 0, 1024, 768);
  }
  // Fine photographic grain, deliberately much smaller than system markers.
  for (let i = 0; i < 500; i++) {
    b.fillStyle = i % 7 ? '#b6d7ed0b' : '#b6d7ed18';
    b.fillRect((i * 97.37) % 1024, (i * 173.71) % 768, 1, 1);
  }
  const sprites = new Map(), starLayer = document.createElement('canvas');
  let lastPoints = null, lastKnowledge = null, lastRadius = 0;
  function sprite(color) {
    if (sprites.has(color)) return sprites.get(color);
    const image = document.createElement('canvas'); image.width = image.height = 40;
    const c = image.getContext('2d'), glow = c.createRadialGradient(20, 20, 0, 20, 20, 20);
    glow.addColorStop(0, '#ffffff'); glow.addColorStop(.09, color);
    glow.addColorStop(.2, color + 'aa'); glow.addColorStop(1, color + '00');
    c.fillStyle = glow; c.fillRect(0, 0, 40, 40); sprites.set(color, image); return image;
  }
  return {
    background(ctx, w, h, camera, mode) {
      ctx.drawImage(backdrop, 0, 0, w, h);
      ctx.save();
      ctx.strokeStyle = '#7496b515'; ctx.lineWidth = 1;
      const spacing = 100;
      const ox = ((camera.x % spacing) + spacing) % spacing;
      const oy = ((camera.y % spacing) + spacing) % spacing;
      for (let x = ox; x < w; x += spacing) for (let y = oy; y < h; y += spacing) {
        ctx.beginPath(); ctx.moveTo(x - 3, y); ctx.lineTo(x + 3, y);
        ctx.moveTo(x, y - 3); ctx.lineTo(x, y + 3); ctx.stroke();
      }
      if (mode === 'galaxy') {
        ctx.strokeStyle = '#91b9d016';
        for (let r = 100; r < Math.max(w, h); r += 140) {
          ctx.beginPath(); ctx.arc(w / 2 + camera.x, h / 2 + camera.y, r * camera.zoom, 0, Math.PI * 2); ctx.stroke();
        }
      }
      ctx.restore();
    },
    star(ctx, x, y, radius, kind, known) {
      const size = radius * (known ? 8 : 5);
      ctx.drawImage(sprite(known ? starColor(kind) : '#9cbed3'), x - size / 2, y - size / 2, size, size);
      if (!known) {
        ctx.strokeStyle = '#9cbed360'; ctx.lineWidth = .7;
        ctx.strokeRect(x - radius, y - radius, radius * 2, radius * 2);
      }
    },
    stars(ctx, points, radius, knowledge) {
      if (lastPoints !== points || lastKnowledge !== knowledge || lastRadius !== radius || starLayer.width !== ctx.canvas.width || starLayer.height !== ctx.canvas.height) {
        starLayer.width = ctx.canvas.width; starLayer.height = ctx.canvas.height;
        const layer = starLayer.getContext('2d'); layer.setTransform(ctx.getTransform());
        for (const point of points) this.star(layer, point.x, point.y, radius, point.star.class, knowledge.has(point.star.id));
        lastPoints = points; lastKnowledge = knowledge; lastRadius = radius;
      }
      ctx.save(); ctx.setTransform(1, 0, 0, 1, 0, 0); ctx.drawImage(starLayer, 0, 0); ctx.restore();
    },
    clear() { starLayer.width = starLayer.height = 0; lastPoints = lastKnowledge = null; },
    sun(ctx, x, y, radius, kind) {
      const color = starColor(kind);
      ctx.drawImage(sprite(color), x - radius * 3, y - radius * 3, radius * 6, radius * 6);
      const skin = ctx.createRadialGradient(x - radius * .35, y - radius * .3, 0, x, y, radius);
      skin.addColorStop(0, '#fff9e5'); skin.addColorStop(.6, color); skin.addColorStop(1, '#be7040');
      ctx.fillStyle = skin; ctx.beginPath(); ctx.arc(x, y, radius, 0, Math.PI * 2); ctx.fill();
      ctx.strokeStyle = color + '44'; ctx.lineWidth = 1;
      for (const k of [1.18, 1.4]) { ctx.beginPath(); ctx.arc(x, y, radius * k, 0, Math.PI * 2); ctx.stroke(); }
    },
    object(ctx, x, y, radius, site, paint) {
      ctx.save();
      if (site.kind === 'Орбитальная станция') {
        ctx.strokeStyle = '#81dbc080'; ctx.fillStyle = '#0c2b30'; ctx.lineWidth = 1;
        ctx.beginPath();
        for (let i = 0; i <= 6; i++) {
          const a = i * Math.PI / 3; const px = x + Math.cos(a) * radius * 1.3, py = y + Math.sin(a) * radius * 1.3;
          if (i) ctx.lineTo(px, py); else ctx.moveTo(px, py);
        }
        ctx.fill(); ctx.stroke(); paint(ctx, x, y, radius * .7, site);
      } else {
        paint(ctx, x, y, radius, site);
        if (site.planet < 101) {
          // A terminator and atmosphere give the existing planet art volume.
          ctx.beginPath(); ctx.arc(x, y, radius, 0, Math.PI * 2); ctx.clip();
          const shade = ctx.createLinearGradient(x - radius, y - radius, x + radius, y + radius);
          shade.addColorStop(0, '#ffffff18'); shade.addColorStop(.45, '#00000000'); shade.addColorStop(1, '#020711e8');
          ctx.fillStyle = shade; ctx.fillRect(x - radius, y - radius, radius * 2, radius * 2);
        }
      }
      ctx.restore();
    },
    label(ctx, text, x, y, accent = '#b6cdda') {
      ctx.save(); ctx.font = '11px system-ui, sans-serif';
      const width = ctx.measureText(text).width + 18;
      ctx.fillStyle = '#091522e8'; ctx.fillRect(x, y - 17, width, 25);
      ctx.fillStyle = accent; ctx.fillRect(x, y - 17, 2, 25); ctx.fillText(text, x + 9, y);
      ctx.restore();
    },
    ship(ctx, x, y, angle, color, paint) {
      ctx.save(); ctx.translate(x, y); ctx.rotate(angle);
      ctx.fillStyle = '#8be4ef25'; ctx.beginPath();
      ctx.moveTo(-8, -5); ctx.lineTo(-32, 0); ctx.lineTo(-8, 5); ctx.fill();
      ctx.restore(); paint(ctx, x, y, 10, angle, color);
    },
    route(ctx, a, b) {
      ctx.save(); ctx.lineWidth = 7; ctx.strokeStyle = '#e8bc621c';
      ctx.beginPath(); ctx.moveTo(a.x, a.y); ctx.lineTo(b.x, b.y); ctx.stroke();
      ctx.lineWidth = 1.8; ctx.strokeStyle = '#f4cd80'; ctx.stroke();
      const x = (a.x + b.x) / 2, y = (a.y + b.y) / 2, angle = Math.atan2(b.y - a.y, b.x - a.x);
      if (Math.hypot(b.x - a.x, b.y - a.y) > 34) {
        ctx.translate(x, y); ctx.rotate(angle); ctx.beginPath();
        ctx.moveTo(-4, -4); ctx.lineTo(2, 0); ctx.lineTo(-4, 4); ctx.stroke();
      }
      ctx.restore();
    },
  };
}
