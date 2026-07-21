import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import type { FrontendServerRenderInput, FrontendServerRuntime } from "@vastplan/frontend-engine-contract";

const serverRuntime: FrontendServerRuntime = Object.freeze({
  id: "ui.runtime.engine.server",
  render(input: FrontendServerRenderInput) {
    const chinese = input.locale.toLowerCase().startsWith("zh");
    const html = renderToStaticMarkup(createElement("main", {
      "aria-busy": "true",
      style: { fontFamily: "system-ui", minHeight: "100vh", display: "grid", placeItems: "center", background: "#f7f8fa", color: "#4e5969" },
    }, createElement("div", null, createElement("strong", null, "VastPlan"), createElement("p", null,
      chinese ? "正在验证并装配平台模块…" : "Verifying and assembling platform modules…"))));
    return { html };
  },
});

export default serverRuntime;
