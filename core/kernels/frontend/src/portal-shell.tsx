import { useEffect, useMemo, useState } from "react";
import { createRoot, type Root } from "react-dom/client";
import { PortalI18nProvider, message, usePortalI18n, usePortalUI, type PluginLocalization, type PortalLocalizationPolicy } from "@vastplan/ui-primitives";
import { VerifiedFrontendPluginLoader, parsePortalRuntimeSpec, type ModuleFetcher, type PortalRuntimeSpec } from "./module-loader";
import { startPortalDevelopmentUpdates } from "./portal-development";
import { startPortalActivationUpdates, type PortalActivationUpdate } from "./portal-updates";
import { PortalGenerationManager } from "./portal-generation";
import { PortalRuntime, type PreparedPortal } from "./portal-runtime";

declare const __VASTPLAN_DEV_HMR__: boolean;

const defaultPortalLocalization: PortalLocalizationPolicy = Object.freeze({ defaultLocale: "zh-CN", supportedLocales: Object.freeze(["zh-CN", "en-US"]) });

export interface PortalBootstrapOptions {
  element: Element;
  pathname?: string;
  fetcher?: ModuleFetcher;
  runtimeEndpoint?: string;
  recoveryEndpoint?: string;
}

/** Fetches the governed runtime lock, verifies every remote module, then mounts. */
export async function bootstrapPortal(options: PortalBootstrapOptions): Promise<Root> {
  const fetcher = options.fetcher ?? globalThis.fetch.bind(globalThis);
  const pathname = options.pathname ?? globalThis.location?.pathname ?? "/";
  const endpoint = options.runtimeEndpoint ?? "/v1/portal-runtime";
  const recoveryEndpoint = options.recoveryEndpoint ?? "/v1/portal-recovery";
  const root = createRoot(options.element);
  let prepared: PreparedPortal | undefined;
  let recoveryMode = false;
  let developmentError: string | undefined;
  let updateNotice: PortalActivationUpdate | undefined;
  let currentSpec: PortalRuntimeSpec | undefined;
  let replaceShellTemplate: (templateID: string) => Promise<void> = async () => undefined;
  let stopDevelopmentUpdates: (() => void) | undefined;
  let stopActivationUpdates: (() => void) | undefined;
  const renderApplication = () => {
    if (prepared !== undefined) root.render(<PortalApplication prepared={prepared} initialPath={pathname} recoveryMode={recoveryMode} developmentError={developmentError} updateNotice={updateNotice} onApplyUpdate={() => globalThis.location?.reload()} onShellTemplateChange={replaceShellTemplate} />);
  };
  const manager = new PortalGenerationManager({
    fetcher,
    descriptorPolicy: __VASTPLAN_DEV_HMR__ ? "development" : "production",
    prepare: async (spec, context) => {
      const loader = new VerifiedFrontendPluginLoader(spec.modules, fetcher, undefined, __VASTPLAN_DEV_HMR__ ? "development" : "production");
      return new PortalRuntime(loader).prepare(spec.portal, { ...context, rendererID: resolveRendererPreference(spec.portal), shellTemplateID: resolveShellTemplatePreference(spec.portal) });
    },
    onDiagnostic: (diagnostic) => {
      if (!__VASTPLAN_DEV_HMR__) return;
      developmentError = `热替换 ${diagnostic.phase} 阶段异常：${errorMessage(diagnostic.error)}`;
      renderApplication();
    },
  });
  manager.subscribe((generation) => {
    prepared = generation.prepared;
    developmentError = undefined;
    renderApplication();
  });
  replaceShellTemplate = async (templateID) => {
    if (prepared === undefined || currentSpec === undefined || templateID === prepared.shellLibrary.id || !prepared.portal.shell.config.userSelectable || !prepared.portal.shell.config.allowedTemplates.includes(templateID)) return;
    const storageKey = shellTemplateStorageKey(prepared.portal);
    const previous = resolveShellTemplatePreference(prepared.portal);
    try {
      globalThis.localStorage?.setItem(storageKey, templateID);
      await manager.replace(currentSpec);
    } catch (error) {
      try {
        if (previous === undefined) globalThis.localStorage?.removeItem(storageKey);
        else globalThis.localStorage?.setItem(storageKey, previous);
      } catch { /* browser privacy mode may deny persistence */ }
      developmentError = errorMessage(error);
      renderApplication();
    }
  };
  root.render(<PortalStarting />);
  try {
    currentSpec = await fetchRuntimeSpec(fetcher, endpoint, pathname);
    await manager.start(currentSpec);
    commitHostEpoch(currentSpec.portal);
    const updatePolicy = currentSpec.portal.updates?.mode ?? "refresh";
    if (updatePolicy !== "refresh") {
      stopActivationUpdates = startPortalActivationUpdates({
        manager,
        policy: updatePolicy,
        pathname: () => globalThis.location?.pathname ?? pathname,
        currentRevision: () => currentSpec?.portal.revision ?? 0,
        fetchRuntime: (path) => fetchRuntimeSpec(fetcher, endpoint, path),
        onRuntime: (spec) => { currentSpec = spec; updateNotice = undefined; },
        onNotify: (update) => { updateNotice = update; renderApplication(); },
        onHostEpoch: (revision) => { if (currentSpec !== undefined) markHostEpochPending(currentSpec.portal, revision); },
        onError: (error) => { developmentError = errorMessage(error); renderApplication(); },
      });
    }
    if (__VASTPLAN_DEV_HMR__) {
      stopDevelopmentUpdates = startPortalDevelopmentUpdates({
        manager,
        fetcher,
        pathname: () => globalThis.location?.pathname ?? pathname,
        onRuntime: (spec) => { currentSpec = spec; },
        onError: (error) => {
          developmentError = errorMessage(error);
          renderApplication();
        },
      });
    }
  } catch (error) {
    if (currentSpec !== undefined && failPendingHostEpoch(currentSpec.portal)) {
      try {
        recoveryMode = true;
        currentSpec = await fetchRuntimeSpec(fetcher, recoveryEndpoint, pathname);
        await manager.start(currentSpec);
        renderApplication();
      } catch (recoveryError) {
        root.render(<PortalRecovery error={recoveryError} />);
      }
    } else {
      const recover = async () => {
        recoveryMode = true;
        currentSpec = await fetchRuntimeSpec(fetcher, recoveryEndpoint, pathname);
        await manager.start(currentSpec);
      };
      root.render(<PortalRecovery error={error} onRecover={recover} />);
    }
  }
  globalThis.addEventListener?.("pagehide", () => {
    stopDevelopmentUpdates?.();
    stopActivationUpdates?.();
    void manager.shutdown();
  }, { once: true });
  return root;
}

