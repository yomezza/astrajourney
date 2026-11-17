import { Lifetime } from "./lifetime.js";
// Scheduling only: visibility never cancels a server-side flight or mining job.
export function sceneLoop(visible, draw, interval = 100) {
  const life = new Lifetime();
  let timer;
  const tick = () => {
    if (document.hidden || !visible()) {
      clearInterval(timer);
      timer = null;
      return;
    }
    draw();
  };
  const sync = () => {
    if (document.hidden || !visible()) {
      clearInterval(timer);
      timer = null;
    } else {
      if (!timer) timer = setInterval(tick, interval);
      tick();
    }
  };
  for (const name of ["moduleopen", "moduleclose", "moduleresize", "signedout"])
    life.listen(window, name, sync);
  life.listen(document, "visibilitychange", sync);
  life.own(() => clearInterval(timer));
  return life;
}
