import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import type { PreparedPortal } from "./portal-runtime";
import { PortalBootstrapError, PortalRecovery, fetchRuntimeSpec, resolvePortalPath } from "./portal-shell";

describe("Portal recovery shell", () => {
  it("renders without any design-system provider", () => {
    const html = renderToStaticMarkup(createElement(PortalRecovery, {
      error: new PortalBootstrapError("RUNTIME_FETCH_FAILED", "运行描述不可用"),
      onRecover: async () => undefined,
    }));
    expect(html).toContain("VASTPLAN SAFE MODE");
    expect(html).toMatch(/启动上一安全版本|Start previous safe version/);
    expect(html).toContain("RUNTIME_FETCH_FAILED");
  });

  it("requests a server-governed recovery spec with the original path", async () => {
    const calls: string[] = [];
    const fetcher = async (input: string) => {
      calls.push(input);
      const digest = "a".repeat(64);
      return new Response(JSON.stringify({ portal: {}, modules: [{ id: "cn.vastplan.test", version: "1.0.0", entry: "frontend/dist/index.js", url: `/v1/portal-recovery-modules/8/7/${digest}.js`, sha256: digest, packageSha256: "b".repeat(64) }] }), { status: 200 });
    };
    await fetchRuntimeSpec(fetcher, "/v1/portal-recovery", "/settings/portals");
    expect(calls).toEqual(["/v1/portal-recovery?path=%2Fsettings%2Fportals"]);
  });
});

describe("Portal landing route", () => {
  const prepared = {
    portal: { route: "/operations" },
    pages: [
      { id: "settings", path: "/operations/settings", navigation: { id: "settings", label: "设置", zone: "settings" } },
      { id: "dashboard", path: "/operations/dashboard", navigation: { id: "dashboard", label: "概览", zone: "primary" } },
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
