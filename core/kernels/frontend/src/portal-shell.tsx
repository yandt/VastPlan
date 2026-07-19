import { useEffect, useMemo, useState } from "react";
import { createRoot, type Root } from "react-dom/client";
import { PortalI18nProvider, message, usePortalI18n, usePortalUI, type PluginLocalization, type PortalLocalizationPolicy } from "@vastplan/ui-primitives";
import { VerifiedFrontendPluginLoader, parsePortalRuntimeSpec, type ModuleFetcher, type PortalRuntimeSpec } from "./module-loader";
import { startPortalDevelopmentUpdates } from "./portal-development";
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
  let stopDevelopmentUpdates: (() => void) | undefined;
  const renderApplication = () => {
    if (prepared !== undefined) root.render(<PortalApplication prepared={prepared} initialPath={pathname} recoveryMode={recoveryMode} developmentError={developmentError} />);
  };
  const manager = new PortalGenerationManager({
    fetcher,
    descriptorPolicy: __VASTPLAN_DEV_HMR__ ? "development" : "production",
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
  root.render(<PortalStarting />);
  try {
    const spec = await fetchRuntimeSpec(fetcher, endpoint, pathname);
    await manager.start(spec);
    if (__VASTPLAN_DEV_HMR__) {
      stopDevelopmentUpdates = startPortalDevelopmentUpdates({
        manager,
        fetcher,
        pathname: () => globalThis.location?.pathname ?? pathname,
        onError: (error) => {
          developmentError = errorMessage(error);
          renderApplication();
        },
      });
    }
  } catch (error) {
    const recover = async () => {
      recoveryMode = true;
      const spec = await fetchRuntimeSpec(fetcher, recoveryEndpoint, pathname);
      await manager.start(spec);
    };
    root.render(<PortalRecovery error={error} onRecover={recover} />);
  }
  globalThis.addEventListener?.("pagehide", () => {
    stopDevelopmentUpdates?.();
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

export function PortalApplication({ prepared, initialPath, recoveryMode = false, developmentError }: { prepared: PreparedPortal; initialPath: string; recoveryMode?: boolean; developmentError?: string }) {
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
    <LocalizedPortalApplication prepared={prepared} pathname={pathname} onNavigate={setPathname} page={page} recoveryMode={recoveryMode} developmentError={developmentError} />
  </PortalI18nProvider>;
}

function LocalizedPortalApplication({ prepared, pathname, onNavigate, page, recoveryMode, developmentError }: { prepared: PreparedPortal; pathname: string; onNavigate(path: string): void; page: PreparedPortal["pages"][number] | undefined; recoveryMode: boolean; developmentError?: string }) {
  const Provider = prepared.renderAdapter.Provider;
  const i18n = usePortalI18n();
  return <Provider locale={i18n.locale} direction={i18n.direction}>
    <PortalContent prepared={prepared} pathname={pathname} onNavigate={onNavigate} page={page} recoveryMode={recoveryMode} />
    {developmentError === undefined ? null : <PortalDevelopmentNotice message={developmentError} />}
  </Provider>;
}

function PortalDevelopmentNotice({ message }: { message: string }) {
  const i18n = usePortalI18n();
  return <aside role="status" data-vastplan-development-error style={{ position: "fixed", right: 16, bottom: 16, zIndex: 2147483647, maxWidth: 520, padding: "12px 16px", borderRadius: 8, background: "#3b1219", color: "#fff", boxShadow: "0 8px 28px rgba(0,0,0,.28)", fontFamily: "system-ui" }}>
    <strong>{i18n.text(messageDescriptor("development.notCommitted", "插件热替换未提交"))}</strong>
    <div style={{ marginTop: 4, whiteSpace: "pre-wrap" }}>{message}</div>
  </aside>;
}

function PortalContent({ prepared, pathname, onNavigate, page, recoveryMode }: {
  prepared: PreparedPortal;
  pathname: string;
  onNavigate(path: string): void;
  page: PreparedPortal["pages"][number] | undefined;
  recoveryMode: boolean;
}) {
  const ui = usePortalUI();
  const i18n = usePortalI18n();
  const composition = prepared.structureComposition.compose({
    pages: prepared.pages,
    shellContributions: prepared.shellContributions,
    activePageID: page?.id,
    config: prepared.portal.structureComposition.config,
  });
  const Layout = prepared.structureLayout.Shell;
  const navigate = (pageID: string) => {
    const target = prepared.pages.find((candidate) => candidate.id === pageID);
    if (target === undefined) return;
    globalThis.history?.pushState({}, "", target.path);
    onNavigate(target.path);
  };
  const branding = prepared.portal.branding ?? {};
  return <Layout
    composition={composition}
    branding={{
      name: typeof branding.name === "string" && branding.name !== "" ? branding.name : typeof branding.title === "string" && branding.title !== "" ? branding.title : prepared.portal.id,
      shortName: typeof branding.shortName === "string" ? branding.shortName : undefined,
      logoURL: typeof branding.logoURL === "string" ? branding.logoURL : undefined,
    }}
    config={prepared.portal.structureLayout.config ?? {}}
    pathname={pathname}
    onNavigate={navigate}
    recoveryNotice={recoveryMode ? <ui.Status tone="warning">{i18n.text(message(kernelNamespace, "recovery.active", "正在运行上一条仍可信的已发布 revision #{revision}。", { revision: prepared.portal.revision }))}</ui.Status> : undefined}
  />;
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
    "zh-CN": { "recovery.active": "正在运行上一条仍可信的已发布 revision #{revision}。", "development.notCommitted": "插件热替换未提交" },
    "en-US": { "recovery.active": "Running the previous trusted published revision #{revision}.", "development.notCommitted": "Plugin hot update was not committed" },
  },
};

function messageDescriptor(key: string, fallback: string) { return message(kernelNamespace, key, fallback); }

function bootstrapText(zhCN: string, enUS: string): string {
  return globalThis.navigator?.language?.toLowerCase().startsWith("zh") === false ? enUS : zhCN;
}
