import { useEffect, useMemo, useRef, useState } from "react";
import { createRoot, hydrateRoot, type Root } from "react-dom/client";
import { PortalI18nProvider, PortalPersonalizationProvider, message, resolveAppearanceColors, usePortalI18n, usePortalUI, type PluginLocalization, type PortalAppearanceSettings, type PortalLocalizationPolicy } from "@vastplan/ui-primitives";
import { VerifiedFrontendPluginLoader, type ModuleFetcher, type PortalRuntimeSpec } from "./module-loader";
import { parseRuntimeSpec } from "./module-runtime-spec";
import { fetchDevelopmentRuntime, startPortalDevelopmentUpdates } from "./portal-development";
import { startPortalActivationUpdates, type PortalActivationUpdate } from "./portal-updates";
import { PortalGenerationManager } from "./portal-generation";
import { PortalRuntime, type PreparedPortal } from "./portal-runtime";
import { AccessLoginPage } from "./access-login";
import { PortalPreferenceSession } from "./portal-preferences";
import { PortalAppearanceSession, resolveSystemScheme } from "./portal-appearance";
import { PortalGenerationCommitClient } from "./portal-generation-client";
import { logoutPortalSession, portalLogoutRedirect } from "./portal-logout";
import type { PortalRuntimeSource } from "./portal-runtime-source";
import { developmentFrontendRuntimeProtocol, productionFrontendRuntimeProtocol, type FrontendRuntimeProtocol } from "./frontend-runtime-protocol";
import { useKernelRecoveryStatus } from "./kernel-recovery";

declare const __VASTPLAN_DEV_HMR__: boolean;

const defaultPortalLocalization: PortalLocalizationPolicy = Object.freeze({ defaultLocale: "zh-CN", supportedLocales: Object.freeze(["zh-CN", "en-US"]) });

export interface PortalBootstrapOptions {
  element: Element;
  pathname?: string;
  fetcher?: ModuleFetcher;
  runtimeEndpoint?: string;
  developmentRuntimeEndpoint?: string;
  recoveryEndpoint?: string;
  runtimeSource?: PortalRuntimeSource;
}

