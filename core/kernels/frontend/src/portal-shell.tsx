import { useEffect, useMemo, useState } from "react";
import { createRoot, type Root } from "react-dom/client";
import { usePortalUI } from "@vastplan/portal-ui";
import { VerifiedFrontendPluginLoader, parsePortalRuntimeSpec, type ModuleFetcher, type PortalRuntimeSpec } from "./module-loader";
import { PortalRuntime, type PreparedPortal } from "./portal-runtime";

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
  root.render(<PortalStarting />);
  try {
    const prepared = await preparePortal(fetcher, endpoint, pathname);
    root.render(<PortalApplication prepared={prepared} initialPath={pathname} />);
  } catch (error) {
    const recover = async () => {
      const prepared = await preparePortal(fetcher, recoveryEndpoint, pathname);
      root.render(<PortalApplication prepared={prepared} initialPath={pathname} recoveryMode />);
    };
    root.render(<PortalRecovery error={error} onRecover={recover} />);
  }
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

export function PortalApplication({ prepared, initialPath, recoveryMode = false }: { prepared: PreparedPortal; initialPath: string; recoveryMode?: boolean }) {
  const [pathname, setPathname] = useState(initialPath);
  useEffect(() => {
    const onPopState = () => setPathname(globalThis.location?.pathname ?? "/");
    globalThis.addEventListener?.("popstate", onPopState);
    return () => globalThis.removeEventListener?.("popstate", onPopState);
  }, []);
  const page = useMemo(() => selectPage(prepared, pathname), [prepared, pathname]);
  const Provider = prepared.designSystem.Provider;
  return <Provider><PortalContent prepared={prepared} pathname={pathname} onNavigate={setPathname} page={page} recoveryMode={recoveryMode} /></Provider>;
}

function PortalContent({ prepared, pathname, onNavigate, page, recoveryMode }: {
  prepared: PreparedPortal;
  pathname: string;
  onNavigate(path: string): void;
  page: PreparedPortal["pages"][number] | undefined;
  recoveryMode: boolean;
}) {
  const ui = usePortalUI();
  const composition = prepared.composition.compose({
    pages: prepared.pages,
    shellContributions: prepared.shellContributions,
    activePageID: page?.id,
    config: prepared.portal.composition.config,
  });
  const Layout = prepared.layout.Shell;
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
    config={prepared.portal.layout.config ?? {}}
    pathname={pathname}
    onNavigate={navigate}
    recoveryNotice={recoveryMode ? <ui.Status tone="warning">正在运行上一条仍可信的已发布 revision #{prepared.portal.revision}。</ui.Status> : undefined}
  />;
}

export function PortalStarting() {
  return <main aria-busy="true" style={{ fontFamily: "system-ui", minHeight: "100vh", display: "grid", placeItems: "center", background: "#f7f8fa", color: "#4e5969" }}>
    <div><strong>VastPlan</strong><p>正在验证并装配平台模块…</p></div>
  </main>;
}

export function PortalRecovery({ error, onRecover }: { error: unknown; onRecover?(): Promise<void> }) {
  const message = error instanceof Error ? error.message : "未知启动错误";
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
      setRecoveryError(cause instanceof Error ? cause.message : "安全恢复版本无法启动");
      setRecovering(false);
    }
  };
  return <main role="alert" data-vastplan-portal-recovery style={{ fontFamily: "system-ui", maxWidth: 720, margin: "10vh auto", padding: 32, border: "1px solid #e5e6eb", borderRadius: 12, background: "#fff" }}>
    <p style={{ color: "#c9cdd4", fontWeight: 600, letterSpacing: 1 }}>VASTPLAN SAFE MODE</p>
    <h1>Portal 无法安全启动</h1>
    <p>{message}</p>
    <p><code>{code}</code></p>
    {recoveryError === undefined ? null : <p style={{ color: "#cb2634" }}>{recoveryError}</p>}
    <div style={{ display: "flex", gap: 12, flexWrap: "wrap" }}>
      <button type="button" onClick={() => globalThis.location?.reload()}>重试当前版本</button>
      {onRecover === undefined ? null : <button type="button" disabled={recovering} onClick={() => void recover()}>{recovering ? "正在验证…" : "启动上一安全版本"}</button>}
    </div>
  </main>;
}

function selectPage(prepared: PreparedPortal, pathname: string) {
  return [...prepared.pages]
    .filter((page) => pathname === page.path || pathname.startsWith(`${page.path.replace(/\/$/, "")}/`))
    .sort((left, right) => right.path.length - left.path.length)[0];
}

export class PortalBootstrapError extends Error {
  public constructor(public readonly code: string, message: string) {
    super(message);
    this.name = "PortalBootstrapError";
  }
}
