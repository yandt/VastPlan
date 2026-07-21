export const frontendEngineContractVersion = "1.0.0" as const;

export type FrontendEngineCapability =
  | "csr"
  | "ssr"
  | "hydration"
  | "generation"
  | "lazy-module"
  | "i18n";

/**
 * Framework-neutral identity exported by a trusted Runtime Engine module.
 * Framework objects and mount handles remain inside the selected engine.
 */
export interface FrontendRuntimeEngine {
  readonly id: "ui.runtime.engine";
  readonly family: string;
  readonly engineContract: string;
  readonly capabilities: readonly FrontendEngineCapability[];
}

export function validateFrontendRuntimeEngine(value: unknown): FrontendRuntimeEngine {
  if (typeof value !== "object" || value === null) throw new Error("Runtime Engine 导出必须是对象");
  const engine = value as Partial<FrontendRuntimeEngine>;
  if (engine.id !== "ui.runtime.engine" || typeof engine.family !== "string" || !/^[a-z][a-z0-9-]{0,63}$/.test(engine.family) ||
      typeof engine.engineContract !== "string" || !Array.isArray(engine.capabilities) ||
      !engine.capabilities.includes("csr") || !engine.capabilities.includes("generation")) {
    throw new Error("Runtime Engine 导出与 engine contract 不兼容");
  }
  const supported = new Set<FrontendEngineCapability>(["csr", "ssr", "hydration", "generation", "lazy-module", "i18n"]);
  if (engine.capabilities.some((capability) => !supported.has(capability))) throw new Error("Runtime Engine 包含未知能力");
  return Object.freeze({ id: "ui.runtime.engine", family: engine.family, engineContract: engine.engineContract, capabilities: Object.freeze([...engine.capabilities]) });
}
