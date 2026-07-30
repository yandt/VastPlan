import { createServer } from "node:http";
import type { AddressInfo } from "node:net";
import { join } from "node:path";
import { afterEach, describe, expect, it } from "vitest";
import { PortalAssets } from "../assets/portal-assets";
import type { PortalComposerPort } from "../capabilities/portal-composer-client";
import { FileIdentityProvider } from "../identity/file-identity-provider";
import { createPortalFixture } from "../testing/portal-fixture";
import { writeSessionFixture } from "../testing/session-fixture";
import { createPortalHandler } from "./portal-handler";

const servers: ReturnType<typeof createServer>[] = [];
afterEach(async () => Promise.all(servers.splice(0).map((server) => new Promise<void>((resolve) => server.close(() => resolve())))));

describe("Portal aggregate routes", () => {
  it("maps versions, publication and releases through one Portal resource", async () => {
    const calls: { operation: string; payload: unknown }[] = [];
    const composer: PortalComposerPort = { async call(_principal, operation, payload) {
      calls.push({ operation, payload: JSON.parse(new TextDecoder().decode(payload)) as unknown });
      return new TextEncoder().encode('{"id":11}');
    } };
    const { origin, headers } = await startServer(composer);
    const requests: [string, "GET" | "POST" | "PUT" | "DELETE", string | undefined][] = [
      ["/v1/portals", "GET", undefined],
      ["/v1/portals", "POST", '{"portalId":"operations","configuration":{"route":"/operations"}}'],
      ["/v1/portals/operations/versions", "POST", '{"route":"/operations-v2"}'],
      ["/v1/portals/operations/versions/7", "PUT", '{"route":"/operations-v3"}'],
      ["/v1/portals/operations/versions/7/submit", "POST", "{}"],
      ["/v1/portals/operations/versions/7/approve", "POST", "{}"],
      ["/v1/portals/operations/versions/7/publish", "POST", '{"breakGlassReason":"emergency repair"}'],
      ["/v1/portals/operations/versions/7/audit", "GET", undefined],
      ["/v1/portals/operations/releases", "POST", '{"portalVersionId":7,"expectedCurrentReleaseId":0}'],
      ["/v1/portals/operations/releases/9/rollback", "POST", '{"portalId":"forged","releaseId":77,"expectedCurrentReleaseId":10,"reason":"restore"}'],
    ];
    for (const [path, method, body] of requests) {
      const response = await fetch(`${origin}${path}`, { method, headers, ...(body === undefined ? {} : { body }) });
      expect(response.status, path).toBe(200);
    }
    expect(calls).toEqual([
      { operation: "portalGovernance", payload: {} },
      { operation: "createPortal", payload: { portalId: "operations", configuration: { route: "/operations" } } },
      { operation: "createPortalVersion", payload: { portalId: "operations", configuration: { route: "/operations-v2" } } },
      { operation: "updatePortalVersion", payload: { portalId: "operations", versionId: 7, configuration: { route: "/operations-v3" } } },
      { operation: "submitPortalVersion", payload: { portalId: "operations", versionId: 7 } },
      { operation: "approvePortalVersion", payload: { portalId: "operations", versionId: 7 } },
      { operation: "publishPortalVersion", payload: { portalId: "operations", versionId: 7, breakGlassReason: "emergency repair" } },
      { operation: "audit", payload: { portalId: "operations", revisionId: 7 } },
      { operation: "releasePortalVersion", payload: { portalId: "operations", release: { portalVersionId: 7, expectedCurrentReleaseId: 0 } } },
      { operation: "rollbackPortalRelease", payload: { portalId: "operations", releaseId: 9, expectedCurrentReleaseId: 10, reason: "restore" } },
    ]);
  });

  it("rejects unknown children and invalid identities before Composer invocation", async () => {
    let calls = 0;
    const composer: PortalComposerPort = { async call() { calls += 1; return new TextEncoder().encode("{}"); } };
    const { origin, headers } = await startServer(composer);
    expect((await fetch(`${origin}/v1/portals/operations/profiles`, { headers })).status).toBe(404);
    const invalid = await fetch(`${origin}/v1/portals/operations/versions/not-a-number`, { method: "PUT", headers, body: "{}" });
    expect(invalid.status).toBe(400);
    expect(await invalid.json()).toEqual({ error: "invalid_portal_version" });
    expect(calls).toBe(0);
  });
});

async function startServer(composer: PortalComposerPort): Promise<{ origin: string; headers: Record<string, string> }> {
  const root = await createPortalFixture();
  const sessionFile = join(root, "sessions.json");
  await writeSessionFixture(sessionFile, "browser-token", new Date(Date.now() + 60_000));
  const assets = await PortalAssets.load(root);
  const identity = await FileIdentityProvider.open(sessionFile);
  const server = createServer(createPortalHandler({ assets, identity, composer, secureCookies: false }));
  servers.push(server);
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  const origin = `http://127.0.0.1:${(server.address() as AddressInfo).port}`;
  const sessionCookie = "vastplan_session=browser-token";
  const csrf = await fetch(`${origin}/v1/csrf`, { headers: { Cookie: sessionCookie } });
  const token = (await csrf.json() as { token: string }).token;
  return { origin, headers: { Cookie: `${sessionCookie}; vastplan_csrf=${token}`, "X-VastPlan-CSRF": token, "Content-Type": "application/json" } };
}
