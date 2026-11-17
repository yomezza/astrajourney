// Renderers receive the lease and must check it before their first DOM write.
// Handling errors here also covers search timers and fire-and-forget UI events.
export function scopedView(scope, draw, onError) {
  const render = async (...args) => {
    const lease = scope.begin({ abort: false });
    if (!lease.current()) return false;
    try {
      return await draw(lease, ...args);
    } catch (error) {
      if (lease.current() && error.name !== "AbortError") onError(error);
      return false;
    }
  };
  render.invalidate = () => scope.invalidate();
  return render;
}
