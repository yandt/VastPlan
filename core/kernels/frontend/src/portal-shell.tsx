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
}

/** Fetches the governed runtime lock, verifies every remote module, then mounts. */
export async function bootstrapPortal(options: PortalBootstrapOptions): Promise<Root> {
  const fetcher = options.fetcher ?? globalThis.fetch.bind(globalThis);
  const pathname = options.pathname ?? globalThis.location?.pathname ?? "/";
  const endpoint = options.runtimeEndpoint ?? "/v1/portal-runtime";
  const root = createRoot(options.element);
  try {
    const spec = await fetchRuntimeSpec(fetcher, endpoint, pathname);
    const loader = new VerifiedFrontendPluginLoader(spec.modules, fetcher);
    const prepared = await new PortalRuntime(loader).prepare(spec.portal);
    root.render(<PortalApplication prepared={prepared} initialPath={pathname} />);
  } catch (error) {
    root.render(<PortalRecovery error={error} />);
  }
  return root;
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

export function PortalApplication({ prepared, initialPath }: { prepared: PreparedPortal; initialPath: string }) {
  const [pathname, setPathname] = useState(initialPath);
  useEffect(() => {
    const onPopState = () => setPathname(globalThis.location?.pathname ?? "/");
    globalThis.addEventListener?.("popstate", onPopState);
    return () => globalThis.removeEventListener?.("popstate", onPopState);
  }, []);
  const route = useMemo(() => selectRoute(prepared, pathname), [prepared, pathname]);
  const Provider = prepared.designSystem.Provider;
  return <Provider><PortalContent prepared={prepared} pathname={pathname} onNavigate={setPathname} route={route} /></Provider>;
}

function PortalContent({ prepared, pathname, onNavigate, route }: {
  prepared: PreparedPortal;
  pathname: string;
  onNavigate(path: string): void;
  route: PreparedPortal["routes"][number] | undefined;
}) {
  const ui = usePortalUI();
  const View = route?.component;
  const menuItems = prepared.menus.map((item) => ({ id: item.id, label: item.title, href: item.route }));
  const activeMenu = prepared.menus.find((item) => item.route === route?.path)?.id;
  const navigate = (id: string) => {
    const item = prepared.menus.find((candidate) => candidate.id === id);
    if (item === undefined) return;
    globalThis.history?.pushState({}, "", item.route);
    onNavigate(item.route);
  };
  return <ui.Page title={prepared.portal.id}>
    <ui.Menu items={menuItems} activeID={activeMenu} onSelect={navigate} />
    {View === undefined ? <ui.EmptyState title="页面不存在" description={`Portal 没有注册路径 ${pathname}`} /> : <View />}
  </ui.Page>;
}

function PortalRecovery({ error }: { error: unknown }) {
  const message = error instanceof Error ? error.message : "未知启动错误";
  return <main role="alert" style={{ fontFamily: "system-ui", maxWidth: 720, margin: "10vh auto", padding: 24 }}>
    <h1>Portal 无法安全启动</h1>
    <p>{message}</p>
    <button type="button" onClick={() => globalThis.location?.reload()}>重试</button>
  </main>;
}

function selectRoute(prepared: PreparedPortal, pathname: string) {
  return [...prepared.routes]
    .filter((route) => pathname === route.path || pathname.startsWith(`${route.path.replace(/\/$/, "")}/`))
    .sort((left, right) => right.path.length - left.path.length)[0];
}

export class PortalBootstrapError extends Error {
  public constructor(public readonly code: string, message: string) {
    super(message);
    this.name = "PortalBootstrapError";
  }
}
