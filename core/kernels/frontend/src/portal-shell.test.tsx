import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import type { PreparedPortal } from "./portal-runtime";
import { PortalBootstrapError, PortalRecovery, createBootstrapRuntimeSource, fetchRuntimeSpec, resolvePortalPath } from "./portal-shell";
import { productionFrontendRuntimeProtocol } from "./frontend-runtime-protocol";

const runtimeDocument = (portal: Record<string, unknown>, modules: readonly Record<string, unknown>[]) => ({
  portal, modules,
  contributions: { schemaVersion: 1, generation: 1, inventoryDigest: "e".repeat(64), contributions: [], digest: "f".repeat(64) },
});

describe("Portal recovery shell", () => {
  it("renders without any design-system provider", () => {
    const html = renderToStaticMarkup(createElement(PortalRecovery, {
      error: new PortalBootstrapError("RUNTIME_FETCH_FAILED", "运行描述不可用"),
      onRecover: async () => undefined,
    }));
    expect(html).toContain("VASTPLAN SAFE MODE");
    expect(html).toMatch(/启动上一安全版本|Start previous safe version/);
    expect(html).toContain("RUNTIME_FETCH_FAILED");
    expect(html).toMatch(/内核恢复状态|Kernel recovery status/);
  });

  it("requests a server-governed recovery spec with the original path", async () => {
    const calls: string[] = [];
    const fetcher = async (input: string) => {
      calls.push(input);
      const digest = "a".repeat(64);
      return new Response(JSON.stringify(runtimeDocument({}, [{ id: "cn.vastplan.test", version: "1.0.0", entry: "frontend/dist/index.js", url: `/v1/portal-recovery-modules/8/7/${digest}.js`, sha256: digest, packageSha256: "b".repeat(64) }])), { status: 200 });
    };
    await fetchRuntimeSpec(fetcher, "/v1/portal-recovery", "/settings/portals", productionFrontendRuntimeProtocol);
    expect(calls).toEqual(["/v1/portal-recovery?path=%2Fsettings%2Fportals"]);
  });

  it("boots development directly from the source runtime overlay", async () => {
    const calls: string[] = [];
    const digest = "a".repeat(64);
    const fetcher = async (input: string) => {
      calls.push(input);
      return new Response(JSON.stringify(runtimeDocument({}, [{ id: "cn.vastplan.test", version: "1.0.0", entry: "frontend/dist/index.js", url: `/__vastplan_dev/modules/${digest}.js`, sha256: digest, packageSha256: "b".repeat(64) }])), { status: 200 });
    };
    const source = createBootstrapRuntimeSource(fetcher, "/v1/portal-runtime", "/__vastplan_dev/runtime", true);
    const spec = await source.read("/operations");
    expect(source.protocol.id).toBe("development");
    expect(calls).toEqual(["/__vastplan_dev/runtime?path=%2Foperations"]);
    expect(spec.modules[0]?.url).toBe(`/__vastplan_dev/modules/${digest}.js`);
  });

  it("keeps production bootstrap on the governed stable runtime", async () => {
    const calls: string[] = [];
    const digest = "c".repeat(64);
    const fetcher = async (input: string) => {
      calls.push(input);
      return new Response(JSON.stringify(runtimeDocument({}, [{ id: "cn.vastplan.test", version: "1.0.0", entry: "frontend/dist/index.js", url: `/v1/portal-modules/1/${digest}.js`, sha256: digest, packageSha256: "d".repeat(64) }])), { status: 200 });
    };
    const source = createBootstrapRuntimeSource(fetcher, "/v1/portal-runtime", "/__vastplan_dev/runtime", false);
    await source.read("/operations");
    expect(source.protocol.id).toBe("production");
    expect(calls).toEqual(["/v1/portal-runtime?path=%2Foperations"]);
  });
});

describe("Portal landing route", () => {
  const prepared = {
    portal: { route: "/operations" },
    navigationCatalogs: [{ pluginID: "cn.vastplan.test", nodes: [
      { id: "settings", ref: { pluginID: "cn.vastplan.test", nodeID: "settings" }, label: "设置", zone: "settings", icon: { kind: "semantic", name: "settings" } },
      { id: "main", ref: { pluginID: "cn.vastplan.test", nodeID: "main" }, label: "概览", zone: "primary", icon: { kind: "semantic", name: "menu" } },
    ] }],
    pages: [
      { id: "settings", path: "/operations/settings", navigation: { id: "settings", label: "设置", parentMenuRef: { pluginID: "cn.vastplan.test", nodeID: "settings" } } },
      { id: "dashboard", path: "/operations/dashboard", navigation: { id: "dashboard", label: "概览", parentMenuRef: { pluginID: "cn.vastplan.test", nodeID: "main" } } },
    ],
  } as unknown as PreparedPortal;

  it("将门户根路径稳定落到最高优先级导航页", () => {
    expect(resolvePortalPath(prepared, "/operations")).toBe("/operations/dashboard");
    expect(resolvePortalPath(prepared, "/operations/")).toBe("/operations/dashboard");
  });

  it("保留已注册页面和未知的门户内深层路径", () => {
    expect(resolvePortalPath(prepared, "/operations/settings")).toBe("/operations/settings");
    expect(resolvePortalPath(prepared, "/operations/not-found")).toBe("/operations/not-found");
  });
});
