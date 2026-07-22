import type {
  FrontendPluginHotLifecycle,
  PluginLocalization,
  UIRenderAdapter,
  UIRenderer,
  UIShellAdapter,
  UIShellLibrary,
  UIWorkbenchAdapter,
} from "@vastplan/ui-primitives";
import { validateFrontendRuntimeEngine } from "@vastplan/frontend-engine-contract";
import type { FrontendPluginModule } from "./portal-contracts";
import { ModuleLoadError } from "./module-errors";

export interface ModuleExportIdentity {
  id: string;
  sha256: string;
}

/** Converts a framework module namespace into the narrow Portal plugin ABI. */
export function normalizeFrontendModule(namespace: unknown, identity: ModuleExportIdentity): FrontendPluginModule {
  if (!isRecord(namespace)) {
    throw new ModuleLoadError("MODULE_EXPORT_INVALID", `前端模块没有对象导出: ${identity.id}`);
  }
  const exported = isRecord(namespace.default) ? namespace.default : namespace;
  const provenance = { signed: true, firstParty: true, integrity: `sha256:${identity.sha256}` };
  const hot = normalizeHotLifecycle(exported.hot, identity.id);
  const localization = normalizeLocalizationExport(namespace.localization ?? exported.localization, identity.id);
  const runtimeEngineExport = isRecord(namespace.runtimeEngine) ? namespace.runtimeEngine : exported.id === "ui.runtime.engine" ? exported : undefined;
  if (runtimeEngineExport !== undefined) {
    try {
      return { provenance, runtimeEngine: validateFrontendRuntimeEngine(runtimeEngineExport), hot, localization };
    } catch (error) {
      throw new ModuleLoadError("RUNTIME_ENGINE_EXPORT_INVALID", `Runtime Engine 导出无效: ${identity.id}: ${String(error)}`);
    }
  }
  if (exported.id === "ui.render.adapter" && typeof exported.uiContract === "string" && typeof exported.defaultRenderer === "string" && Array.isArray(exported.renderers)) {
    return { provenance, renderAdapter: exported as unknown as UIRenderAdapter, hot, localization };
  }
  const renderer = isRecord(namespace.renderer) ? namespace.renderer : undefined;
  if (renderer !== undefined && typeof renderer.id === "string" && typeof renderer.framework === "string" && typeof renderer.Provider === "function" &&
      Array.isArray(renderer.capabilities) && Array.isArray(renderer.themeTemplates) && typeof renderer.defaultThemeTemplate === "string" &&
      Array.isArray(renderer.iconThemes) && typeof renderer.defaultIconTheme === "string") {
    return { provenance, renderer: renderer as unknown as UIRenderer, hot, localization };
  }
  if (exported.id === "ui.structure.shell" && typeof exported.compose === "function" && Array.isArray(exported.templates)) {
    return { provenance, shell: exported as unknown as UIShellAdapter, hot, localization };
  }
  const library = isRecord(namespace.shellLibrary) ? namespace.shellLibrary : exported;
  if (typeof library.id === "string" && library.shell === "ui.structure.shell" && typeof library.uiContract === "string" && typeof library.Shell === "function") {
    return { provenance, shellLibrary: library as unknown as UIShellLibrary, hot, localization };
  }
  if (exported.id === "ui.workflow.workbench" && typeof exported.CollectionPage === "function") {
    return { provenance, workbench: exported as unknown as UIWorkbenchAdapter, hot, localization };
  }
  if (typeof exported.register === "function") {
    return { provenance, register: exported.register.bind(exported) as FrontendPluginModule["register"], hot, localization };
  }
  throw new ModuleLoadError("MODULE_EXPORT_INVALID", `前端模块未导出 Runtime Engine、设计系统、Shell、Workbench 或 register: ${identity.id}`);
}

function normalizeLocalizationExport(value: unknown, pluginID: string): PluginLocalization | undefined {
  if (value === undefined) return undefined;
  if (!isRecord(value) || typeof value.defaultLocale !== "string" || !isRecord(value.messages)) {
    throw new ModuleLoadError("MODULE_LOCALIZATION_INVALID", `前端模块语言资源结构无效: ${pluginID}`);
  }
  return value as unknown as PluginLocalization;
}

function normalizeHotLifecycle(value: unknown, pluginID: string): FrontendPluginHotLifecycle | undefined {
  if (value === undefined) return undefined;
  if (!isRecord(value)) throw new ModuleLoadError("MODULE_HOT_INVALID", `前端模块热替换生命周期无效: ${pluginID}`);
  for (const name of ["capture", "restore", "dispose"] as const) {
    if (value[name] !== undefined && typeof value[name] !== "function") {
      throw new ModuleLoadError("MODULE_HOT_INVALID", `前端模块热替换钩子无效: ${pluginID}/${name}`);
    }
  }
  return Object.freeze({
    capture: typeof value.capture === "function" ? value.capture.bind(value) as FrontendPluginHotLifecycle["capture"] : undefined,
    restore: typeof value.restore === "function" ? value.restore.bind(value) as FrontendPluginHotLifecycle["restore"] : undefined,
    dispose: typeof value.dispose === "function" ? value.dispose.bind(value) as FrontendPluginHotLifecycle["dispose"] : undefined,
  });
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null;
}
