// Own subscriptions, observers and timers independently of gameplay operations.
export class Lifetime {
  #cleanup = new Set();
  #disposed = false;
  own(cleanup) {
    if (this.#disposed) cleanup();
    else this.#cleanup.add(cleanup);
    return cleanup;
  }
  listen(target, event, callback, options) {
    target.addEventListener(event, callback, options);
    return this.own(() => target.removeEventListener(event, callback, options));
  }
  dispose() {
    if (this.#disposed) return;
    this.#disposed = true;
    for (const cleanup of this.#cleanup) cleanup();
    this.#cleanup.clear();
  }
}
