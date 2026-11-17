// Public graphics only. Session snapshots and game GETs never enter this cache.
export class AssetCache {
  #entries = new Map();
  constructor(limit = 64) {
    this.limit = limit;
  }
  load(key, read) {
    if (this.#entries.has(key)) return this.#entries.get(key);
    const pending = Promise.resolve()
      .then(read)
      .catch((error) => {
        if (this.#entries.get(key) === pending) this.#entries.delete(key);
        throw error;
      });
    this.#entries.set(key, pending);
    while (this.#entries.size > this.limit)
      this.#entries.delete(this.#entries.keys().next().value);
    return pending;
  }
}
