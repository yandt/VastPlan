import { createServer } from "node:http";
import type { AddressInfo } from "node:net";
import { join } from "node:path";
import { describe, expect, it } from "vitest";
import { PortalAssets } from "../assets/portal-assets";
import type { PlatformControlBootstrapPort } from "../capabilities/platform-control-bootstrap-client";
import { FileIdentityProvider } from "../identity/file-identity-provider";
import { createPortalFixture } from "../testing/portal-fixture";
import { writeSessionFixture } from "../testing/session-fixture";
import { bootstrapPlatformControlPageContract, bootstrapPlatformControlPageContractHeader } from "./bootstrap-platform-control-page";
import { createPortalHandler } from "./portal-handler";

describe("Platform Control bootstrap surface", () => {
  it("redirects the seed root to an authenticated minimal page and keeps mutations CSRF protected", async () => {
    const calls: string[] = [];
    const payloads: unknown[] = [];
    let phase = "unconfigured";
    const client: PlatformControlBootstrapPort = { logicalService: "platform.database", async call(_principal, operation, payload) {
      calls.push(operation);
      payloads.push(JSON.parse(new TextDecoder().decode(payload)) as unknown);
      return new TextEncoder().encode(JSON.stringify({ phase, generation: phase === "ready" ? 1 : 0 }));
    } };
    const root = await createPortalFixture();
    const sessionFile = join(root, "sessions.json");
    await writeSessionFixture(sessionFile, "browser-token", new Date(Date.now() + 60_000), ["platform.database.read", "platform.database.probe", "platform.database.write"]);
    const identity = await FileIdentityProvider.open(sessionFile);
    const assets = await PortalAssets.load(root);
    const server = createServer(createPortalHandler({ assets, identity, platformControlBootstrap: client, secureCookies: false }));
    await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
    const origin = `http://127.0.0.1:${(server.address() as AddressInfo).port}`;
    const cookie = "vastplan_session=browser-token";
    try {
      const rootResponse = await fetch(`${origin}/operations`, { headers: { Cookie: cookie }, redirect: "manual" });
      expect(rootResponse.status).toBe(307);
      expect(rootResponse.headers.get("location")).toBe("/bootstrap/platform-control");
      const page = await fetch(`${origin}/bootstrap/platform-control`, { headers: { Cookie: cookie } });
      expect(page.status).toBe(200);
      expect(page.headers.get(bootstrapPlatformControlPageContractHeader)).toBe(bootstrapPlatformControlPageContract);
      const pageHTML = await page.text();
      expect(pageHTML).toContain("配置平台控制数据库");
      expect(pageHTML).toContain("ensureCurrentPage");
      expect(pageHTML).toContain("配置页面已更新，正在重新加载");
      expect(pageHTML).toContain("const request=payload();busy(true)");
      expect(pageHTML).toContain("mutate('/v1/bootstrap/platform-control/test','POST',request)");
      expect(pageHTML).not.toContain("body:JSON.stringify(payload())");
      const pageHead = await fetch(`${origin}/bootstrap/platform-control`, { method: "HEAD", headers: { Cookie: cookie } });
      expect(pageHead.status).toBe(200);
      expect(pageHead.headers.get(bootstrapPlatformControlPageContractHeader)).toBe(bootstrapPlatformControlPageContract);

      const csrfResponse = await fetch(`${origin}/v1/csrf`, { headers: { Cookie: cookie } });
      const token = (await csrfResponse.json() as { token: string }).token;
      const writeHeaders = { Cookie: `${cookie}; vastplan_csrf=${token}`, "X-VastPlan-CSRF": token, "Content-Type": "application/json" };
      const change = { profile: {}, expectedGeneration: 0, secretMaterial: "one-time-password" };
      const testResponse = await fetch(`${origin}/v1/bootstrap/platform-control/test`, { method: "POST", headers: writeHeaders, body: JSON.stringify(change) });
      expect(testResponse.status).toBe(200);
      expect(calls).toContain("platformControlTest");
      expect(payloads).toContainEqual(change);
      expect(await testResponse.text()).not.toContain("one-time-password");

      phase = "ready";
      const configure = await fetch(`${origin}/v1/bootstrap/platform-control`, { method: "PUT", headers: writeHeaders, body: JSON.stringify(change) });
      expect(configure.status).toBe(409);
      expect(await configure.json()).toEqual({ error: "platform_control_ready" });
      expect(calls).not.toContain("platformControlConfigure");
    } finally {
      await new Promise<void>((resolve) => server.close(() => resolve()));
    }
  });
});
