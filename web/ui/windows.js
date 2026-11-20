import { moduleLoading } from "./module-loading.js";
import { Session } from "../core/session.js";
import { Lifetime } from "../core/lifetime.js";
import { Preferences } from "../core/preferences.js";
import { I18n } from "../core/i18n.js";
import { Modules } from "./runtime.js";
import { node as createNode } from "./dom.js";
import { lineIcon } from "./icons.js";

export function initWindows() {
  const windows = new Map(),
    links = new Map();
  let z = 40,
    resetting = false,
    activeWindow;
  const switcherHost = document.getElementById("window-switcher");
  const tr = (s) => I18n.t(s),
    read = (key, fallback) => Preferences.read(key, fallback);

  const node = createNode;
  const narrow = w => w.matrix && matchMedia("(max-width: 700px)").matches;
  function active(w) {
    if (activeWindow === w) return;
    activeWindow?.dialog.classList.remove("window-active");
    activeWindow = w;
    w.dialog.classList.add("window-active");
    if (z >= 8000) {
      const ordered = [...windows.values()].sort(
        (a, b) => Number(a.dialog.style.zIndex) - Number(b.dialog.style.zIndex),
      );
      z = 40;
      for (const entry of ordered) entry.dialog.style.zIndex = ++z;
    }
    w.dialog.style.zIndex = ++z;
    switcher();
  }
  function switcher() {
    if (!switcherHost) return;
    for (const [id, w] of windows) {
      if (!w.dialog.open) {
        w.switchButton?.remove();
        w.switchButton = null;
        continue;
      }
      if (!w.switchButton) {
        const button = node("button");
        button.type = "button";
        button.onclick = () => Modules.focus(id);
        w.switchButton = button;
        switcherHost.append(button);
      }
      const button = w.switchButton;
      if (button.textContent !== w.title) {
        button.textContent = w.title;
        button.title = w.title;
      }
      button.classList.toggle("active", activeWindow === w);
      button.setAttribute("aria-pressed", String(activeWindow === w));
    }
  }
  function closeDialog(w) {
    const restoreFocus = w.dialog.contains(document.activeElement);
    w.dialog.close();
    if (activeWindow === w) {
      w.dialog.classList.remove("window-active");
      activeWindow = null;
      let next;
      for (const candidate of windows.values())
        if (candidate.dialog.open && !candidate.dialog.inert &&
            (!next || Number(candidate.dialog.style.zIndex) > Number(next.dialog.style.zIndex)))
          next = candidate;
      if (next) active(next);
    }
    switcher();
    if (restoreFocus && activeWindow && !activeWindow.dialog.contains(document.activeElement))
      activeWindow.bar.focus({preventScroll:true});
  }
  const docked = w => w.dialog.classList.contains("matrix-comms-docked");
  function save(id, w) {
    if (resetting || !w.dialog.open || w.maximized || narrow(w) || docked(w)) return;
    const r = w.dialog.getBoundingClientRect();
    if (w.layout) {
      if (!w.minimized && innerWidth > 550) w.layout.save({
        x: Math.round(r.x), y: Math.round(r.y),
        w: Math.round(r.width), h: Math.round(r.height), pinned: w.pinned,
      });
      return;
    }
    Preferences.write("module:" + id, {
      x: r.x, y: r.y, w: r.width, h: r.height, pinned: w.pinned,
    });
  }
  function clamp(w) {
    if (narrow(w) || docked(w)) return;
    const d = w.dialog,
      r = d.getBoundingClientRect();
    const reserve = w.layout && innerWidth >= 1120 ?
      Number.parseFloat(getComputedStyle(document.body).getPropertyValue("--matrix-comms-reserve")) || 0 : 0;
    d.style.left = Math.max(4, Math.min(r.x, innerWidth - reserve - r.width - 4)) + "px";
    d.style.top =
      Math.max(62, Math.min(r.y, innerHeight - r.height - 36)) + "px";
  }
  function linkedPeers(id) {
    const seen = new Set([id]),
      result = [],
      queue = [id];
    for (let i = 0; i < queue.length; i++) {
      for (const peerID of links.get(queue[i]) || []) {
        if (seen.has(peerID)) continue;
        seen.add(peerID);
        const peer = windows.get(peerID);
        if (!peer?.dialog.open || peer.pinned || peer.maximized) continue;
        queue.push(peerID);
        result.push([peerID, peer]);
      }
    }
    return result;
  }
  Object.assign(Modules, {
    header(id) {
      return windows.get(id)?.bar;
    },
    create(id, title, width = 820, height = 600, options = {}) {
      if (windows.has(id)) {
        const existing = windows.get(id);
        existing.h.dataset.i18n = title;
        existing.h.textContent = tr(title);
        existing.title = tr(title);
        switcher();
        if (options.resizeOnReuse && !existing.pinned && !existing.maximized) {
          existing.dialog.style.width = Math.min(width, innerWidth - 12) + "px";
          existing.dialog.style.height =
            Math.min(height, innerHeight - 106) + "px";
          clamp(existing);
        }
        return existing.content;
      }
      const d = node("dialog", undefined, "module-window"),
        bar = node("div", undefined, "module-titlebar"),
        h = node("h2", tr(title)),
        actions = node("div", undefined, "window-actions"),
        pin = node("button", "⌖"),
        max = node("button", "□"),
        close = node("button", "×"),
        content = node("div", undefined, "module-content"),
        small = node(
          "p",
          tr("Увеличьте окно, чтобы увидеть содержимое"),
          "window-small",
        );
      d.id = "window-" + id;
      const matrix = id !== "chat" && id !== "notifications" && !id.startsWith("chat-");
      d.classList.toggle("matrix-window",matrix);
      if (matrix) {
        pin.replaceChildren(lineIcon("pin"));
        max.replaceChildren(lineIcon("maximize"));
        close.replaceChildren(lineIcon("close"));
      }
      h.id = "title-" + id;
      h.dataset.i18n = title;
      d.setAttribute("aria-labelledby", h.id);
      bar.tabIndex = 0;
      for (const [b, label] of [
        [pin, "Закрепить окно"],
        [max, "Развернуть окно"],
        [close, "Закрыть"],
      ]) {
        b.type = "button";
        if (!matrix || b !== close) b.dataset.help = label;
        b.dataset.label = label;
        b.setAttribute("aria-label", tr(label));
      }
      pin.setAttribute("aria-pressed", "false");
      if (options.layout) {
        const minimize = node("button", "−");
        if (matrix) minimize.replaceChildren(lineIcon("minimize"));
        minimize.type = "button";
        minimize.dataset.label = "Свернуть окно";
        minimize.setAttribute("aria-label", tr(minimize.dataset.label));
        minimize.onclick = () => w.minimize(!w.minimized);
        actions.append(minimize);
      }
      actions.append(pin, max, close);
      bar.append(h, actions);
      d.append(bar, content, small);
      document.body.append(d);
      const w = {
        life: new Lifetime(),
        layout: options.layout,
        matrix,
        minimize(value, persist = true) {
          w.minimized = value;
          d.classList.toggle("studio-minimized", value);
          if (persist) w.layout.minimize(value);
        },
        transient: !!options.transient,
        private: options.private !== false,
        audience: options.audience,
        dialog: d,
        content,
        bar,
        h,
        title: tr(title),
        defaultTitle: title,
        pinned: false,
        maximized: false,
        initialized: false,
        minContentWidth: options.minContentWidth ?? 350,
        width,
        height,
      };
      w.loading = moduleLoading(d, content);
      w.life.own(() => w.loading.dispose());
      windows.set(id, w);
      const show = (prepare) => {
        if (w.audience === "admin" && Session.data.state?.user.role !== "admin") return;
        if (!d.open) {
          d.show();
          if (!w.initialized) {
            const l = w.layout ? w.layout.read() : read("module:" + id, {});
            if (
              id === "ship" &&
              innerWidth >= 1000 &&
              !read("layout:ship-inline-v1", false)
            ) {
              if (l.w && l.w < 700 && !l.pinned)
                l.w = Math.min(width, innerWidth - 220);
              try {
                Preferences.write("layout:ship-inline-v1", true);
              } catch {}
            }
            const sidebar = options.avoidSidebar && l.x == null && l.w == null
              ? document.querySelector("#sidebar:not([hidden])")
              : null;
            const inset = sidebar ? Math.ceil(sidebar.getBoundingClientRect().right) + 6 : 6;
            const fitWidth = narrow(w) ? l.w || width : Math.min(l.w || width, innerWidth - inset - 6);
            const fitHeight = narrow(w) ? l.h || height : Math.min(l.h || height, innerHeight - 106);
            d.style.width = fitWidth + "px";
            d.style.height = fitHeight + "px";
            d.style.left =
              (l.x ?? Math.max(inset, (innerWidth - width) / 2)) + "px";
            d.style.top =
              (l.y ?? Math.max(64, (innerHeight - height) / 2)) + "px";
            w.pinned = !!l.pinned;
            d.classList.toggle("pinned", w.pinned);
            pin.setAttribute("aria-pressed", String(w.pinned));
            if (w.layout) w.minimize(!!l.minimized, false);
            w.initialized = true;
          }
          clamp(w);
        }
        active(w);
        bar.focus();
        document
          .querySelectorAll(`:is(#sidebar,#matrix-mobile) [data-open="${id}"]`)
          .forEach((b) => b.classList.add("module-open"));
        return w.loading.run(async (signal) => {
          if (prepare) return prepare(signal);
          window.dispatchEvent(new CustomEvent("moduleopen", { detail: id }));
          return w.prepare?.(signal);
        });
      };
      const hide = () => {
        if (!d.open) return;
        save(id, w);
        closeDialog(w);
        w.loading.cancel();
        document
          .querySelectorAll(`:is(#sidebar,#matrix-mobile) [data-open="${id}"]`)
          .forEach((b) => b.classList.remove("module-open"));
        window.dispatchEvent(new CustomEvent("moduleclose", { detail: id }));
        if (
          w.transient &&
          !w.pinned &&
          !w.content.querySelector('[data-dirty="true"]')
        )
          Modules.destroy(id);
      };
      w.show = show;
      w.hide = hide;
      const maximize = () => {
        if (w.pinned || narrow(w) || docked(w)) return;
        if (!w.maximized) {
          const r = d.getBoundingClientRect();
          w.restore = { x: r.x, y: r.y, w: r.width, h: r.height };
          d.style.left = "5px";
          d.style.top = "62px";
          d.style.width = innerWidth - 10 + "px";
          d.style.height = innerHeight - 101 + "px";
        } else {
          const r = w.restore;
          d.style.left = r.x + "px";
          d.style.top = r.y + "px";
          d.style.width = r.w + "px";
          d.style.height = r.h + "px";
          clamp(w);
        }
        w.maximized = !w.maximized;
        d.classList.toggle("maximized", w.maximized);
        if (matrix) max.replaceChildren(lineIcon(w.maximized ? "restore" : "maximize"));
        else max.textContent = w.maximized ? "▣" : "□";
        save(id, w);
      };
      pin.onclick = () => {
        w.pinned = !w.pinned;
        d.classList.toggle("pinned", w.pinned);
        pin.setAttribute("aria-pressed", String(w.pinned));
        pin.dataset.help = w.pinned ? "Открепить окно" : "Закрепить окно";
        save(id, w);
      };
      close.onclick = hide;
      max.onclick = maximize;
      bar.ondblclick = (e) => {
        if (d.open && !e.target.closest("button")) maximize();
      };
      let drag, resize;
      d.addEventListener("focusin", () => { if (d.open) active(w); });
      d.addEventListener("pointerdown", (e) => {
        active(w);
        if (w.pinned || w.maximized || narrow(w) || docked(w) || e.button !== 0) return;
        const r = d.getBoundingClientRect(),
          dx = e.clientX - r.x,
          dy = e.clientY - r.y;
        const right = r.width - dx < 8,
          bottom = r.height - dy < 8,
          left = dx < 6;
        if (right || bottom || left) {
          resize = { x: e.clientX, y: e.clientY, r, right, bottom, left };
          d.setPointerCapture(e.pointerId);
          e.preventDefault();
        }
      });
      bar.onpointerdown = (e) => {
        if (
          !d.open ||
          w.pinned ||
          w.maximized ||
          narrow(w) ||
          docked(w) ||
          e.target.closest("button") ||
          e.button !== 0
        )
          return;
        const r = d.getBoundingClientRect();
        drag = {
          x: e.clientX - r.x,
          y: e.clientY - r.y,
        };
        bar.setPointerCapture(e.pointerId);
      };
      bar.onpointermove = (e) => {
        if (!drag) return;
        const moving = [[id, w], ...linkedPeers(id)],
          rects = moving.map(([, peer]) => peer.dialog.getBoundingClientRect()),
          r = rects[0],
          left = Math.min(...rects.map((r) => r.left)),
          right = Math.max(...rects.map((r) => r.right)),
          top = Math.min(...rects.map((r) => r.top)),
          bottom = Math.max(...rects.map((r) => r.bottom)),
          dx = Math.max(
            4 - left,
            Math.min(e.clientX - drag.x - r.x, innerWidth - 4 - right),
          ),
          dy = Math.max(
            62 - top,
            Math.min(e.clientY - drag.y - r.y, innerHeight - 36 - bottom),
          );
        for (let i = 0; i < moving.length; i++) {
          const [peerID, peer] = moving[i];
          peer.dialog.style.left = rects[i].x + dx + "px";
          peer.dialog.style.top = rects[i].y + dy + "px";
          if (!peer.layout) save(peerID, peer);
        }
      };
      d.addEventListener("pointermove", (e) => {
        if (!resize) return;
        const dx = e.clientX - resize.x,
          dy = e.clientY - resize.y,
          r = resize.r;
        if (resize.right) d.style.width = Math.max(260, r.width + dx) + "px";
        if (resize.left) {
          const width = Math.max(260, r.width - dx);
          d.style.width = width + "px";
          d.style.left = r.right - width + "px";
        }
        if (resize.bottom) d.style.height = Math.max(170, r.height + dy) + "px";
      });
      const finish = () => {
        if (drag) for (const [peerID, peer] of linkedPeers(id)) {
          if (peer.layout) save(peerID, peer);
        }
        const changed = drag || resize;
        drag = null;
        resize = null;
        if (!w.layout || changed) save(id, w);
      };
      for (const event of [
        "pointerup",
        "pointercancel",
        "lostpointercapture",
      ]) {
        bar.addEventListener(event, finish);
        d.addEventListener(event, finish);
      }
      bar.onkeydown = (e) => {
        const delta = {
          ArrowLeft: [-20, 0],
          ArrowRight: [20, 0],
          ArrowUp: [0, -20],
          ArrowDown: [0, 20],
        }[e.key];
        if (!d.open || !delta || w.pinned || w.maximized || narrow(w) || docked(w) || e.target !== bar)
          return;
        e.preventDefault();
        d.style.left = d.offsetLeft + delta[0] + "px";
        d.style.top = d.offsetTop + delta[1] + "px";
        clamp(w);
        save(id, w);
      };
      d.addEventListener("keydown", (e) => {
        if (
          e.key === "Escape" &&
          !document.querySelector(".context-menu:not([hidden]),.picker-popover")
        ) {
          e.preventDefault();
          hide();
        }
      });
      let timer;
      const observer = new ResizeObserver(() => {
        if (!d.open) return;
        const r = d.getBoundingClientRect();
        d.classList.toggle(
          "too-small",
          !narrow(w) && !docked(w) && (r.width < w.minContentWidth || r.height < 220),
        );
        clearTimeout(timer);
        if (!w.layout) timer = setTimeout(() => save(id, w), 200);
        window.dispatchEvent(new CustomEvent("moduleresize", { detail: id }));
      });
      observer.observe(d);
      w.life.own(() => {
        observer.disconnect();
        clearTimeout(timer);
      });
      if (w.layout) w.life.listen(window,"matrixcommslayout",() => {
        if (d.open) clamp(w);
      });
      w.life.listen(window, "resize", () => {
        if (!d.open || narrow(w) || docked(w)) return;
        d.style.width = Math.min(d.offsetWidth, innerWidth - 12) + "px";
        d.style.height = Math.min(d.offsetHeight, innerHeight - 106) + "px";
        clamp(w);
      });
      return content;
    },
    destroy(id) {
      const w = windows.get(id);
      if (!w) return;
      if (w.dialog.open) closeDialog(w);
      w.life.dispose();
      for (const peer of [...(links.get(id) || [])])
        Modules.link(id, peer, false);
      windows.delete(id);
      w.dialog.remove();
      switcher();
      window.dispatchEvent(new CustomEvent("moduledestroy", { detail: id }));
    },
    prepare(id, work) {
      const w = windows.get(id);
      if (w) w.prepare = work;
    },
    wait(id, work) {
      return windows.get(id)?.loading.wait(work);
    },
    // Background updates reuse the open window without focus, inert content or
    // a readiness overlay. Foreground open/load keeps the full loading boundary.
    update(id, work) {
      return windows.get(id)?.loading.refresh(work) ?? Promise.resolve(false);
    },
    load(id, work) {
      if (document.body.classList.contains("planet-mode") && id !== "chat") return;
      return windows.get(id)?.show(work);
    },
    signal: (id) => windows.get(id)?.loading.signal,
    isLoading: (id) => !!windows.get(id)?.loading.pending,
    open: (id) => {
      if (document.body.classList.contains("planet-mode") && id !== "chat")
        return;
      return windows.get(id)?.show();
    },
    focus: (id) => {
      const w = windows.get(id);
      if (w?.dialog.open && !w.dialog.inert &&
          (w.audience !== "admin" || Session.data.state?.user.role === "admin")) {
        if (w.minimized) w.minimize(false);
        active(w);
        w.bar.focus();
      }
    },
    openWindows: () => [...windows].filter(([,w]) =>
      w.dialog.open && (w.audience !== "admin" || Session.data.state?.user.role === "admin"))
      .map(([id,w]) => ({id,title:w.title})),
    close: (id) => windows.get(id)?.hide(),
    isOpen: (id) => !!windows.get(id)?.dialog.open,
    closeAll() {
      for (const w of [...windows.values()]) w.hide();
    },
    clearPrivate() {
      for (const [id, w] of [...windows])
        if (w.transient) Modules.destroy(id);
        else if (w.private) {
          w.content.replaceChildren();
          w.title = tr(w.defaultTitle);
          w.h.textContent = w.title;
          w.h.dataset.i18n = w.defaultTitle;
        }
    },
    refreshTitles() {
      for (const w of windows.values())
        if (w.h.dataset.i18n) w.title = tr(w.h.dataset.i18n);
      switcher();
    },
    title(id, text) {
      const w = windows.get(id);
      if (w) {
        w.title = text;
        w.h.removeAttribute("data-i18n");
        w.h.textContent = text;
        switcher();
      }
    },
    adjacent(id, anchor, side = "right") {
      const w = windows.get(id),
        a = windows.get(anchor);
      if (!w || !a || w.pinned || w.maximized) return;
      const r = a.dialog.getBoundingClientRect();
      w.dialog.style.left =
        (side === "left"
          ? Math.max(6, r.left - w.dialog.offsetWidth - 8)
          : Math.min(innerWidth - w.dialog.offsetWidth - 6, r.right + 8)) +
        "px";
      w.dialog.style.top = r.y + "px";
      clamp(w);
      save(id, w);
    },
    link(a, b, enabled) {
      if (enabled) {
        if (!links.has(a)) links.set(a, new Set());
        if (!links.has(b)) links.set(b, new Set());
        links.get(a).add(b);
        links.get(b).add(a);
      } else {
        links.get(a)?.delete(b);
        links.get(b)?.delete(a);
        if (!links.get(a)?.size) links.delete(a);
        if (!links.get(b)?.size) links.delete(b);
      }
    },
    linked: (id, anchor) =>
      anchor ? !!links.get(id)?.has(anchor) : links.has(id),
    place(id) {
      const w = windows.get(id);
      if (w) {
        clamp(w);
        save(id, w);
      }
    },
    closeAudience(audience) {
      for (const [id, w] of [...windows])
        if (w.audience === audience) {
          w.hide();
          if (windows.has(id)) {
            w.content.replaceChildren();
            if (w.transient) Modules.destroy(id);
          }
        }
    },
    resetLayout() {
      resetting = true;
      for (const [id, w] of [...windows]) {
        if (w.layout) continue;
        w.hide();
        Preferences.remove("module:" + id);
      }
      resetting = false;
    },
  });
}
