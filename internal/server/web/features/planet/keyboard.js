// Physical codes work with either Latin or Russian keyboard layouts. Input is
// a bounded direction lease; coordinates and speed remain entirely server-owned.
export function keyboardMovement(canvas, current, steer) {
  const keys = new Set(), codes = new Set(["KeyW", "KeyA", "KeyS", "KeyD"]);
  let timer = null, last = "0,0", disposed = false;
  const editing = (element) => element?.closest?.(
    'input,textarea,select,[contenteditable]:not([contenteditable="false"]),[role="textbox"],#window-chat',
  );
  const send = (renew = false) => {
    const dx = Number(keys.has("KeyD")) - Number(keys.has("KeyA")),
      dy = Number(keys.has("KeyS")) - Number(keys.has("KeyW")), next = `${dx},${dy}`;
    if (renew || next !== last) { steer(dx, dy); last = next; }
    if (next === "0,0") { clearInterval(timer); timer = null; }
    else if (!timer) timer = setInterval(() => {
      if (!current() || document.hidden || editing(document.activeElement)) reset();
      else send(true);
    }, 250);
  };
  function reset() { keys.clear(); send(); }
  function down(event) {
    if (event.repeat || disposed || !codes.has(event.code) || !current() || document.hidden || editing(event.target) || event.ctrlKey || event.metaKey || event.altKey) return;
    event.preventDefault();
    if (!keys.has(event.code)) { keys.add(event.code); canvas.dataset.control = "keyboard"; send(); }
  }
  function up(event) { if (codes.has(event.code) && keys.delete(event.code)) { event.preventDefault(); send(); } }
  function focus(event) { if (editing(event.target)) reset(); }
  function visibility() { if (document.hidden) reset(); }
  window.addEventListener("keydown", down, true);
  window.addEventListener("keyup", up, true);
  window.addEventListener("blur", reset);
  document.addEventListener("visibilitychange", visibility);
  document.addEventListener("focusin", focus);
  return {
    reset,
    mouse() { reset(); canvas.dataset.control = "mouse"; },
    stop() {
      if (disposed) return;
      reset(); disposed = true;
      window.removeEventListener("keydown", down, true);
      window.removeEventListener("keyup", up, true);
      window.removeEventListener("blur", reset);
      document.removeEventListener("visibilitychange", visibility);
      document.removeEventListener("focusin", focus);
    },
  };
}