export async function preparePortal(fetcher: ModuleFetcher, endpoint: string, pathname: string): Promise<PreparedPortal> {
  const spec = await fetchRuntimeSpec(fetcher, endpoint, pathname);
  const loader = new VerifiedFrontendPluginLoader(spec.modules, fetcher);
  return new PortalRuntime(loader).prepare(spec.portal);
}

export async function fetchRuntimeSpec(fetcher: ModuleFetcher, endpoint: string, pathname: string): Promise<PortalRuntimeSpec> {
  const separator = endpoint.includes("?") ? "&" : "?";
  const response = await fetcher(`${endpoint}${separator}path=${encodeURIComponent(pathname)}`, {
    credentials: "same-origin",
    cache: "no-store",
  });
  if (!response.ok) {
    throw new PortalBootstrapError("RUNTIME_FETCH_FAILED", `Portal 运行描述获取失败 (${response.status})`);
  }
  return parsePortalRuntimeSpec(await response.json());
}

export function PortalApplication({ prepared, initialPath, recoveryMode = false, developmentError, updateNotice, onApplyUpdate, onShellTemplateChange }: { prepared: PreparedPortal; initialPath: string; recoveryMode?: boolean; developmentError?: string; updateNotice?: PortalActivationUpdate; onApplyUpdate?(): void; onShellTemplateChange?(templateID: string): Promise<void> }) {
  const landingPath = useMemo(() => resolvePortalPath(prepared, initialPath), [prepared, initialPath]);
  const [pathname, setPathname] = useState(landingPath);
  useEffect(() => {
    const onPopState = () => setPathname(globalThis.location?.pathname ?? "/");
    globalThis.addEventListener?.("popstate", onPopState);
    return () => globalThis.removeEventListener?.("popstate", onPopState);
  }, []);
  useEffect(() => {
    if (landingPath !== initialPath) globalThis.history?.replaceState({}, "", landingPath);
  }, [initialPath, landingPath]);
  const page = useMemo(() => selectPage(prepared, pathname), [prepared, pathname]);
  const policy = prepared.portal.localization ?? defaultPortalLocalization;
  const catalogs = useMemo(() => ({ ...prepared.messageCatalogs, [kernelNamespace]: kernelLocalization }), [prepared.messageCatalogs]);
  return <PortalI18nProvider policy={policy} catalogs={catalogs} candidates={globalThis.navigator?.languages ?? []} storageKey={`vastplan.locale.${prepared.portal.tenantId}.${prepared.portal.id}`}>
    <LocalizedPortalApplication prepared={prepared} pathname={pathname} onNavigate={setPathname} page={page} recoveryMode={recoveryMode} developmentError={developmentError} updateNotice={updateNotice} onApplyUpdate={onApplyUpdate} onRendererChange={(rendererID) => changeRendererPreference(prepared, rendererID)} onShellTemplateChange={onShellTemplateChange} />
  </PortalI18nProvider>;
}

