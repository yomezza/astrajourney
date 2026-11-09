import { uuid } from "../../core/uuid.js";
import { createController } from "./controller.js";
import { api } from "../../core/api.js";
import { Session, RequestScope } from "../../core/session.js";
import { I18n } from "../../core/i18n.js";
import { UI, Modules } from "../../ui/runtime.js";
import { notify } from "../../ui/feedback.js";
const key = "planet-visit",
  scope = new RequestScope();
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
    { signal },
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
  const root = Modules.create(key, "Планетарная локация", 1000, 760);
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
  let welcome,
    returning = false,
    connecting = false,
    canReturn = false,
    finishLeave,
    recoveryStart = 0,
    attempts = 0;
  let commandSeq = 1,
    serverSeq = 0,
    lastFrame = 0,
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
  const leave = button("Вернуться в космос", async () => {
    leave.disabled = true;
    returning = true;
    clearTimeout(retryTimer);
    renderer?.stop();
    renderer = null;
    try {
      if (socket?.readyState === WebSocket.OPEN && welcome) {
        await new Promise((resolve) => {
          const timer = setTimeout(resolve, 4000);
          finishLeave = () => {
            clearTimeout(timer);
            resolve();
          };
          if (!send("leave", { operation: uuid() })) {
            clearTimeout(timer);
            resolve();
          }
        });
      }
      const state = await api(
        "planet/return",
        { operation: uuid(), visit: welcome?.visit || "" },
        task.signal,
      );
      if (!task.current()) return;
      socket?.close();
      socket = null;
      canvas.hidden = true;
      resume.hidden = true;
      status.textContent = tr(
        state.state === "none" ? "Возвращение завершено" : "Завершаем высадку",
      );
      clearInterval(polling);
      if (state.state !== "none")
        polling = setInterval(async () => {
          try {
            const v = await api("planet/visit", undefined, task.signal);
            if (!task.current()) return;
            if (v.state === "none") {
              clearInterval(polling);
              polling = null;
              status.textContent = tr("Возвращение завершено");
            }
          } catch {}
        }, 1500);
    } catch (e) {
      if (task.current()) {
        notify(e.message, "error");
        returning = false;
        leave.disabled = false;
        resume.hidden = false;
      }
    }
  });
  leave.disabled = true;
  resume.hidden = true;
  tools.append(
    resume,
    leave,
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
  Modules.open(key);
  function reconnect() {
    if (!task.current() || returning) return;
    if (!recoveryStart) recoveryStart = Date.now();
    if (attempts >= 8 || Date.now() - recoveryStart >= 60000) {
      resume.hidden = false;
      status.textContent = tr(
        "Не удалось восстановить связь. Продолжите высадку вручную или вернитесь на корабль.",
      );
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
    if (connecting || !task.current() || returning) return;
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
      let hello;
      if (["active", "disconnected"].includes(state.state))
        hello = { resume_visit: state.visit };
      else if (["legacy", "return_pending"].includes(state.state)) {
        status.textContent = tr("Завершаем высадку");
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
        status.textContent = tr("Выберите доступную посадочную площадку");
        resume.hidden = false;
        return;
      }
      canReturn = true;
      site = null;
      const url = new URL("/planet/ws/v1", location.href);
      url.protocol = location.protocol === "https:" ? "wss:" : "ws:";
      const conn = new WebSocket(url);
      const handshake = setTimeout(() => conn.close(), 5000);
      socket = conn;
      let fatal = false,
        connectedAt = 0;
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
        lastFrame = Date.now();
        conn.send(
          JSON.stringify({ v: 1, type: "hello", seq: 1, payload: hello }),
        );
        hello = null;
        monitor = setInterval(() => {
          if (current() && Date.now() - lastFrame > 6000) conn.close();
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
          if (message.type === "visit.ended") {
            finishLeave?.();
            return;
          }
          if (returning) return;
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
            } else welcome = p;
            connectedAt = Date.now();
            resume.hidden = true;
            status.textContent =
              welcome.planet + " · " + tr("Сектор") + " " + welcome.sector;
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
            canvas.dataset.sector = String(welcome.sector);
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
              : welcome.planet +
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
      conn.onclose = () => {
        clearTimeout(handshake);
        if (!task.current() || socket !== conn) return;
        clearInterval(monitor);
        activeOffer = null;
        transitionPanel.hidden = true;
        viewGeneration++;
        locationAbort?.abort();
        renderer?.stop();
        renderer = null;
        finishLeave?.();
        if (returning) return;
        if (fatal) {
          resume.hidden = false;
          return;
        }
        reconnect();
      };
      conn.onerror = () => {
        if (task.current() && socket === conn)
          status.textContent = tr("Не удалось подключиться");
      };
    } catch (e) {
      if (task.current() && !returning) {
        status.textContent = e.message;
        if ([401, 403, 400, 409].includes(e.status)) resume.hidden = false;
        else reconnect();
      }
    } finally {
      connecting = false;
      if (task.current()) leave.disabled = !canReturn;
    }
  }
  try {
    await connect();
  } finally {
    busy = false;
  }
}
window.addEventListener("signedout", stop);
window.addEventListener("moduleclose", (e) => {
  if (e.detail === key) stop();
});
window.addEventListener("moduledestroy", (e) => {
  if (e.detail === key) stop();
});
