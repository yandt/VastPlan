import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import type { FrontendServerRenderInput, FrontendServerRuntime } from "@vastplan/frontend-engine-contract";

const serverRuntime: FrontendServerRuntime = Object.freeze({
  id: "ui.runtime.engine.server",
  render(input: FrontendServerRenderInput) {
    const title = typeof input.branding.title === "string" ? input.branding.title : "VastPlan";
    const html = renderToStaticMarkup(createElement("div", {
      "data-vastplan-ssr-generation": String(input.generation),
      "data-vastplan-portal": input.portalId,
      "data-vastplan-path": input.path,
      lang: input.locale,
    }, createElement("strong", null, title), createElement("p", null, input.locale === "zh-CN" ? "正在装配平台模块…" : "Preparing platform modules…")));
    return { html };
  },
});

export default serverRuntime;