function LocalizedPortalApplication({ prepared, pathname, onNavigate, page, recoveryMode, developmentError, updateNotice, onApplyUpdate, onRendererChange, onShellTemplateChange }: { prepared: PreparedPortal; pathname: string; onNavigate(path: string): void; page: PreparedPortal["pages"][number] | undefined; recoveryMode: boolean; developmentError?: string; updateNotice?: PortalActivationUpdate; onApplyUpdate?(): void; onRendererChange(rendererID: string): void; onShellTemplateChange?(templateID: string): Promise<void> }) {
  const Provider = prepared.renderAdapter.Provider;
  const i18n = usePortalI18n();
  const themeTemplate = prepared.portal.renderAdapter.config.rendererOptions?.[prepared.renderAdapter.id]?.themeTemplate;
  return <Provider locale={i18n.locale} direction={i18n.direction} themeTemplate={themeTemplate}>
    <PortalContent prepared={prepared} pathname={pathname} onNavigate={onNavigate} page={page} recoveryMode={recoveryMode} onRendererChange={onRendererChange} onShellTemplateChange={onShellTemplateChange} />
    {developmentError === undefined ? null : <PortalDevelopmentNotice message={developmentError} />}
    {updateNotice === undefined ? null : <PortalUpdateNotice update={updateNotice} onApply={onApplyUpdate} />}
  </Provider>;
}

function PortalUpdateNotice({ update, onApply }: { update: PortalActivationUpdate; onApply?(): void }) {
  const i18n = usePortalI18n();
  return <aside role="status" data-vastplan-update-available style={{ position: "fixed", right: 16, bottom: 16, zIndex: 2147483646, maxWidth: 420, padding: "12px 16px", borderRadius: 8, background: "#17233d", color: "#fff", boxShadow: "0 8px 28px rgba(0,0,0,.24)", fontFamily: "system-ui" }}>
    <strong>{i18n.text(messageDescriptor("update.available", "Portal 新版本已就绪"))}</strong>
    <div style={{ marginTop: 4 }}>{i18n.text(message(kernelNamespace, "update.revision", "Activation #{revision}", { revision: update.activationId }))}</div>
    <button type="button" onClick={onApply} style={{ marginTop: 8 }}>{i18n.text(messageDescriptor("update.apply", "刷新并应用"))}</button>
  </aside>;
}

