import { Visuals } from "../runtime.js";
export function initVisuals() {
  Object.assign(Visuals, {
    object(ctx, x, y, r, site, scale = 1) {
      const kind = site.kind;
      if (kind === "Звезда" || kind === "Чёрная дыра") {
        ctx.save();
        ctx.fillStyle = kind === "Звезда" ? "#efbd68" : "#050710";
        ctx.shadowColor = kind === "Звезда" ? "#efad58" : "#a68bcf";
        ctx.shadowBlur = r * 0.35;
        ctx.beginPath();
        ctx.arc(x, y, r * 0.7, 0, Math.PI * 2);
        ctx.fill();
        ctx.strokeStyle = "#bba0e4";
        if (kind === "Чёрная дыра") {
          ctx.lineWidth = 5;
          ctx.beginPath();
          ctx.ellipse(x, y, r, r * 0.35, 0.3, 0, Math.PI * 2);
          ctx.stroke();
        }
        ctx.restore();
        return;
      }
      if (kind === "Орбитальная станция") {
        ctx.save();
        ctx.translate(x, y);
        ctx.rotate(0.4);
        ctx.strokeStyle = "#8ccfb8";
        ctx.lineWidth = Math.max(1, r * 0.12);
        ctx.strokeRect(-r, -r * 0.6, r * 2, r * 1.2);
        ctx.fillStyle = "#243f4d";
        ctx.fillRect(-r * 0.5, -r * 0.5, r, r);
        ctx.fillStyle = "#c5dadd";
        ctx.fillRect(-r * 1.4, -r * 0.1, r * 2.8, r * 0.2);
        ctx.restore();
        return;
      }
      if (kind === "Пояс астероидов" || kind === "Ледяное поле") {
        for (let i = 0; i < 5; i++) {
          const a = i * 2.4,
            px = x + Math.cos(a) * r * 0.65,
            py = y + Math.sin(a) * r * 0.6,
            size = r * (0.22 + i * 0.025);
          ctx.beginPath();
          for (let j = 0; j < 6; j++) {
            const theta = (j * Math.PI) / 3,
              rad = size * (j % 2 ? 0.75 : 1);
            const fx = px + Math.cos(theta) * rad,
              fy = py + Math.sin(theta) * rad;
            if (j === 0) ctx.moveTo(fx, fy);
            else ctx.lineTo(fx, fy);
          }
          ctx.closePath();
          ctx.fillStyle =
            kind === "Ледяное поле"
              ? ["#86cde8", "#c0ecfa"][i % 2]
              : ["#787c81", "#a29a8c", "#5d636c"][i % 3];
          ctx.fill();
          ctx.strokeStyle = kind === "Ледяное поле" ? "#d5f5ff" : "#c0b7a3";
          ctx.lineWidth = 1;
          ctx.stroke();
        }
        return;
      }
      const colors = {
        Ледяная: ["#b6edff", "#326bb1"],
        Каменистая: ["#b0b1b3", "#4b4f56"],
        "Садовый мир": ["#74ba86", "#225d7a"],
        Вулканическая: ["#ef9c4d", "#8a292b"],
        "Газовый гигант": ["#d1bb8b", "#72507e"],
      };
      const color = colors[kind] || ["#b8c4d0", "#485b70"];
      const gradient = ctx.createRadialGradient(
        x - r * 0.35,
        y - r * 0.4,
        r * 0.1,
        x,
        y,
        r,
      );
      gradient.addColorStop(0, color[0]);
      gradient.addColorStop(1, color[1]);
      ctx.fillStyle = gradient;
      ctx.beginPath();
      ctx.arc(x, y, r, 0, Math.PI * 2);
      ctx.fill();
      ctx.save();
      ctx.beginPath();
      ctx.arc(x, y, r, 0, Math.PI * 2);
      ctx.clip();
      if (kind === "Газовый гигант") {
        for (let i = -3; i <= 3; i++) {
          ctx.fillStyle = i % 2 ? "#d9bf9a66" : "#51406755";
          ctx.fillRect(x - r, y + i * r * 0.23, r * 2, r * 0.12);
        }
      } else if (kind === "Садовый мир") {
        ctx.fillStyle = "#8dbb6c";
        for (let i = 0; i < 6; i++) {
          ctx.beginPath();
          ctx.ellipse(
            x + Math.sin(i * 2) * r * 0.55,
            y + Math.cos(i * 1.7) * r * 0.6,
            r * 0.3,
            r * 0.14,
            i,
            0,
            Math.PI * 2,
          );
          ctx.fill();
        }
      } else if (kind === "Каменистая") {
        ctx.fillStyle = "#333b4455";
        for (let i = 0; i < 6; i++) {
          ctx.beginPath();
          ctx.arc(
            x + Math.sin(i * 2) * r * 0.6,
            y + Math.cos(i * 3) * r * 0.6,
            r * 0.12,
            0,
            Math.PI * 2,
          );
          ctx.fill();
        }
      }
      ctx.restore();
    },
    ship(ctx, x, y, size = 10, angle = 0, color = "#9de5f2") {
      ctx.save();
      ctx.translate(x, y);
      ctx.rotate(angle);
      ctx.fillStyle = color;
      ctx.strokeStyle = "#f4ffff";
      ctx.lineWidth = 1;
      ctx.shadowColor = color;
      ctx.shadowBlur = 9;
      ctx.beginPath();
      ctx.moveTo(size, 0);
      ctx.lineTo(-size * 0.65, -size * 0.6);
      ctx.lineTo(-size * 0.35, 0);
      ctx.lineTo(-size * 0.65, size * 0.6);
      ctx.closePath();
      ctx.fill();
      ctx.stroke();
      ctx.restore();
    },
  });
}
