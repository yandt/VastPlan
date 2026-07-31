import { createServer } from "node:http";
import type { AddressInfo } from "node:net";
import { join } from "node:path";
import { afterEach, describe, expect, it, vi } from "vitest";
import { PortalAssets } from "../assets/portal-assets";
import type { PortalComposerPort } from "../capabilities/portal-composer-client";
import { FileIdentityProvider } from "../identity/file-identity-provider";
import { PortalDeliveryStore } from "../runtime/portal-delivery-store";
import { createPortalDeliveryFixture, writePortalDeliveryRevision } from "../testing/portal-delivery-fixture";
import { createPortalFixture } from "../testing/portal-fixture";
import { writeSessionFixture } from "../testing/session-fixture";
import { createPortalHandler } from "./portal-handler";
import type { PortalGenerationCoordinationPort } from "../runtime/portal-generation-coordinator";

const servers: ReturnType<typeof createServer>[] = [];
afterEach(async () => Promise.all(servers.splice(0).map((server) => new Promise<void>((resolve) => server.close(() => resolve())))));

describe("Portal runtime routes", () => {
  it("serves active RuntimeSpec and only its digest-authorized immutable objects", async () => {
    const fixture = await startRuntimeServer();
    const runtimeResponse = await fetch(`${fixture.origin}/v1/portal-runtime?path=/operations/settings`, { headers: fixture.headers });
    expect(runtimeResponse.status).toBe(200);
    expect(runtimeResponse.headers.get("link")).toContain("crossorigin=use-credentials");
    const runtime = await runtimeResponse.json() as { portal: { revision: number; experience: { permissions: string[] }; account: unknown; preferenceScope: unknown }; modules: Array<{ url: string; sha256: string }> };
    expect(runtime.portal.revision).toBe(7);
    expect(runtime.portal.experience.permissions).toEqual(["portal.read"]);
    expect(runtime.portal.account).toEqual({ subjectID: "alice", tenantID: "tenant-a", displayName: "alice" });
    expect(runtime.portal.preferenceScope).toEqual(preferenceScope());
    expect(runtimeResponse.headers.get("cache-control")).toContain("no-store");
    expect(runtimeResponse.headers.get("vary")).toContain("Cookie");
    expect(runtime.modules[0]?.sha256).toBe(fixture.activeDigest);

    const module = await fetch(`${fixture.origin}${runtime.modules[0]!.url}`, { headers: fixture.headers });
    expect(module.status).toBe(200);
    expect(module.headers.get("cache-control")).toContain("immutable");
    expect(module.headers.get("x-vastplan-module-sha256")).toBe(fixture.activeDigest);
    expect(await module.text()).toContain("active");
    const cached = await fetch(`${fixture.origin}${runtime.modules[0]!.url}`, { headers: { ...fixture.headers, "If-None-Match": module.headers.get("etag")! } });
    expect(cached.status).toBe(304);

    const historical = await fetch(`${fixture.origin}/v1/portal-modules/6/${fixture.fallbackDigest}.js`, { headers: fixture.headers });
    expect(historical.status).toBe(404);
    const unknown = await fetch(`${fixture.origin}/v1/portal-modules/7/${"a".repeat(64)}.js`, { headers: fixture.headers });
    expect(unknown.status).toBe(404);
  });

  it("binds recovery URLs to the current revision and server-selected fallback", async () => {
    const fixture = await startRuntimeServer();
    const response = await fetch(`${fixture.origin}/v1/portal-recovery?path=/operations`, { headers: fixture.headers });
    expect(response.status).toBe(200);
    expect(response.headers.get("x-vastplan-recovery-from")).toBe("7");
    expect(response.headers.get("x-vastplan-recovery-revision")).toBe("6");
    const runtime = await response.json() as { modules: Array<{ url: string }> };
    expect(runtime.modules[0]?.url).toBe(`/v1/portal-recovery-modules/7/6/${fixture.fallbackDigest}.js`);
    const module = await fetch(`${fixture.origin}${runtime.modules[0]!.url}`, { headers: fixture.headers });
    expect(module.status).toBe(200);
    expect(await module.text()).toContain("fallback");
    const forged = await fetch(`${fixture.origin}/v1/portal-recovery-modules/7/5/${fixture.fallbackDigest}.js`, { headers: fixture.headers });
    expect(forged.status).toBe(404);
  });

  it("rejects invalid selection queries and preserves HEAD semantics", async () => {
    const fixture = await startRuntimeServer();
    expect((await fetch(`${fixture.origin}/v1/portal-runtime?path=relative`, { headers: fixture.headers })).status).toBe(400);
    expect((await fetch(`${fixture.origin}/v1/portal-runtime?path=/operations&path=/other`, { headers: fixture.headers })).status).toBe(400);
    const head = await fetch(`${fixture.origin}/v1/portal-runtime?path=/operations`, { method: "HEAD", headers: fixture.headers });
    expect(head.status).toBe(200);
    expect(await head.text()).toBe("");
  });

  it("fails closed when Composer returns a malformed active routing contract", async () => {
    const fixture = await startRuntimeServer({ domains: "not-an-array" });
    const response = await fetch(`${fixture.origin}/v1/portal-runtime?path=/operations`, { headers: fixture.headers });
    expect(response.status).toBe(502);
    expect(await response.json()).toEqual({ error: "portal_service_unavailable" });
  });

  it("opens one authenticated SSE stream from the durable current revision", async () => {
    const fixture = await startRuntimeServer();
    const controller = new AbortController();
    const response = await fetch(`${fixture.origin}/v1/portal-updates?path=/operations&revision=7`, {
      headers: fixture.headers, signal: controller.signal,
    });
    expect(response.status).toBe(200);
    expect(response.headers.get("content-type")).toContain("text/event-stream");
    const event = await response.body!.getReader().read();
    expect(new TextDecoder().decode(event.value)).toContain('"activationId":7');
    controller.abort();
    const future = await fetch(`${fixture.origin}/v1/portal-updates?path=/operations&revision=8`, { headers: fixture.headers });
    expect(future.status).toBe(400);
  });

  it("projects a prepared Server transaction and commits it only through authenticated CSRF", async () => {
    const transactionId = "a".repeat(64);
    const prepare = vi.fn(async () => ({ state: "prepared" as const, activationId: 7, transactionId }));
    const generations: PortalGenerationCoordinationPort = {
      prepare,
      commit: vi.fn(async (_principal, value) => {
        expect(value).toBe(transactionId);
        return { state: "committed" as const, activationId: 7 };
      }),
    };
    const fixture = await startRuntimeServer({}, generations);
    const runtimeResponse = await fetch(`${fixture.origin}/v1/portal-runtime?path=/operations`, { headers: fixture.headers });
    expect((await runtimeResponse.json() as { coordination: unknown }).coordination).toEqual({ state: "prepared", activationId: 7, transactionId });
    expect((await fetch(`${fixture.origin}/v1/portal-runtime?path=/operations`, { method: "HEAD", headers: fixture.headers })).status).toBe(200);
    expect(prepare).toHaveBeenCalledOnce();
    expect((await fetch(`${fixture.origin}/v1/portal-generation-commit`, {
      method: "POST", headers: { ...fixture.headers, "Content-Type": "application/json" }, body: JSON.stringify({ transactionId }),
    })).status).toBe(403);

    const csrfResponse = await fetch(`${fixture.origin}/v1/csrf`, { headers: fixture.headers });
    const { token } = await csrfResponse.json() as { token: string };
    const csrfCookie = csrfResponse.headers.get("set-cookie")?.split(";", 1)[0];
    const committed = await fetch(`${fixture.origin}/v1/portal-generation-commit`, {
      method: "POST",
      headers: { Cookie: `${fixture.headers.Cookie}; ${csrfCookie}`, "Content-Type": "application/json", "X-VastPlan-CSRF": token },
      body: JSON.stringify({ transactionId }),
    });
    expect(committed.status).toBe(200);
    expect(await committed.json()).toEqual({ state: "committed", activationId: 7 });
  });
});