function PortalDevelopmentNotice({ message }: { message: string }) {
  const i18n = usePortalI18n();
  return <aside role="status" data-vastplan-development-error style={{ position: "fixed", right: 16, bottom: 16, zIndex: 2147483647, maxWidth: 520, padding: "12px 16px", borderRadius: 8, background: "#3b1219", color: "#fff", boxShadow: "0 8px 28px rgba(0,0,0,.28)", fontFamily: "system-ui" }}>
    <strong>{i18n.text(messageDescriptor("development.notCommitted", "插件热替换未提交"))}</strong>
    <div style={{ marginTop: 4, whiteSpace: "pre-wrap" }}>{message}</div>
  </aside>;
}

function PortalContent({ prepared, pathname, onNavigate, page, recoveryMode, onRendererChange, onShellTemplateChange }: {
  prepared: PreparedPortal;
  pathname: string;
  onNavigate(path: string): void;
  page: PreparedPortal["pages"][number] | undefined;
  recoveryMode: boolean;
  onRendererChange(rendererID: string): void;
  onShellTemplateChange?(templateID: string): Promise<void>;
}) {
  const ui = usePortalUI();
  const i18n = usePortalI18n();
  const composition = prepared.shell.compose({
    pages: prepared.pages,
    shellContributions: prepared.shellContributions,
    activePageID: page?.id,
    config: prepared.portal.shell.config,
  });
  const templateID = prepared.shellLibrary.id;
  const changeTemplate = (next: string) => {
    if (!prepared.portal.shell.config.userSelectable || !prepared.portal.shell.config.allowedTemplates.includes(next)) return;
    void onShellTemplateChange?.(next);
  };
  const navigate = (pageID: string) => {
    const target = prepared.pages.find((candidate) => candidate.id === pageID);
    if (target === undefined) return;
    globalThis.history?.pushState({}, "", target.path);
    onNavigate(target.path);
  };
  const branding = prepared.portal.branding ?? {};
  const Shell = prepared.shellLibrary.Shell;
  return <Shell
    composition={composition}
    template={{ id: templateID, options: prepared.portal.shell.config.templateOptions?.[templateID] ?? {} }}
    availableTemplates={prepared.portal.shell.config.userSelectable ? prepared.shell.templates.filter((template) => prepared.portal.shell.config.allowedTemplates.includes(template.id)) : []}
    onTemplateChange={prepared.portal.shell.config.userSelectable ? changeTemplate : undefined}
    renderers={prepared.portal.renderAdapter.config.userSelectable ? prepared.renderAdapterCatalog.renderers.filter((renderer) => prepared.portal.renderAdapter.config.allowedRenderers.includes(renderer.id)).map((renderer) => ({ id: renderer.id, label: renderer.label, framework: renderer.framework })) : []}
    renderer={{ id: prepared.renderAdapter.id, options: prepared.portal.renderAdapter.config.rendererOptions?.[prepared.renderAdapter.id] ?? {} }}
    onRendererChange={prepared.portal.renderAdapter.config.userSelectable ? onRendererChange : undefined}
    branding={{
      name: typeof branding.name === "string" && branding.name !== "" ? branding.name : typeof branding.title === "string" && branding.title !== "" ? branding.title : prepared.portal.id,
      shortName: typeof branding.shortName === "string" ? branding.shortName : undefined,
      logoURL: typeof branding.logoURL === "string" ? branding.logoURL : undefined,
    }}
    pathname={pathname}
    onNavigate={navigate}
    recoveryNotice={recoveryMode ? <ui.Status tone="warning">{i18n.text(message(kernelNamespace, "recovery.active", "正在运行上一条仍可信的已发布 revision #{revision}。", { revision: prepared.portal.revision }))}</ui.Status> : undefined}
  />;
}

function rendererStorageKey(portal: PreparedPortal["portal"]): string {
  return `vastplan.renderer.${portal.tenantId}.${portal.id}.${portal.renderAdapter.id}`;
}

