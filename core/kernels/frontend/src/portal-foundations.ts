import { contractSatisfies } from "./contract-version";
import { PortalAssemblyError } from "./portal-errors";
import type {
  FrontendPluginLoader,
  FrontendPluginModule,
  PluginRef,
  PortalPrepareOptions,
  PortalSpec,
} from "./portal-contracts";
import { prepareRuntimeEngine } from "./runtime-engine";
import {
  assertTrustedFirstParty,
  moduleKey,
  requiredCapabilities,
  samePlugin,
  validRenderAdapterConfig,
  validRendererCatalog,
  validShellConfig,
  validShellTemplateCatalog,
  validThemeTemplateCatalog,
} from "./portal-validation";

export interface LoadedPortalFoundations {
  runtimeEngine: Awaited<ReturnType<typeof prepareRuntimeEngine>>;
  renderAdapterCatalog: NonNullable<FrontendPluginModule["renderAdapter"]>;
  renderer: NonNullable<FrontendPluginModule["renderer"]>;
  shell: NonNullable<FrontendPluginModule["shell"]>;
  shellLibrary: NonNullable<FrontendPluginModule["shellLibrary"]>;
  loaded: readonly { ref: PluginRef; module: FrontendPluginModule }[];
  rendererModuleKeys: ReadonlySet<string>;
  shellLibraryModuleKeys: ReadonlySet<string>;
}

