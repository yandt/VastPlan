import { createServer } from "node:http";
import type { AddressInfo } from "node:net";
import { join } from "node:path";
import { afterEach, describe, expect, it } from "vitest";
import { PortalAssets } from "../assets/portal-assets";
import { CapabilityApplicationError } from "../capabilities/capability-invoker";
import type { PortalComposerPort } from "../capabilities/portal-composer-client";
import type { PortalPreferencePort } from "../capabilities/portal-preference-client";
import { FileIdentityProvider } from "../identity/file-identity-provider";
import { createPortalFixture } from "../testing/portal-fixture";
import { writeSessionFixture } from "../testing/session-fixture";
import { createPortalHandler } from "./portal-handler";

const servers: ReturnType<typeof createServer>[] = [];
afterEach(async () => Promise.all(servers.splice(0).map((server) => new Promise<void>((resolve) => server.close(() => resolve())))));

describe("PortalPreference routes", () => {
  it("derives scope from the active Portal and never accepts browser identity", async () => {
    const calls: Array<{ operation: string; payload: Record<string, unknown> }> = [];
    const fixture = await startPreferenceServer({ async call(_principal, operation, payload) {
      const request = JSON.parse(new TextDecoder().decode(payload)) as Record<string, unknown>;
      calls.push({ operation, payload: request });
      const scope = request.scope;
      return new TextEncoder().encode(JSON.stringify({ revision: operation === "get" ? 0 : 1, scope, values: operation === "get" ? {} : request.values, updatedAt: "2026-07-23T00:00:00Z" }));
    } });

    const response = await fetch(`${fixture.origin}/v1/portal-preference?path=/operations/settings`, { headers: fixture.headers });
    expect(response.status).toBe(200);
    const preference = await response.json() as { revision: number; scope: { portalId: string; renderer: { id: string; contractMajor: number } } };
    expect(preference.revision).toBe(0);
    expect(preference.scope).toMatchObject({ portalId: "operations", renderer: { id: "cn.vastplan.render", contractMajor: 4 } });
    expect(calls[0]?.payload).not.toHaveProperty("tenantId");
    expect(calls[0]?.payload).not.toHaveProperty("subjectId");

    const csrf = await issueCSRF(fixture.origin, fixture.headers.Cookie);
    const invalid = await fetch(`${fixture.origin}/v1/portal-preference?path=/operations`, {
      method: "PUT", headers: { Cookie: `${fixture.headers.Cookie}; ${csrf.cookie}`, "X-VastPlan-CSRF": csrf.token, "Content-Type": "application/json" },
      body: JSON.stringify({ expectedRevision: 0, values: {}, subjectId: "mallory" }),
    });
    expect(invalid.status).toBe(400);

    const updated = await fetch(`${fixture.origin}/v1/portal-preference?path=/operations`, {
      method: "PUT", headers: { Cookie: `${fixture.headers.Cookie}; ${csrf.cookie}`, "X-VastPlan-CSRF": csrf.token, "Content-Type": "application/json" },
      body: JSON.stringify({ expectedRevision: 0, values: { rendererId: "mui", shellTemplateId: "top-navigation" } }),
    });
    expect(updated.status).toBe(200);
    expect(calls.at(-1)?.operation).toBe("put");
    expect(calls.at(-1)?.payload).toMatchObject({ expectedRevision: 0, values: { rendererId: "mui", shellTemplateId: "top-navigation" } });
  });

  it("maps capability CAS conflicts without exposing backend detail", async () => {
    const fixture = await startPreferenceServer({ async call() { throw new CapabilityApplicationError("portal.preference.conflict", "internal revision 42"); } });
    const csrf = await issueCSRF(fixture.origin, fixture.headers.Cookie);
    const response = await fetch(`${fixture.origin}/v1/portal-preference?path=/operations`, {
      method: "PUT", headers: { Cookie: `${fixture.headers.Cookie}; ${csrf.cookie}`, "X-VastPlan-CSRF": csrf.token, "Content-Type": "application/json" },
      body: JSON.stringify({ expectedRevision: 0, values: {} }),
    });
    expect(response.status).toBe(409);
    expect(await response.json()).toEqual({ error: "portal_preference_conflict" });
  });
});

async function startPreferenceServer(preferences: PortalPreferencePort): Promise<{ origin: string; headers: { Cookie: string } }> {
  const root = await createPortalFixture();
  const sessionFile = join(root, "sessions.json");
  await writeSessionFixture(sessionFile, "browser-token", new Date(Date.now() + 60_000), ["portal.read"]);
  const resolved = {
    revision: 7, id: "operations", tenantId: "tenant-a", route: "/operations", audience: ["portal.read"],
    renderAdapter: { id: "cn.vastplan.render", uiContract: "^4.0.0" },
    shell: { id: "cn.vastplan.shell", uiContract: "^4.0.0" },
    workbench: { id: "cn.vastplan.workbench", uiContract: "^4.1.0" },
  };
  const composer: PortalComposerPort = { async call(_principal, operation) {
    if (operation !== "listActivations") throw new Error("unexpected operation");
    return new TextEncoder().encode(JSON.stringify([{ id: 7, tenantId: "tenant-a", portalId: "operations", status: "Current", resolved }]));
  } };
  const assets = await PortalAssets.load(root);
  const identity = await FileIdentityProvider.open(sessionFile);
  const server = createServer(createPortalHandler({ assets, identity, composer, preferences, secureCookies: false }));
  servers.push(server);
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  const address = server.address() as AddressInfo;
  return { origin: `http://127.0.0.1:${address.port}`, headers: { Cookie: "vastplan_session=browser-token" } };
}

async function issueCSRF(origin: string, sessionCookie: string): Promise<{ token: string; cookie: string }> {
  const response = await fetch(`${origin}/v1/csrf`, { headers: { Cookie: sessionCookie } });
  const body = await response.json() as { token: string };
  return { token: body.token, cookie: response.headers.get("set-cookie")!.split(";")[0]! };
}