function resolveRendererPreference(portal: PreparedPortal["portal"]): string | undefined {
  if (!portal.renderAdapter.config.userSelectable) return undefined;
  try {
    const saved = globalThis.localStorage?.getItem(rendererStorageKey(portal));
    return saved !== null && saved !== undefined && portal.renderAdapter.config.allowedRenderers.includes(saved) ? saved : undefined;
  } catch { return undefined; }
}

function changeRendererPreference(prepared: PreparedPortal, rendererID: string): void {
  const config = prepared.portal.renderAdapter.config;
  if (!config.userSelectable || !config.allowedRenderers.includes(rendererID) || rendererID === prepared.renderAdapter.id) return;
  try { globalThis.localStorage?.setItem(rendererStorageKey(prepared.portal), rendererID); } catch { /* privacy mode may deny persistence */ }
  globalThis.location?.reload();
}

interface HostEpochState { active?: number; lastKnownGood?: number; pending?: number; failed?: number; }

function hostEpochStorageKey(portal: PortalRuntimeSpec["portal"]): string {
  return `vastplan.host-epoch.${portal.tenantId}.${portal.id}`;
}

function readHostEpoch(portal: PortalRuntimeSpec["portal"]): HostEpochState {
  try {
    const raw = globalThis.localStorage?.getItem(hostEpochStorageKey(portal));
    if (raw === null || raw === undefined) return {};
    const value = JSON.parse(raw) as HostEpochState;
    return typeof value === "object" && value !== null ? value : {};
  } catch { return {}; }
}

function writeHostEpoch(portal: PortalRuntimeSpec["portal"], value: HostEpochState): void {
  try { globalThis.localStorage?.setItem(hostEpochStorageKey(portal), JSON.stringify(value)); } catch { /* privacy mode may deny persistence */ }
}

function markHostEpochPending(portal: PortalRuntimeSpec["portal"], revision: number): void {
  const state = readHostEpoch(portal);
  writeHostEpoch(portal, { active: state.active ?? portal.revision, lastKnownGood: state.active ?? portal.revision, pending: revision, failed: state.failed });
}

function commitHostEpoch(portal: PortalRuntimeSpec["portal"]): void {
  const state = readHostEpoch(portal);
  writeHostEpoch(portal, { active: portal.revision, lastKnownGood: portal.revision, failed: state.failed === portal.revision ? undefined : state.failed });
}

function failPendingHostEpoch(portal: PortalRuntimeSpec["portal"]): boolean {
  const state = readHostEpoch(portal);
  if (state.pending !== portal.revision) return false;
  writeHostEpoch(portal, { active: state.lastKnownGood, lastKnownGood: state.lastKnownGood, failed: portal.revision });
  return true;
}

function shellTemplateStorageKey(portal: PreparedPortal["portal"]): string {
  return `vastplan.shell-template.${portal.tenantId}.${portal.id}.${portal.shell.id}`;
}

function resolveShellTemplatePreference(portal: PreparedPortal["portal"]): string | undefined {
  const allowed = new Set(portal.shell.config.allowedTemplates);
  try {
    const saved = globalThis.localStorage?.getItem(shellTemplateStorageKey(portal));
    if (saved !== null && saved !== undefined && allowed.has(saved)) return saved;
  } catch { /* browser privacy mode may deny persistence */ }
  return undefined;
}

export function PortalStarting() {
  return <main aria-busy="true" style={{ fontFamily: "system-ui", minHeight: "100vh", display: "grid", placeItems: "center", background: "#f7f8fa", color: "#4e5969" }}>
    <div><strong>VastPlan</strong><p>{bootstrapText("正在验证并装配平台模块…", "Verifying and assembling platform modules…")}</p></div>
  </main>;
}

