import { bootstrapPortal } from "./portal-shell";
import { createPortalSessionFetch } from "./portal-session-boundary";

const host = document.getElementById("vastplan-portal");
if (host === null) {
  throw new Error("Portal Shell 缺少 #vastplan-portal 宿主节点");
}

// The Portal owns one shadow tree. Framework CSS and ordinary plugin selectors
// cannot escape into a surrounding product page; plugins never receive host.
const shadow = host.shadowRoot ?? host.attachShadow({ mode: "open" });
let mount = shadow.getElementById("vastplan-portal-root");
if (mount === null) {
  mount = document.createElement("div");
  mount.id = "vastplan-portal-root";
  shadow.replaceChildren(mount);
}

const portalFetch = createPortalSessionFetch(globalThis.fetch.bind(globalThis), globalThis.location);
globalThis.fetch = portalFetch;
void bootstrapPortal({ element: mount, fetcher: portalFetch });
