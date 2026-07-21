import type { FrontendRuntimeEngine } from "@vastplan/frontend-engine-contract";

export const runtimeEngine: FrontendRuntimeEngine = Object.freeze({
  id: "ui.runtime.engine",
  family: "react",
  engineContract: "1.0.0",
  capabilities: Object.freeze(["csr", "ssr", "hydration", "generation", "lazy-module", "i18n"] as const),
});

export const localization = Object.freeze({
  defaultLocale: "zh-CN",
  messages: Object.freeze({
    "zh-CN": Object.freeze({ "engine.react": "React 运行引擎" }),
    "en-US": Object.freeze({ "engine.react": "React Runtime Engine" }),
  }),
});

export default runtimeEngine;
