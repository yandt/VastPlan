import type { FrontendRuntimeEngine } from "@vastplan/frontend-engine-contract";
import { contractSatisfies } from "./contract-version";
import { PortalAssemblyError } from "./portal-errors";
import type { FrontendPluginLoader, PluginRef } from "./portal-contracts";

export interface RuntimeEngineSelection extends PluginRef {
  engineContract: string;
  family: string;
}

/** Loads and validates the selected Foundation Engine before any visual plugin. */
export async function prepareRuntimeEngine(loader: FrontendPluginLoader, selection: RuntimeEngineSelection): Promise<FrontendRuntimeEngine> {
  const module = await loader.load(selection);
  if (!module.provenance.signed || !module.provenance.firstParty || !module.provenance.integrity) {
    throw new PortalAssemblyError("UNTRUSTED_RUNTIME_ENGINE", `拒绝加载未签名或非第一方 Runtime Engine: ${selection.id}`);
  }
  const engine = module.runtimeEngine;
  if (engine === undefined || engine.id !== "ui.runtime.engine" || engine.family !== selection.family ||
      !contractSatisfies(engine.engineContract, selection.engineContract) ||
      !engine.capabilities.includes("csr") || !engine.capabilities.includes("generation")) {
    throw new PortalAssemblyError("RUNTIME_ENGINE_INVALID", "Frontend Runtime Engine 缺失、能力不足或契约不兼容");
  }
  return engine;
}
