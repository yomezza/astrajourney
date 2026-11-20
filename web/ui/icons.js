const paths = {
  chat: "M5 5h14v11H9l-4 3V5zM8 9h8M8 13h5",
  galaxy: "M12 3v18M3 12h18M5 5l14 14M5 19L19 5",
  ship: "M3 17L21 3l-6 18-4-8-8 4z",
  research: "M4 20l9-9m-6-7c5-2 10 1 13 6l-4 3c-1-4-3-6-7-6z",
  market: "M3 7h16l-4-4m6 14H5l4 4",
  corporations: "M12 2l9 5v10l-9 5-9-5V7zM8 9h8v7H8z",
  tasks: "M8 6h13M8 12h13M8 18h13M3 6h1M3 12h1M3 18h1",
  studies:
    "M12 2v4M12 18v4M2 12h4M18 12h4M7 5l2 3M17 5l-2 3M7 19l2-3M17 19l-2-3M7 12a5 5 0 1 0 10 0a5 5 0 1 0-10 0",
  ideas: "M12 3a6 6 0 0 1 4 10l-1 2H9l-1-2a6 6 0 0 1 4-10zM9 18h6M10 21h4",
  guide: "M3 4h7l2 2 2-2h7v15h-7l-2 2-2-2H3zM12 6v15",
  portal:
    "M12 4c5 0 9 4 9 8s-4 8-9 8-9-4-9-8 4-8 9-8zM8 12c0-3 2-5 4-5s4 2 4 5-2 5-4 5-4-2-4-5z",
  system: "M12 12h.01M20 12a8 4 0 1 1-16 0 8 4 0 1 1 16 0ZM17 6a4 8 45 1 1-10 12 4 8 45 1 1 10-12Z",
  cargo: "M4 6l8-3 8 3v12l-8 3-8-3V6Zm0 0 8 3 8-3m-8 3v12",
  logistics: "M3 4h18v16H3zM3 9h18M9 9v11m6-11v11",
  colonies: "m12 3 8 5v8l-8 5-8-5V8zM9 13h6m-3-3v6",
  finance: "M4 3h16v18H4zM8 8h8m-8 4h8m-8 4h4",
  profile: "M5 21v-2a7 7 0 0 1 14 0v2M16 7a4 4 0 1 1-8 0 4 4 0 0 1 8 0Z",
  settings: "M3 6h18M3 12h18M3 18h18M8 4v4m8 2v4m-5 2v4",
  notifications: "M6 8a6 6 0 0 1 12 0v6l3 3H3l3-3V8M9 21h6",
  administration: "M4 4h16v16H4zM8 9l3 3-3 3m6 0h3",
  catalog: "M16 10a6 6 0 1 1-12 0 6 6 0 0 1 12 0Zm-2 4 6 6",
  studio: "M3 4h18v16H3zM3 9h18m-11 0v11",
  close: "m6 6 12 12M18 6 6 18",
  maximize: "M5 5h14v14H5z",
  restore: "M8 5h11v11M5 8h11v11H5z",
  minimize: "M5 17h14",
  pin: "M12 15v6M8 4h8l-1 5 3 5H6l3-5z",
  left: "M5 12h14m-9-5-5 5 5 5",
  right: "M5 12h14m-5-5 5 5-5 5",
  wide: "M3 12h18M7 8l-4 4 4 4m10-8 4 4-4 4",
  plus: "M5 12h14m-7-7v14",
  eye: "M2 12s4-6 10-6 10 6 10 6-4 6-10 6S2 12 2 12Zm13 0a3 3 0 1 1-6 0 3 3 0 0 1 6 0Z",
};

export function lineIcon(name) {
  const path = paths[name];
  if (!path) return null;
  const icon = document.createElementNS("http://www.w3.org/2000/svg", "svg");
  icon.setAttribute("viewBox", "0 0 24 24");
  icon.setAttribute("aria-hidden", "true");
  icon.classList.add("line-icon");
  const shape = document.createElementNS(icon.namespaceURI, "path");
  shape.setAttribute("d", path);
  icon.append(shape);
  return icon;
}