export async function loadPortalFoundations(
  loader: FrontendPluginLoader,
  portal: PortalSpec,
  options: PortalPrepareOptions,
): Promise<LoadedPortalFoundations> {
  const runtimeEngineModule = await loader.load(portal.runtimeEngine);
  const runtimeEngine = await prepareRuntimeEngine({ load: async () => runtimeEngineModule }, portal.runtimeEngine);

  const renderAdapterModule = await loader.load(portal.renderAdapter);
  assertTrustedFirstParty(renderAdapterModule, portal.renderAdapter.id);
  const renderAdapter = renderAdapterModule.renderAdapter;
  if (renderAdapter === undefined) throw new PortalAssemblyError("DESIGN_SYSTEM_MISSING", "指定插件没有 ui.render.adapter 贡献");
  if (renderAdapter.id !== "ui.render.adapter") throw new PortalAssemblyError("DESIGN_SYSTEM_INVALID", "设计系统贡献 ID 必须为 ui.render.adapter");
  if (!contractSatisfies(renderAdapter.uiContract, portal.renderAdapter.uiContract)) throw new PortalAssemblyError("UI_CONTRACT_INCOMPATIBLE", "设计系统与 Portal 的 UI 契约不兼容");
  if (!validRendererCatalog(renderAdapter) || !validRenderAdapterConfig(portal.renderAdapter.config, renderAdapter)) {
    throw new PortalAssemblyError("DESIGN_SYSTEM_RENDERER_CATALOG_INVALID", "渲染适配器目录或 Platform Profile 配置无效");
  }

  const rendererID = options.rendererID !== undefined && portal.renderAdapter.config.userSelectable && portal.renderAdapter.config.allowedRenderers.includes(options.rendererID)
    ? options.rendererID : portal.renderAdapter.config.defaultRenderer;
  const rendererTemplate = renderAdapter.renderers.find((renderer) => renderer.id === rendererID);
  if (rendererTemplate === undefined) throw new PortalAssemblyError("DESIGN_SYSTEM_RENDERER_INVALID", `渲染适配器不支持 Renderer: ${rendererID}`);
  if (!portal.plugins.some((candidate) => samePlugin(candidate, rendererTemplate.module))) {
    throw new PortalAssemblyError("DESIGN_SYSTEM_RENDERER_MODULE_MISSING", `Renderer 模块未包含在 Portal 解析锁中: ${rendererID}`);
  }
  if (portal.resolution.pluginOrigins[rendererTemplate.module.id] !== "platform-profile") {
    throw new PortalAssemblyError("DESIGN_SYSTEM_RENDERER_ORIGIN", `Renderer 模块必须来自 Platform Profile: ${rendererID}`);
  }
  const rendererModule = await loader.load(rendererTemplate.module);
  assertTrustedFirstParty(rendererModule, rendererTemplate.module.id);
  const renderer = rendererModule.renderer;
  if (renderer === undefined || renderer.id !== rendererTemplate.id || renderer.framework !== rendererTemplate.framework) {
    throw new PortalAssemblyError("DESIGN_SYSTEM_RENDERER_INVALID", `Renderer 模块与 Adapter 目录不匹配: ${rendererID}`);
  }
  const capabilities = new Set(renderer.capabilities);
  if (!requiredCapabilities.every((capability) => capabilities.has(capability)) || !validThemeTemplateCatalog(renderer)) {
    throw new PortalAssemblyError("DESIGN_SYSTEM_INCOMPLETE", "选定 Renderer 未实现 Portal 所需的全部 UI 能力或主题目录无效");
  }
  const configuredThemeTemplate = portal.renderAdapter.config.rendererOptions?.[rendererID]?.themeTemplate;
  const declaredThemeTemplates = new Set(renderer.themeTemplates.map((template) => template.id));
  if (!declaredThemeTemplates.has(renderer.defaultThemeTemplate) || (configuredThemeTemplate !== undefined && !declaredThemeTemplates.has(configuredThemeTemplate))) {
    throw new PortalAssemblyError("DESIGN_SYSTEM_THEME_TEMPLATE_INVALID", `Renderer 不支持主题模板: ${configuredThemeTemplate ?? renderer.defaultThemeTemplate}`);
  }

  const shellModule = await loader.load(portal.shell);
  assertTrustedFirstParty(shellModule, portal.shell.id);
  const shell = shellModule.shell;
  if (shell?.id !== "ui.structure.shell" || typeof shell.compose !== "function" || !contractSatisfies(shell.uiContract, portal.shell.uiContract)) {
    throw new PortalAssemblyError("SHELL_INVALID", "Shell Catalog 缺失或 UI 契约不兼容");
  }
  if (!validShellTemplateCatalog(shell) || !validShellConfig(portal.shell.config, shell)) {
    throw new PortalAssemblyError("SHELL_TEMPLATE_INVALID", "Shell Library 目录或 Platform Profile 配置无效");
  }
  const shellTemplateID = options.shellTemplateID !== undefined && portal.shell.config.userSelectable && portal.shell.config.allowedTemplates.includes(options.shellTemplateID)
    ? options.shellTemplateID : portal.shell.config.defaultTemplate;
  const shellTemplate = shell.templates.find((template) => template.id === shellTemplateID);
  if (shellTemplate === undefined) throw new PortalAssemblyError("SHELL_LIBRARY_MISSING", `Shell Library 不在目录中: ${shellTemplateID}`);
  assertLockedPlatformModule(portal, shellTemplate.module, "SHELL_LIBRARY_MISSING", `Shell Library 未包含在 Platform Profile 解析锁中: ${shellTemplateID}`);
  const shellLibraryModule = await loader.load(shellTemplate.module);
  assertTrustedFirstParty(shellLibraryModule, shellTemplate.module.id);
  const shellLibrary = shellLibraryModule.shellLibrary;
  if (shellLibrary === undefined || shellLibrary.id !== shellTemplateID || shellLibrary.shell !== shell.id || typeof shellLibrary.Shell !== "function" || !contractSatisfies(shellLibrary.uiContract, portal.shell.uiContract)) {
    throw new PortalAssemblyError("SHELL_LIBRARY_INVALID", `Shell Library 导出与 Catalog 不一致: ${shellTemplateID}`);
  }

  const rendererModuleKeys = new Set(renderAdapter.renderers.map((item) => moduleKey(item.module)));
  const shellLibraryModuleKeys = new Set(shell.templates.map((item) => moduleKey(item.module)));
  const otherRefs = portal.plugins.filter((ref) =>
    !samePlugin(ref, portal.runtimeEngine) && !samePlugin(ref, portal.renderAdapter) && !samePlugin(ref, portal.shell) &&
    !rendererModuleKeys.has(moduleKey(ref)) && !shellLibraryModuleKeys.has(moduleKey(ref)),
  );
  const otherLoaded = await Promise.all(otherRefs.map(async (ref) => ({ ref, module: await loader.load(ref) })));
  return {
    runtimeEngine,
    renderAdapterCatalog: renderAdapter,
    renderer,
    shell,
    shellLibrary,
    rendererModuleKeys,
    shellLibraryModuleKeys,
    loaded: [
      { ref: portal.runtimeEngine, module: runtimeEngineModule },
      { ref: portal.renderAdapter, module: renderAdapterModule },
      { ref: rendererTemplate.module, module: rendererModule },
      { ref: portal.shell, module: shellModule },
      { ref: shellTemplate.module, module: shellLibraryModule },
      ...otherLoaded,
    ],
  };
}

function assertLockedPlatformModule(portal: PortalSpec, ref: PluginRef, code: string, message: string): void {
  if (!portal.plugins.some((candidate) => samePlugin(candidate, ref))) throw new PortalAssemblyError(code, message);
  if (portal.resolution.pluginOrigins[ref.id] !== "platform-profile") throw new PortalAssemblyError(code, message);
}
