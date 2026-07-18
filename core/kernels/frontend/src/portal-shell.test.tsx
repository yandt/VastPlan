import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { PortalBootstrapError, PortalRecovery, fetchRuntimeSpec } from "./portal-shell";

describe("Portal recovery shell", () => {
  it("renders without any design-system provider", () => {
    const html = renderToStaticMarkup(createElement(PortalRecovery, {
      error: new PortalBootstrapError("RUNTIME_FETCH_FAILED", "运行描述不可用"),
      onRecover: async () => undefined,
    }));
    expect(html).toContain("VASTPLAN SAFE MODE");
    expect(html).toContain("启动上一安全版本");
    expect(html).toContain("RUNTIME_FETCH_FAILED");
  });

  it("requests a server-governed recovery spec with the original path", async () => {
    const calls: string[] = [];
    const fetcher = async (input: string) => {
      calls.push(input);
      return new Response(JSON.stringify({ portal: {}, modules: [] }), { status: 200 });
    };
    await fetchRuntimeSpec(fetcher, "/v1/portal-recovery", "/settings/portals");
    expect(calls).toEqual(["/v1/portal-recovery?path=%2Fsettings%2Fportals"]);
  });
});
