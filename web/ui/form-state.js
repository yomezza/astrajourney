// Feature-owned draft values. Restoring a value never invokes a game command.
export class FormState {
  #values = new Map();
  constructor(scope = () => "") {
    this.scope = scope;
  }
  field(create, root, label, id, initial, ...options) {
    const key = `${this.scope()}|${id}`;
    const input = create(
      root,
      label,
      id,
      this.#values.has(key) ? this.#values.get(key) : initial,
      ...options,
    );
    input.addEventListener("input", () => this.#values.set(key, input.value));
    return input;
  }
  clear() {
    this.#values.clear();
  }
  forget(...ids) {
    for (const id of ids) this.#values.delete(`${this.scope()}|${id}`);
  }
}
