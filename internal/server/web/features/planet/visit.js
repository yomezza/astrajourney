import { PlanetMode } from "./mode.js";
import { refresh } from "../../core/snapshot-store.js";
import { uuid } from "../../core/uuid.js";
import { createController } from "./controller.js";
import { livenessAction } from "./liveness.js";
import { api } from "../../core/api.js";
import { Session, RequestScope } from "../../core/session.js";
import { I18n } from "../../core/i18n.js";
import { UI, Modules } from "../../ui/runtime.js";
import { notify } from "../../ui/feedback.js";
const scope = new RequestScope();
let socket,
  renderer,
  polling,
  retryTimer,
  monitor,
  busy = false;
const tr = (s) => I18n.t(s);
const terminalErrors = new Set([
  "auth_required",
  "access_denied",
  "session_replaced",
  "visit_unavailable",
  "geometry_changed",
  "unsupported_version",
]);
function stop() {
  scope.invalidate();
  clearTimeout(retryTimer);
  clearInterval(monitor);
  clearInterval(polling);
  retryTimer = monitor = polling = null;
  renderer?.stop();
  renderer = null;
  socket?.close();
  socket = null;
}
async function requestScene(w, signal) {
  const response = await fetch(
    `/planet/api/v1/scene?visit=${encodeURIComponent(w.visit)}&epoch=${w.connection_epoch}`,
    { signal: AbortSignal.any([signal, AbortSignal.timeout(12000)]) },
  );
  if (!response.ok) {
    const error = await response.json();
    throw new Error(tr(error.error));
  }
  return response.json();
}
export async function openVisit(site) {
  if (busy) return;
  busy = true;
  stop();
  const task = scope.begin();
  const button = (label, ...args) => {
    const b = UI.button(label, ...args);
    if (label.length > 1) b.dataset.i18n = label;
    return b;
  };
  const root = PlanetMode.host();
  PlanetMode.loading("Подготовка планетарной высадки", () => connect());
  const status = UI.node("p", tr("Подключение к планетарной службе")),
    tools = UI.node("div", undefined, "visit-tools"),
    canvas = UI.node("canvas");
  canvas.width = canvas.height = 1024;
  canvas.className = "visit-canvas";
  canvas.hidden = true;
  const viewport = UI.node("div", undefined, "visit-viewport");
  const objectInfo = UI.node("div", undefined, "visit-object-info");
  objectInfo.hidden = true;
  viewport.append(canvas, objectInfo);
  const artWarning = UI.node("div", undefined, "visit-art-warning");
  artWarning.hidden = true;
  const artWarningText = UI.node("span", tr("Часть графики недоступна"));
  artWarningText.dataset.i18n = "Часть графики недоступна";
  artWarning.append(
    artWarningText,
    button("Повторить графику", () => connect()),
  );
  root.classList.add("visit-content");
  let landingSector = null,
    landingSurface = null,
    welcome,
    returning = false,
    connecting = false,
    canReturn = false,
    returnWaiter,
    returnOperation = null,
    recoveryStart = 0,
    attempts = 0;
  let commandSeq = 1,
    serverSeq = 0,
    lastFrame = 0,
    lastLabelRead = 0,
    lastState,
    lastPath,
    peers = new Map();
  const send = (type, payload) => {
    if (socket?.readyState !== WebSocket.OPEN || !welcome) return false;
    socket.send(
      JSON.stringify({
        v: 1,
        type,
        seq: ++commandSeq,
        visit: welcome.visit,
        connection_epoch: welcome.connection_epoch,
        payload,
      }),
    );
    return commandSeq;
  };
  const resume = button("Продолжить высадку", () => {
    attempts = 0;
    recoveryStart = 0;
    return connect();
  });
  async function finishReturn(state) {
    const start = Date.now();
    while (task.current()) {
      if (state.state === "none") {
        stop();
        root.replaceChildren();
        PlanetMode.leave();
        await refresh(true);
        return;
      }
      if (Date.now() - start > 30000)
        throw Error(
          tr("Возврат ещё подтверждается сервером. Повторите проверку."),
        );
      await new Promise((resolve) => {
        const done = () => {
          task.signal.removeEventListener("abort", done);
          resolve();
        };
        polling = setTimeout(done, 1000);
        task.signal.addEventListener("abort", done, { once: true });
      });
      if (!task.current()) return;
      state = await api(
        "planet/visit",
        undefined,
        AbortSignal.any([task.signal, AbortSignal.timeout(8000)]),
      );
    }
  }
  function showFailure(message) {
    status.textContent = message;
    resume.hidden = false;
    if (recover.hidden) PlanetMode.failed(message, () => connect());
    else PlanetMode.ready();
  }
  async function normalReturn() {
    if (returning || !welcome || socket?.readyState !== WebSocket.OPEN) return;
    returning = true;
    clearTimeout(retryTimer);
    PlanetMode.loading("Возвращение на корабль", () => connect());
    try {
      returnOperation ||= uuid();
      await new Promise((resolve, reject) => {
        const timer = setTimeout(() => {
          returnWaiter = null;
          reject(
            Error(
              tr("Разрешение на возврат не получено. Повторите подключение."),
            ),
          );
        }, 8000);
        returnWaiter = {
          operation: returnOperation,
          resolve: () => {
            clearTimeout(timer);
            returnWaiter = null;
            resolve();
          },
          reject: (error) => {
            clearTimeout(timer);
            returnWaiter = null;
            reject(error);
          },
        };
        if (!send("return.prepare", { operation: returnOperation })) {
          clearTimeout(timer);
          returnWaiter = null;
          reject(Error(tr("Нет соединения с планетарной службой")));
        }
      });
      const state = await api(
        "planet/return",
        { operation: returnOperation, visit: welcome.visit },
        AbortSignal.any([task.signal, AbortSignal.timeout(12000)]),
      );
      if (task.current()) await finishReturn(state);
    } catch (error) {
      if (task.current()) {
        returning = false;
        resume.hidden = false;
        notify(error.message, "error");
        showFailure(error.message);
      }
    }
  }
  const recover = button("Восстановить высадку", async () => {
    if (returning) return;
    returning = true;
    PlanetMode.loading("Восстановление высадки", () => connect());
    try {
      const state = await api(
        "planet/recover",
        { operation: uuid(), visit: welcome?.visit || "" },
        task.signal,
      );
      if (task.current()) await finishReturn(state);
    } catch (error) {
      if (task.current()) {
        returning = false;
        showFailure(error.message);
      }
    }
  });
  recover.hidden = true;
  resume.hidden = true;
  tools.append(
    resume,
    recover,
    button("Чат", () => Modules.open("chat")),
    button("Полный экран", async () => {
      try {
        if (!document.fullscreenElement)
          await document.documentElement.requestFullscreen?.();
        else await document.exitFullscreen?.();
      } catch {
        notify("Полноэкранный режим недоступен", "warning");
      }
    }),
    button("−", () => renderer?.zoom(1 / 1.2)),
    button("+", () => renderer?.zoom(1.2)),
    button("К персонажу", () => renderer?.centre()),
    button("Весь сектор", () => renderer?.fit()),
  );
  const transitionPanel = UI.node("div", undefined, "visit-transition");
  transitionPanel.hidden = true;
  let activeOffer = null,
    appliedOperation = null,
    viewGeneration = 0,
    locationAbort;
  function showOffer(offer) {
    activeOffer = offer;
    transitionPanel.hidden = false;
    const confirm = button(
      "Перейти в соседний сектор",
      () => {
        confirm.disabled = true;
        PlanetMode.loading("Переход в соседний сектор", () => connect());
        if (!activeOffer.operation) activeOffer.operation = uuid();
        send("transition.confirm", {
          offer: offer.offer,
          operation: activeOffer.operation,
        });
      },
      !offer.ready,
    );
    transitionPanel.replaceChildren(
      UI.node("strong", tr("Переход в сектор") + " " + offer.neighbor),
      UI.node("span", tr(offer.owner), "muted"),
      UI.node(
        "span",
        tr(
          offer.ready
            ? "Соседняя локация готова"
            : "Подготавливаем соседнюю локацию…",
        ),
        "muted",
      ),
      confirm,
      button("Остаться", () =>
        send("transition.cancel", { offer: offer.offer }),
      ),
    );
  }
  root.replaceChildren(
    status,
    tools,
    UI.node("p", tr("ЛКМ — идти · Колесо — масштаб · ПКМ — обзор"), "muted"),
    transitionPanel,
    artWarning,
    viewport,
  );
  PlanetMode.enter();
  function reconnect() {
    if (!task.current() || returning) return;
    if (!recoveryStart) recoveryStart = Date.now();
    if (attempts >= 8 || Date.now() - recoveryStart >= 60000) {
      resume.hidden = false;
      status.textContent = tr(
        "Не удалось восстановить связь. Продолжите высадку вручную или вернитесь на корабль.",
      );
      PlanetMode.ready();
      return;
    }
    const delay =
      Math.min(8000, 500 * 2 ** attempts) * (0.8 + Math.random() * 0.4);
    attempts++;
    status.textContent =
      tr("Восстанавливаем соединение с планетарной службой") +
      ` · ${attempts}/8`;
    clearTimeout(retryTimer);
    retryTimer = setTimeout(() => connect(), delay);
  }
  async function connect() {
    if (connecting || !task.current()) return;
    returning = false;
    returnOperation = null;
    PlanetMode.loading("Подключение к планетарной службе", () => connect());
    connecting = true;
    clearTimeout(retryTimer);
    clearInterval(monitor);
    renderer?.stop();
    renderer = null;
    canvas.dataset.ready = "false";
    const previous = socket;
    socket = null;
    previous?.close();
    welcome = null;
    commandSeq = 1;
    serverSeq = 0;
    lastState = lastPath = null;
    peers = new Map();
    activeOffer = null;
    transitionPanel.hidden = true;
    viewGeneration++;
    locationAbort?.abort();
    resume.hidden = true;
    try {
      const state = await api(
        "planet/visit",
        undefined,
        AbortSignal.any([task.signal, AbortSignal.timeout(8000)]),
      );
      if (!task.current()) return;
      canReturn = state.state !== "none";
      recover.hidden = !state.recoverable;
      let hello;
      if (["active", "disconnected"].includes(state.state))
        hello = { resume_visit: state.visit };
      else if (state.state === "return_pending") {
        returning = true;
        await finishReturn(state);
        return;
      } else if (state.state === "legacy") {
        resume.hidden = false;
        recover.hidden = !state.recoverable;
        PlanetMode.ready();
        status.textContent = tr(
          "Для старой высадки доступно серверное восстановление",
        );
        return;
      } else if (site || state.state === "pending") {
        const ticket = await api(
          "planet/handoff",
          {
            operation: state.operation || uuid(),
            landing_site: String(state.landing_site || site),
            ship: String(state.ship || Session.data.expedition.ship.id),
          },
          task.signal,
        );
        if (!task.current()) return;
        if (ticket.ticket) {
          hello = { ticket: ticket.ticket };
          delete ticket.ticket;
        } else if (ticket.visit) hello = { resume_visit: ticket.visit };
        else throw new Error(tr("Повторите подключение"));
      } else {
        PlanetMode.leave();
        stop();
        return;
      }
      canReturn = true;
      site = null;
      const url = new URL("/planet/ws/v1", location.href);
      url.protocol = location.protocol === "https:" ? "wss:" : "ws:";
      const conn = new WebSocket(url);
      const handshake = setTimeout(() => conn.close(), 12000);
      socket = conn;
      let fatal = false,
        connectedAt = 0,
        openedAt = 0,
        lastProbeAt = 0;
      const current = () =>
        task.current() &&
        !returning &&
        socket === conn &&
        conn.readyState === WebSocket.OPEN;
      conn.onopen = () => {
        clearTimeout(handshake);
        if (!current()) {
          conn.close();
          return;
        }
        openedAt = lastFrame = Date.now();
        conn.send(
          JSON.stringify({ v: 1, type: "hello", seq: 1, payload: hello }),
        );
        hello = null;
        monitor = setInterval(async () => {
          if (current()) {
            const now = Date.now();
            const action = livenessAction(
              now,
              openedAt,
              lastFrame,
              !!welcome,
              lastProbeAt,
            );
            if (action === "timeout") {
              console.warn("Planet WS timed out", {
                phase: welcome ? "stream" : "welcome",
                silentMs: now - lastFrame,
              });
              conn.close(4000, "planet_timeout");
              return;
            }
            if (action === "probe") {
              lastProbeAt = now;
              send("resync", { last_server_seq: serverSeq });
            }
          }
          if (current() && welcome && Date.now() - lastLabelRead > 15000) {
            lastLabelRead = Date.now();
            try {
              const v = await api(
                "planet/visit",
                undefined,
                AbortSignal.any([task.signal, AbortSignal.timeout(8000)]),
              );
              if (!current()) return;
              if (v.state === "none") {
                returning = true;
                await finishReturn(v);
                return;
              }
              if (v.planet_name) welcome.planet = v.planet_name;
              welcome.colony_name = v.colony_name || "";
            } catch {}
          }
        }, 1000);
      };
      conn.onmessage = async (event) => {
        if (!task.current() || socket !== conn) return;
        lastFrame = Date.now();
        try {
          const message = JSON.parse(event.data);
          if (message.v !== 1) {
            fatal = true;
            conn.close();
            return;
          }
          if (message.server_seq <= serverSeq) return;
          serverSeq = message.server_seq;
          if (message.type === "return.authorized") {
            if (
              returnWaiter &&
              welcome &&
              message.visit === welcome.visit &&
              message.connection_epoch === welcome.connection_epoch &&
              message.payload.operation === returnWaiter.operation
            )
              returnWaiter.resolve();
            return;
          }
          if (returning) {
            if (message.type === "error")
              returnWaiter?.reject(Error(tr(message.payload.code)));
            return;
          }
          if (message.type === "visit.ended") {
            connect();
            return;
          }

          if (
            welcome &&
            (message.visit !== welcome.visit ||
              (message.connection_epoch !== welcome.connection_epoch &&
                !(
                  message.type === "transition.complete" &&
                  message.connection_epoch > welcome.connection_epoch
                )))
          )
            return;
          const p = message.payload;
          if (message.type === "error") {
            status.textContent = tr(p.code);
            PlanetMode.ready();
            renderer?.error(p.code, p.seq);
            if (terminalErrors.has(p.code)) {
              fatal = true;
              conn.close();
            }
            return;
          }
          if (message.type === "transition.offer") {
            showOffer(p);
            return;
          }
          if (message.type === "transition.cancelled") {
            PlanetMode.ready();
            activeOffer = null;
            transitionPanel.hidden = true;
            if (!["move", "cancelled"].includes(p.reason))
              notify(tr(p.reason), "warning");
            return;
          }
          if (
            message.type === "welcome" ||
            message.type === "transition.complete"
          ) {
            if (message.type === "transition.complete") {
              if (appliedOperation === p.operation) return;
              appliedOperation = p.operation;
              welcome = p.welcome;
              activeOffer = null;
              transitionPanel.hidden = true;
              peers = new Map();
              lastState = lastPath = null;
            } else {
              welcome = p;
              landingSector = p.landing_sector;
              landingSurface = p.landing_surface;
            }
            connectedAt = Date.now();
            resume.hidden = true;
            status.textContent =
              (welcome.colony_name || welcome.planet) +
              " · " +
              tr("Сектор") +
              " " +
              welcome.sector;
            locationAbort?.abort();
            locationAbort = new AbortController();
            const graphicsSignal = AbortSignal.any([
              task.signal,
              locationAbort.signal,
            ]);
            const generation = ++viewGeneration;
            const locationCurrent = () =>
              current() && generation === viewGeneration;
            renderer?.stop();
            renderer = null;
            canvas.dataset.ready = "false";
            const scene = await requestScene(welcome, graphicsSignal);
            if (!locationCurrent()) return;
            canvas.hidden = false;
            const controller = await createController(canvas, scene, welcome, {
              current: locationCurrent,
              target: (x, y) => send("move", { x_q: x, y_q: y }),
              signal: graphicsSignal,
              assets: (missing) => {
                if (locationCurrent()) artWarning.hidden = !missing.length;
              },
              onInteract: (info, event) => {
                if (info?.id !== "landing") return false;
                const x = lastState?.x_q ?? welcome.x_q,
                  y = lastState?.y_q ?? welcome.y_q;
                const near =
                  String(welcome.surface) === String(landingSurface) &&
                  welcome.sector === landingSector &&
                  scene.landing &&
                  Math.hypot(
                    x - scene.landing.SpawnX,
                    y - scene.landing.SpawnY,
                  ) <= 5120 &&
                  !lastState?.moving;
                UI.menu(
                  [
                    {
                      label: "Подойти к модулю",
                      action: () =>
                        send("move", {
                          x_q: scene.landing.SpawnX,
                          y_q: scene.landing.SpawnY,
                        }),
                    },
                    {
                      label: "Вернуться на корабль",
                      disabled: !near || returning || !!activeOffer,
                      action: normalReturn,
                    },
                  ],
                  event.clientX,
                  event.clientY,
                  { owner: canvas, theme: "chat" },
                );
                return true;
              },
              onHover: (info) => {
                if (!locationCurrent()) return;
                objectInfo.hidden = !info;
                objectInfo.textContent = info
                  ? info.name + " · " + info.text
                  : "";
              },
            });
            if (!locationCurrent()) {
              controller?.stop();
              return;
            }
            renderer = controller;
            canvas.dataset.ready = "true";
            PlanetMode.ready();
            canvas.dataset.sector = String(welcome.sector);
            canvas.dataset.connectionEpoch = String(welcome.connection_epoch);
            renderer?.actors([...peers.values()], true);
            if (lastState) renderer?.update(lastState);
            if (lastPath) renderer?.accepted(lastPath);
            return;
          }
          if (!welcome) return;
          if (message.type === "sector.snapshot") {
            peers = new Map(p.actors.map((a) => [a.actor, a]));
            renderer?.actors(p.actors, true);
            lastState = p.self;
            lastPath = p.self;
            renderer?.accepted(p.self);
            renderer?.update(p.self);
          } else if (message.type === "actor.enter") {
            peers.set(p.actor, p);
            renderer?.actors([p]);
          } else if (message.type === "actor.leave") {
            peers.delete(p.actor);
            renderer?.remove(p.actor);
          } else if (message.type === "path.accepted") {
            lastPath = p;
            renderer?.accepted(p);
          } else if (message.type === "state.delta") {
            lastState = p;
            renderer?.update(p);
            for (const a of p.actors || []) peers.set(a.actor, a);
            renderer?.actors(p.actors || []);
            status.textContent = p.suspended
              ? tr(
                  activeOffer
                    ? "Ожидаем подтверждения перехода"
                    : "Движение приостановлено: проверяем связь с сервером",
                )
              : (welcome.colony_name || welcome.planet) +
                " · " +
                tr("Сектор") +
                " " +
                welcome.sector +
                " · " +
                tr("Пилотов в секторе") +
                ": " +
                peers.size;
            if (connectedAt && Date.now() - connectedAt > 10000) {
              attempts = 0;
              recoveryStart = 0;
            }
          }
        } catch (e) {
          if (current()) {
            status.textContent = e.message;
            conn.close();
          }
        }
      };
      conn.onclose = (event) => {
        clearTimeout(handshake);
        if (!task.current() || socket !== conn) return;
        clearInterval(monitor);
        if (event.code !== 1000)
          console.warn("Planet WS disconnected", {
            code: event.code,
            reason: event.reason || "no close reason",
            welcomed: !!welcome,
          });
        activeOffer = null;
        transitionPanel.hidden = true;
        viewGeneration++;
        locationAbort?.abort();
        renderer?.stop();
        renderer = null;
        returnWaiter?.reject(Error(tr("Соединение прервано")));
        if (returning) return;
        if (fatal) {
          resume.hidden = false;
          showFailure(status.textContent);
          return;
        }
        reconnect();
      };
      conn.onerror = () => {
        if (task.current() && socket === conn)
          status.textContent = tr("Не удалось подключиться");
      };
    } catch (e) {
      if (task.current()) {
        returning = false;
        status.textContent = e.message;
        if ([401, 403, 400, 409].includes(e.status)) resume.hidden = false;
        else reconnect();
        showFailure(e.message);
      }
    } finally {
      connecting = false;
    }
  }
  try {
    await connect();
  } finally {
    busy = false;
  }
}
window.addEventListener("signedout", stop);
