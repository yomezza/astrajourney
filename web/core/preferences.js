const volatile = new Map();
export const Preferences = {
  read(key, fallback, valid = () => true) {
    let value;
    try {
      if (volatile.has(key)) value = volatile.get(key);
      else {
        const raw = localStorage.getItem(key);
        if (raw !== null) {
          try {
            value = JSON.parse(raw);
          } catch {
            value = raw;
          }
        }
      }
    } catch {
      value = volatile.get(key);
    }
    const initial = typeof fallback === "function" ? fallback() : fallback;
    const sameType =
      initial === undefined ||
      (typeof value === typeof initial &&
        (initial === null ||
          typeof initial !== "object" ||
          (value !== null && Array.isArray(value) === Array.isArray(initial))));
    return value !== undefined && sameType && valid(value) ? value : initial;
  },
  write(key, value) {
    try {
      localStorage.setItem(key, JSON.stringify(value));
      volatile.delete(key);
    } catch {
      volatile.set(key, value);
    }
  },
  // Existing unquoted language strings remain compatible with older releases.
  language(fallback = "ru") {
    if (volatile.has("language")) return volatile.get("language");
    try {
      const value = localStorage.getItem("language");
      return ["ru", "en"].includes(value) ? value : fallback;
    } catch {
      return volatile.get("language") || fallback;
    }
  },
  setLanguage(value) {
    try {
      localStorage.setItem("language", value);
      volatile.delete("language");
    } catch {
      volatile.set("language", value);
    }
  },
  remove(key) {
    volatile.delete(key);
    try {
      localStorage.removeItem(key);
    } catch {}
  },
  key(feature, { account, instance, version } = {}) {
    return [feature, account, instance, version && "v" + version]
      .filter((v) => v !== undefined && v !== null)
      .join(":");
  },
};