async function startRuntimeServer(activeOverrides: Readonly<Record<string, unknown>> = {}, generations?: PortalGenerationCoordinationPort): Promise<{ origin: string; headers: { Cookie: string }; activeDigest: string; fallbackDigest: string }> {
  const assetsRoot = await createPortalFixture();
  const sessionFile = join(assetsRoot, "sessions.json");
  await writeSessionFixture(sessionFile, "browser-token", new Date(Date.now() + 60_000), ["portal.read"]);
  const deliveryFixture = await createPortalDeliveryFixture();
  const base = {
    id: "operations", tenantId: "tenant-a", route: "/operations", audience: ["portal.read"], updates: { mode: "generation" },
    renderAdapter: { id: "cn.vastplan.render", uiContract: "^4.0.0" },
    shell: { id: "cn.vastplan.shell", uiContract: "^4.0.0" },
    workbench: { id: "cn.vastplan.workbench", uiContract: "^4.0.0" },
  } as const;
  const active = await writePortalDeliveryRevision(deliveryFixture, { ...base, revision: 7 }, "export const state = 'active';\n");
  const fallback = await writePortalDeliveryRevision(deliveryFixture, { ...base, revision: 6 }, "export const state = 'fallback';\n");
  const activations = [
    { id: 7, tenantId: "tenant-a", portalId: "operations", status: "Current", resolved: { ...active.spec, ...activeOverrides } },
    { id: 6, tenantId: "tenant-a", portalId: "operations", status: "Superseded", resolved: fallback.spec },
    { id: 5, tenantId: "tenant-a", portalId: "operations", status: "Failed", resolved: {} },
  ];
  const composer: PortalComposerPort = { async call(_principal, operation) {
    if (operation !== "listPortalReleases") throw new Error("unexpected operation");
    return new TextEncoder().encode(JSON.stringify(activations));
  } };
  const assets = await PortalAssets.load(assetsRoot);
  const identity = await FileIdentityProvider.open(sessionFile);
  const delivery = await PortalDeliveryStore.open(deliveryFixture.cache, deliveryFixture.origin);
  const server = createServer(createPortalHandler({ assets, identity, composer, delivery, secureCookies: false, ...(generations === undefined ? {} : { generations }) }));
  servers.push(server);
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  const address = server.address() as AddressInfo;
  return { origin: `http://127.0.0.1:${address.port}`, headers: { Cookie: "vastplan_session=browser-token" }, activeDigest: active.digest, fallbackDigest: fallback.digest };
}

function preferenceScope() {
  return {
    portalId: "operations",
    workbench: { id: "cn.vastplan.workbench", contractMajor: 4 },
  };
}