/** Fetches the governed runtime lock, verifies every remote module, then mounts. */
export async function bootstrapPortal(options: PortalBootstrapOptions): Promise<Root> {
  const fetcher = options.fetcher ?? globalThis.fetch.bind(globalThis);
  const pathname = options.pathname ?? globalThis.location?.pathname ?? "/";
  const endpoint = options.runtimeEndpoint ?? "/v1/portal-runtime";
  const developmentEndpoint = options.developmentRuntimeEndpoint ?? "/__vastplan_dev/runtime";
  const recoveryEndpoint = options.recoveryEndpoint ?? "/v1/portal-recovery";
	const runtimeSource = options.runtimeSource ?? createBootstrapRuntimeSource(fetcher, endpoint, developmentEndpoint, __VASTPLAN_DEV_HMR__);
	const hydrated = options.element.hasChildNodes();
	const root = hydrated ? hydrateRoot(options.element, <PortalStarting />) : createRoot(options.element);
  if (pathname === "/auth/access") {
    root.render(<AccessLoginPage fetcher={fetcher} />);
    return root;
  }
  let prepared: PreparedPortal | undefined;
  let recoveryMode = false;
  let developmentError: string | undefined;
  let updateNotice: PortalActivationUpdate | undefined;
  let currentSpec: PortalRuntimeSpec | undefined;
  let preferenceSession: PortalPreferenceSession | undefined;
  let appearanceSession: PortalAppearanceSession | undefined;
  let replaceShellTemplate: (templateID: string) => Promise<void> = async () => undefined;
  let replaceRenderer: (rendererID: string) => void = () => undefined;
  let replaceIconTheme: (iconThemeID: string) => void = () => undefined;
  let replaceAppearance: (appearance: PortalAppearanceSettings) => void = () => undefined;
  let stopDevelopmentUpdates: (() => void) | undefined;
  let stopActivationUpdates: (() => void) | undefined;
  const renderApplication = () => {
    if (prepared !== undefined && appearanceSession !== undefined) root.render(<PortalApplication prepared={prepared} appearanceSession={appearanceSession} initialPath={pathname} recoveryMode={recoveryMode} developmentError={developmentError} updateNotice={updateNotice} onApplyUpdate={() => globalThis.location?.reload()} onRendererChange={replaceRenderer} onShellTemplateChange={replaceShellTemplate} onIconThemeChange={replaceIconTheme} onAppearanceChange={replaceAppearance} />);
  };
  const generationCommits = new PortalGenerationCommitClient(fetcher);
  const manager = new PortalGenerationManager({
    fetcher,
    runtimeProtocol: runtimeSource.protocol,
    prepare: async (spec, context) => {
      const loader = new VerifiedFrontendPluginLoader(spec, { protocol: runtimeSource.protocol, fetcher });
      const appearance = appearanceSession?.resolve(spec.portal) ?? {};
      return new PortalRuntime(loader).prepare(spec.portal, { ...context, ...appearance, preferences: preferenceSession, extensions: spec.extensions, contributions: spec.contributions });
    },
    beforeCommit: (spec) => generationCommits.commit(spec),
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
    if (prepared === undefined || currentSpec === undefined || appearanceSession === undefined || templateID === prepared.shellLibrary.id || !prepared.portal.shell.config.userSelectable || !prepared.portal.shell.config.allowedTemplates.includes(templateID)) return;
    const previous = prepared.shellLibrary.id;
    try {
      appearanceSession.setShellTemplate(templateID);
      await manager.replace(currentSpec);
    } catch (error) {
      appearanceSession.setShellTemplate(previous);
      await manager.replace(currentSpec);
      developmentError = errorMessage(error);
      renderApplication();
    }
  };
  replaceRenderer = (rendererID) => {
    if (prepared === undefined || appearanceSession === undefined) return;
    const config = prepared.portal.renderAdapter.config;
    if (!config.userSelectable || !config.allowedRenderers.includes(rendererID) || rendererID === prepared.renderAdapter.id) return;
    appearanceSession.setRenderer(rendererID);
    globalThis.location?.reload();
  };
  replaceIconTheme = (iconThemeID) => {
    if (prepared === undefined || appearanceSession === undefined) return;
    appearanceSession.setIconTheme(prepared.renderAdapter.id, iconThemeID);
    renderApplication();
  };
  replaceAppearance = (appearance) => {
    if (prepared === undefined || appearanceSession === undefined) return;
    appearanceSession.setAppearance(prepared.renderAdapter.id, appearance);
    renderApplication();
  };
	if (!hydrated) root.render(<PortalStarting />);
  try {
    currentSpec = await runtimeSource.read(pathname);
    appearanceSession = PortalAppearanceSession.open(currentSpec.portal);
    preferenceSession = await PortalPreferenceSession.open(fetcher, pathname, currentSpec.portal);
    await manager.start(currentSpec);
    appearanceSession.commitPendingRenderer();
    commitHostEpoch(currentSpec.portal);
    const updatePolicy = currentSpec.portal.updates?.mode ?? "refresh";
    if (updatePolicy !== "refresh") {
      stopActivationUpdates = startPortalActivationUpdates({
        manager,
        policy: updatePolicy,
        pathname: () => globalThis.location?.pathname ?? pathname,
        currentRevision: () => currentSpec?.portal.revision ?? 0,
        fetchRuntime: (path) => fetchRuntimeSpec(fetcher, endpoint, path, runtimeSource.protocol),
        onRuntime: (spec) => { currentSpec = spec; updateNotice = undefined; },
        onNotify: (update) => { updateNotice = update; renderApplication(); },
        onHostEpoch: (revision) => { if (currentSpec !== undefined) markHostEpochPending(currentSpec.portal, revision); },
        onError: (error) => { developmentError = errorMessage(error); renderApplication(); },
      });
    }
    if (__VASTPLAN_DEV_HMR__) {
      stopDevelopmentUpdates = startPortalDevelopmentUpdates({
        manager,
        runtimeSource,
        pathname: () => globalThis.location?.pathname ?? pathname,
        onRuntime: (spec) => { currentSpec = spec; },
        onError: (error) => {
          developmentError = errorMessage(error);
          renderApplication();
        },
      });
    }
  } catch (error) {
    if (currentSpec !== undefined && appearanceSession?.hasPendingRenderer()) {
      try {
        appearanceSession.discardPendingRenderer();
        await manager.start(currentSpec);
        commitHostEpoch(currentSpec.portal);
        renderApplication();
      } catch (recoveryError) {
        root.render(<PortalRecovery error={recoveryError} />);
      }
    } else if (currentSpec !== undefined && failPendingHostEpoch(currentSpec.portal)) {
      try {
        recoveryMode = true;
        currentSpec = await fetchRuntimeSpec(fetcher, recoveryEndpoint, pathname, runtimeSource.protocol);
        await manager.start(currentSpec);
        renderApplication();
      } catch (recoveryError) {
        root.render(<PortalRecovery error={recoveryError} />);
      }
    } else {
      const recover = async () => {
        recoveryMode = true;
        currentSpec = await fetchRuntimeSpec(fetcher, recoveryEndpoint, pathname, runtimeSource.protocol);
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

/** 组合根只选择一次 Runtime 协议，首次启动与后续替换共用该实例。 */
export function createBootstrapRuntimeSource(fetcher: ModuleFetcher, endpoint: string, developmentEndpoint: string, development: boolean): PortalRuntimeSource {
  const protocol = development ? developmentFrontendRuntimeProtocol : productionFrontendRuntimeProtocol;
  return development
    ? { protocol, read: (pathname) => fetchDevelopmentRuntime(fetcher, developmentEndpoint, pathname, protocol) }
    : { protocol, read: (pathname) => fetchRuntimeSpec(fetcher, endpoint, pathname, protocol) };
}

export async function fetchRuntimeSpec(fetcher: ModuleFetcher, endpoint: string, pathname: string, protocol: FrontendRuntimeProtocol): Promise<PortalRuntimeSpec> {
  const separator = endpoint.includes("?") ? "&" : "?";
  const response = await fetcher(`${endpoint}${separator}path=${encodeURIComponent(pathname)}`, {
    credentials: "same-origin",
    cache: "no-store",
  });
  if (!response.ok) {
    throw new PortalBootstrapError("RUNTIME_FETCH_FAILED", `Portal 运行描述获取失败 (${response.status})`);
  }
  return parseRuntimeSpec(await response.json(), protocol);
}

interface PortalApplicationProps {
  prepared: PreparedPortal;
  appearanceSession: PortalAppearanceSession;
  initialPath: string;
  recoveryMode?: boolean;
  developmentError?: string;
  updateNotice?: PortalActivationUpdate;
  onApplyUpdate?(): void;
  onRendererChange?(rendererID: string): void;
  onShellTemplateChange?(templateID: string): Promise<void>;
  onIconThemeChange?(iconThemeID: string): void;
  onAppearanceChange?(appearance: PortalAppearanceSettings): void;
}

export function PortalApplication({ prepared, appearanceSession, initialPath, recoveryMode = false, developmentError, updateNotice, onApplyUpdate, onRendererChange, onShellTemplateChange, onIconThemeChange, onAppearanceChange }: PortalApplicationProps) {
  const landingPath = useMemo(() => resolvePortalPath(prepared, initialPath), [prepared, initialPath]);
  const [pathname, setPathname] = useState(landingPath);
	const previousPrepared = useRef(prepared);
  useEffect(() => {
    const onPopState = () => setPathname(globalThis.location?.pathname ?? "/");
    globalThis.addEventListener?.("popstate", onPopState);
    return () => globalThis.removeEventListener?.("popstate", onPopState);
  }, []);
  useEffect(() => {
    if (landingPath !== initialPath) globalThis.history?.replaceState({}, "", landingPath);
  }, [initialPath, landingPath]);
	useEffect(() => {
		const previous = previousPrepared.current;
		previousPrepared.current = prepared;
		const fallback = resolveDeactivatedPagePath(previous, prepared, pathname);
		if (fallback === pathname) return;
		globalThis.history?.replaceState({}, "", fallback);
		setPathname(fallback);
	}, [prepared, pathname]);
  const policy = prepared.portal.localization ?? defaultPortalLocalization;
  const catalogs = useMemo(() => ({ ...prepared.messageCatalogs, [kernelNamespace]: kernelLocalization }), [prepared.messageCatalogs]);
  return <PortalI18nProvider policy={policy} catalogs={catalogs} candidates={globalThis.navigator?.languages ?? []} storageKey={`vastplan.locale.${prepared.portal.tenantId}.${prepared.portal.id}`}>
    <LocalizedPortalApplication prepared={prepared} appearanceSession={appearanceSession} pathname={pathname} onNavigate={setPathname} recoveryMode={recoveryMode} developmentError={developmentError} updateNotice={updateNotice} onApplyUpdate={onApplyUpdate} onRendererChange={onRendererChange ?? (() => undefined)} onShellTemplateChange={onShellTemplateChange} onIconThemeChange={onIconThemeChange} onAppearanceChange={onAppearanceChange} />
  </PortalI18nProvider>;
}

function LocalizedPortalApplication({ prepared, appearanceSession, pathname, onNavigate, recoveryMode, developmentError, updateNotice, onApplyUpdate, onRendererChange, onShellTemplateChange, onIconThemeChange, onAppearanceChange }: Omit<PortalApplicationProps, "initialPath"> & { pathname: string; onNavigate(path: string): void; recoveryMode: boolean }) {
  const Provider = prepared.renderAdapter.Provider;
  const i18n = usePortalI18n();
  const appearance = appearanceSession.appearance(prepared.renderAdapter.id);
  const systemScheme = useSystemScheme();
  const scheme = appearance.mode === "system" ? systemScheme : appearance.mode;
  const themeTemplateID = appearance[scheme].templateID;
  const themeColors = useMemo(() => resolveAppearanceColors(themeTemplateID, appearance[scheme].colors), [appearance, scheme, themeTemplateID]);
  const iconThemeID = appearanceSession.resolve(prepared.portal).iconThemeID ?? prepared.iconThemeID;
  return <Provider locale={i18n.locale} direction={i18n.direction} themeTemplate={themeTemplateID} themeColors={themeColors} iconTheme={iconThemeID}>
    <PortalContent prepared={prepared} appearance={appearance} themeTemplateID={themeTemplateID} iconThemeID={iconThemeID} pathname={pathname} onNavigate={onNavigate} recoveryMode={recoveryMode} onRendererChange={onRendererChange ?? (() => undefined)} onShellTemplateChange={onShellTemplateChange} onIconThemeChange={onIconThemeChange} onAppearanceChange={onAppearanceChange} />
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

function PortalContent({ prepared, appearance, themeTemplateID, iconThemeID, pathname, onNavigate, recoveryMode, onRendererChange, onShellTemplateChange, onIconThemeChange, onAppearanceChange }: {
  prepared: PreparedPortal;
  appearance: PortalAppearanceSettings;
  themeTemplateID: string;
  iconThemeID: string;
  pathname: string;
  onNavigate(path: string): void;
  recoveryMode: boolean;
  onRendererChange(rendererID: string): void;
  onShellTemplateChange?(templateID: string): Promise<void>;
  onIconThemeChange?(iconThemeID: string): void;
  onAppearanceChange?(appearance: PortalAppearanceSettings): void;
}) {
  const ui = usePortalUI();
  const i18n = usePortalI18n();
  const templateID = prepared.shellLibrary.id;
  const rendererOptions = prepared.portal.renderAdapter.config.rendererOptions?.[prepared.renderAdapter.id];
  const account = prepared.portal.account ?? { subjectID: "anonymous", tenantID: prepared.portal.tenantId, displayName: "User" };
  const availableTemplates = prepared.portal.shell.config.userSelectable ? prepared.shell.templates.filter((template) => prepared.portal.shell.config.allowedTemplates.includes(template.id)) : [];
  const renderers = prepared.portal.renderAdapter.config.userSelectable ? prepared.renderAdapterCatalog.renderers.filter((renderer) => prepared.portal.renderAdapter.config.allowedRenderers.includes(renderer.id)).map((renderer) => ({ id: renderer.id, label: renderer.label, framework: renderer.framework })) : [];
  const iconThemes = rendererOptions?.iconUserSelectable === true ? prepared.renderAdapter.iconThemes.filter((item) => rendererOptions.allowedIconThemes?.includes(item.id) === true) : [];
  const page = selectPage(prepared, pathname);
  const composition = prepared.shell.compose({
    pages: prepared.pages,
    shellContributions: prepared.shellContributions,
    navigationCatalogs: prepared.navigationCatalogs,
    accountNavigationOwnerID: prepared.portal.accountCenter?.id,
    activePageID: page?.id,
    config: prepared.portal.shell.config,
  });
  const changeTemplate = (next: string) => {
    if (!prepared.portal.shell.config.userSelectable || !prepared.portal.shell.config.allowedTemplates.includes(next)) return;
    void onShellTemplateChange?.(next);
  };
  const navigate = (pageID: string) => {
    const target = composition.pages.find((candidate) => candidate.id === pageID);
    if (target === undefined) return;
    globalThis.history?.pushState({}, "", target.path);
    onNavigate(target.path);
  };
  const branding = prepared.portal.branding ?? {};
  const Shell = prepared.shellLibrary.Shell;
  const logout = async () => {
    try {
      await logoutPortalSession(globalThis.fetch.bind(globalThis));
      globalThis.location?.assign(portalLogoutRedirect(pathname));
    } catch {
      ui.notify({ title: i18n.text(message(kernelNamespace, "logout.failed", "退出登录失败，请稍后重试。")), kind: "error" });
    }
  };
  const personalization = {
    account,
    appearance,
    availableTemplates,
    template: { id: templateID, options: prepared.portal.shell.config.templateOptions?.[templateID] ?? {} },
    onTemplateChange: prepared.portal.shell.config.userSelectable ? changeTemplate : undefined,
    renderers,
    renderer: { id: prepared.renderAdapter.id, options: prepared.portal.renderAdapter.config.rendererOptions?.[prepared.renderAdapter.id] ?? {} },
    onRendererChange: prepared.portal.renderAdapter.config.userSelectable ? onRendererChange : undefined,
    iconThemes,
    iconThemeID,
    onIconThemeChange: rendererOptions?.iconUserSelectable === true ? onIconThemeChange : undefined,
    onAppearanceChange,
  };
  return <PortalPersonalizationProvider value={personalization}><Shell
    composition={composition}
    template={{ id: templateID, options: prepared.portal.shell.config.templateOptions?.[templateID] ?? {} }}
    availableTemplates={availableTemplates}
    onTemplateChange={prepared.portal.shell.config.userSelectable ? changeTemplate : undefined}
    renderers={renderers}
    renderer={{ id: prepared.renderAdapter.id, options: prepared.portal.renderAdapter.config.rendererOptions?.[prepared.renderAdapter.id] ?? {} }}
    onRendererChange={prepared.portal.renderAdapter.config.userSelectable ? onRendererChange : undefined}
    themeTemplates={rendererOptions?.themeUserSelectable === true ? prepared.renderAdapter.themeTemplates.filter((item) => rendererOptions.allowedThemeTemplates?.includes(item.id) === true) : []}
    themeTemplateID={themeTemplateID}
    iconThemes={iconThemes}
    iconThemeID={iconThemeID}
    onIconThemeChange={rendererOptions?.iconUserSelectable === true ? onIconThemeChange : undefined}
    account={account}
    appearance={appearance}
    onAppearanceChange={onAppearanceChange}
    onLogout={logout}
    branding={{
      name: typeof branding.name === "string" && branding.name !== "" ? branding.name : typeof branding.title === "string" && branding.title !== "" ? branding.title : prepared.portal.id,
      shortName: typeof branding.shortName === "string" ? branding.shortName : undefined,
      logoURL: typeof branding.logoURL === "string" ? branding.logoURL : undefined,
    }}
    pathname={pathname}
    onNavigate={navigate}
    recoveryNotice={recoveryMode ? <ui.Status tone="warning">{i18n.text(message(kernelNamespace, "recovery.active", "正在运行上一条仍可信的已发布 revision #{revision}。", { revision: prepared.portal.revision }))}</ui.Status> : undefined}
  /></PortalPersonalizationProvider>;
}

function useSystemScheme(): "light" | "dark" {
  const [scheme, setScheme] = useState<"light" | "dark">(() => resolveSystemScheme("system"));
  useEffect(() => {
    const media = globalThis.matchMedia?.("(prefers-color-scheme: dark)");
    if (media === undefined) return;
    const update = () => setScheme(media.matches ? "dark" : "light");
    media.addEventListener("change", update);
    return () => media.removeEventListener("change", update);
  }, []);
  return scheme;
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
  const kernelRecovery = useKernelRecoveryStatus();
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
    <section aria-label={bootstrapText("内核恢复状态", "Kernel recovery status")} style={{ margin: "20px 0", padding: 16, borderRadius: 8, background: "#f7f8fa" }}>
      <strong>{bootstrapText("内核恢复状态", "Kernel recovery status")}</strong>
      {kernelRecovery.status === undefined ? <p>{kernelRecovery.unavailable ? bootstrapText("恢复状态暂不可用。", "Recovery status is currently unavailable.") : bootstrapText("正在读取恢复状态…", "Loading recovery status…")}</p> : <>
        <p>{kernelRecovery.status.overall} · {kernelRecovery.status.scope} · {bootstrapText(`${kernelRecovery.status.nodes} 个可信节点`, `${kernelRecovery.status.nodes} trusted node(s)`)}</p>
        <div style={{ display: "grid", gap: 8 }}>
          {kernelRecovery.status.stages.map((stage) => <div key={stage.id} style={{ display: "flex", justifyContent: "space-between", gap: 12 }}>
            <span>{stageLabel(stage.id)}</span><span>{stage.status} · {stage.ready}/{stage.required}</span>
          </div>)}
        </div>
      </>}
    </section>
    {recoveryError === undefined ? null : <p style={{ color: "#cb2634" }}>{recoveryError}</p>}
    <div style={{ display: "flex", gap: 12, flexWrap: "wrap" }}>
      <button type="button" onClick={() => globalThis.location?.reload()}>{bootstrapText("重试当前版本", "Retry current version")}</button>
      {onRecover === undefined ? null : <button type="button" disabled={recovering} onClick={() => void recover()}>{recovering ? bootstrapText("正在验证…", "Verifying…") : bootstrapText("启动上一安全版本", "Start previous safe version")}</button>}
    </div>
  </main>;
}

function stageLabel(id: string): string {
  if (id === "recovery") return bootstrapText("恢复基础", "Recovery foundation");
  if (id === "control-plane") return bootstrapText("控制面", "Control plane");
  if (id === "platform") return bootstrapText("完整平台", "Full platform");
  return id;
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
  const navigationZones = new Map(prepared.navigationCatalogs.flatMap((catalog) => catalog.nodes.map((node) => [`${node.ref.pluginID}/${node.ref.nodeID}`, node.zone] as const)));
  navigationZones.set("vastplan.host/account", "secondary");
  const navigable = prepared.pages
    .map((page, index) => ({ page, index }))
    .filter(({ page }) => page.navigation !== undefined)
    .sort((left, right) => {
      const leftRef = left.page.navigation!.parentMenuRef;
      const rightRef = right.page.navigation!.parentMenuRef;
      const leftZone = navigationZones.get(`${leftRef.pluginID}/${leftRef.nodeID}`) ?? "secondary";
      const rightZone = navigationZones.get(`${rightRef.pluginID}/${rightRef.nodeID}`) ?? "secondary";
      return zoneRank[leftZone] - zoneRank[rightZone] || left.index - right.index;
    });
  return navigable[0]?.page.path ?? prepared.pages[0]?.path ?? pathname;
}

/** Redirects only when the current path belonged to a page in the previous
 * Generation and that page disappeared. Unknown deep links remain not-found. */
export function resolveDeactivatedPagePath(previous: PreparedPortal, next: PreparedPortal, pathname: string): string {
	const previousPage = selectPage(previous, pathname);
	if (previousPage === undefined || selectPage(next, pathname) !== undefined) return pathname;
	const nextComposition = next.shell.compose({ pages: next.pages, shellContributions: next.shellContributions, navigationCatalogs: next.navigationCatalogs, accountNavigationOwnerID: next.portal.accountCenter?.id, config: next.portal.shell.config });
	const previousComposition = previous.shell.compose({ pages: previous.pages, shellContributions: previous.shellContributions, navigationCatalogs: previous.navigationCatalogs, accountNavigationOwnerID: previous.portal.accountCenter?.id, activePageID: previousPage.id, config: previous.portal.shell.config });
	const groupID = previousPage.navigation === undefined ? undefined : `${previousPage.navigation.parentMenuRef.pluginID}/${previousPage.navigation.parentMenuRef.nodeID}`;
	if (groupID !== undefined) {
		const sameGroup = orderedFallbackPages(next.pages.filter((page) => page.navigation !== undefined && `${page.navigation.parentMenuRef.pluginID}/${page.navigation.parentMenuRef.nodeID}` === groupID));
		const after = sameGroup.find((page) => comparePageNavigation(page, previousPage) > 0);
		if (after !== undefined) return after.path;
		if (sameGroup[0] !== undefined) return sameGroup[0].path;
	}
	const rootID = previousComposition.activeNavigationPath?.rootGroupID;
	if (rootID !== undefined) {
		for (const zone of ["primary", "settings", "secondary"] as const) {
			const root = nextComposition.navigation[zone].find((group) => group.id === rootID);
			const candidate = root === undefined ? undefined : [...root.pages, ...root.children.flatMap((child) => child.pages)][0];
			const page = candidate === undefined ? undefined : next.pages.find((item) => item.id === candidate.id);
			if (page !== undefined) return page.path;
		}
	}
	for (const zone of ["primary", "settings", "secondary"] as const) {
		for (const root of nextComposition.navigation[zone]) {
			const candidate = [...root.pages, ...root.children.flatMap((child) => child.pages)][0];
			const page = candidate === undefined ? undefined : next.pages.find((item) => item.id === candidate.id);
			if (page !== undefined) return page.path;
		}
	}
	return next.pages[0]?.path ?? pathname;
}

function orderedFallbackPages<T extends PreparedPortal["pages"][number]>(pages: readonly T[]): T[] {
	return [...pages].sort(comparePageNavigation);
}

function comparePageNavigation(left: PreparedPortal["pages"][number], right: PreparedPortal["pages"][number]): number {
	return (left.navigation?.order ?? 0) - (right.navigation?.order ?? 0) || left.id.localeCompare(right.id);
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
    "zh-CN": { "recovery.active": "正在运行上一条仍可信的已发布 revision #{revision}。", "development.notCommitted": "插件热替换未提交", "update.available": "Portal 新版本已就绪", "update.revision": "Activation #{revision}", "update.apply": "刷新并应用", "page.help": "页面帮助", "page.helpPending": "此页面暂未配置帮助内容。" },
    "en-US": { "recovery.active": "Running the previous trusted published revision #{revision}.", "development.notCommitted": "Plugin hot update was not committed", "update.available": "A new Portal version is ready", "update.revision": "Activation #{revision}", "update.apply": "Refresh and apply", "page.help": "Page help", "page.helpPending": "Help content has not been configured for this page yet." },
  },
};

function messageDescriptor(key: string, fallback: string) { return message(kernelNamespace, key, fallback); }

function bootstrapText(zhCN: string, enUS: string): string {
  return globalThis.navigator?.language?.toLowerCase().startsWith("zh") === false ? enUS : zhCN;
}
