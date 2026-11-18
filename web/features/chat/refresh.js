// One refresh at a time; a burst requests at most one subsequent pass. A queue
// belongs to one signed-in session and is disposed before replacing the client.
export class RefreshQueue {
  constructor(refresh, onError, {delay = 150, interval = 350, jitter = () => Math.random() * 150} = {}) {
    Object.assign(this, {refresh, onError, delay, interval, jitter});
    this.last = 0;
  }
  schedule(delay = this.delay + this.jitter()) {
    if (this.closed) return;
    if (this.task) { this.dirty = true; return; }
    if (this.timer) return;
    this.timer = setTimeout(() => { this.timer = null; this.run(); }, delay);
  }
  run() {
    if (this.closed) return Promise.resolve();
    this.dirty = true;
    if (this.task) return this.task;
    clearTimeout(this.timer);
    this.timer = null;
    const task = Promise.resolve().then(async () => {
      while (this.dirty && !this.closed) {
        this.dirty = false;
        const wait = Math.max(0, this.last + this.interval - Date.now());
        if (wait) await new Promise(resolve => {
          this.wake = resolve;
          this.wait = setTimeout(resolve, wait);
        });
        if (this.closed) break;
        this.dirty = false;
        this.last = Date.now();
        try { await this.refresh(); }
        catch (error) {
          if (this.closed) break;
          this.onError(error);
          if (error.status === 429 || error.status === 503) {
            this.dirty = false;
            this.retry = Math.max(1000, error.retryAfter || 1000);
            break;
          }
        }
      }
    }).finally(() => {
      if (this.task === task) this.task = null;
      if (this.retry && !this.closed) {
        const delay = this.retry;
        this.retry = 0;
        this.schedule(delay + this.jitter());
      }
    });
    this.task = task;
    return task;
  }
  close() {
    this.closed = true;
    this.dirty = false;
    clearTimeout(this.timer);
    clearTimeout(this.wait);
    this.wake?.();
  }
}