export function PortalRecovery({ error, onRecover }: { error: unknown; onRecover?(): Promise<void> }) {
  const errorText = error instanceof Error ? error.message : bootstrapText("未知启动错误", "Unknown startup error");
  const code = error instanceof PortalBootstrapError ? error.code : error instanceof Error && "code" in error ? String(error.code) : "PORTAL_START_FAILED";
  const [recovering, setRecovering] = useState(false);
  const [recoveryError, setRecoveryError] = useState<string>();
  const recover = async () => {
    if (onRecover === undefined) return;
    setRecovering(true);
    setRecoveryError(undefined);
    try {
      await onRecover();
    } catch (cause) {
      setRecoveryError(cause instanceof Error ? cause.message : bootstrapText("安全恢复版本无法启动", "The safe recovery version could not start"));
      setRecovering(false);
    }
  };
  return <main role="alert" data-vastplan-portal-recovery style={{ fontFamily: "system-ui", maxWidth: 720, margin: "10vh auto", padding: 32, border: "1px solid #e5e6eb", borderRadius: 12, background: "#fff" }}>
    <p style={{ color: "#c9cdd4", fontWeight: 600, letterSpacing: 1 }}>VASTPLAN SAFE MODE</p>
    <h1>{bootstrapText("Portal 无法安全启动", "Portal could not start safely")}</h1>
    <p>{errorText}</p>
    <p><code>{code}</code></p>
    {recoveryError === undefined ? null : <p style={{ color: "#cb2634" }}>{recoveryError}</p>}
    <div style={{ display: "flex", gap: 12, flexWrap: "wrap" }}>
      <button type="button" onClick={() => globalThis.location?.reload()}>{bootstrapText("重试当前版本", "Retry current version")}</button>
      {onRecover === undefined ? null : <button type="button" disabled={recovering} onClick={() => void recover()}>{recovering ? bootstrapText("正在验证…", "Verifying…") : bootstrapText("启动上一安全版本", "Start previous safe version")}</button>}
    </div>
  </main>;
}

function selectPage(prepared: PreparedPortal, pathname: string) {
  return [...prepared.pages]
    .filter((page) => pathname === page.path || pathname.startsWith(`${page.path.replace(/\/$/, "")}/`))
    .sort((left, right) => right.path.length - left.path.length)[0];
}

/** Resolves only the Portal root to a deterministic landing page; unknown nested paths remain not-found. */
export function resolvePortalPath(prepared: PreparedPortal, pathname: string): string {
  if (selectPage(prepared, pathname) !== undefined) return pathname;
  const root = prepared.portal.route === "/" ? "/" : prepared.portal.route.replace(/\/$/, "");
  if (pathname !== root && (root === "/" || pathname !== `${root}/`)) return pathname;
  const zoneRank = { primary: 0, settings: 1, secondary: 2 } as const;
  const navigable = prepared.pages
    .map((page, index) => ({ page, index }))
    .filter(({ page }) => page.navigation !== undefined)
    .sort((left, right) => zoneRank[left.page.navigation!.zone] - zoneRank[right.page.navigation!.zone] || left.index - right.index);
  return navigable[0]?.page.path ?? prepared.pages[0]?.path ?? pathname;
}

export class PortalBootstrapError extends Error {
  public constructor(public readonly code: string, message: string) {
    super(message);
    this.name = "PortalBootstrapError";
  }
}

function errorMessage(error: unknown): string { return error instanceof Error ? error.message : String(error); }

const kernelNamespace = "cn.vastplan.kernel.frontend";
const kernelLocalization: PluginLocalization = {
  defaultLocale: "zh-CN",
  messages: {
    "zh-CN": { "recovery.active": "正在运行上一条仍可信的已发布 revision #{revision}。", "development.notCommitted": "插件热替换未提交", "update.available": "Portal 新版本已就绪", "update.revision": "Activation #{revision}", "update.apply": "刷新并应用" },
    "en-US": { "recovery.active": "Running the previous trusted published revision #{revision}.", "development.notCommitted": "Plugin hot update was not committed", "update.available": "A new Portal version is ready", "update.revision": "Activation #{revision}", "update.apply": "Refresh and apply" },
  },
};

function messageDescriptor(key: string, fallback: string) { return message(kernelNamespace, key, fallback); }

function bootstrapText(zhCN: string, enUS: string): string {
  return globalThis.navigator?.language?.toLowerCase().startsWith("zh") === false ? enUS : zhCN;
}
