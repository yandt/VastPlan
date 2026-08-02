import { createServer } from "node:http";
import type { AddressInfo } from "node:net";
import { join } from "node:path";
import { afterEach, describe, expect, it } from "vitest";
import { PortalAssets } from "../assets/portal-assets";
import { CapabilityApplicationError } from "../capabilities/capability-invoker";
import type { PortalComposerPort } from "../capabilities/portal-composer-client";
import { FileIdentityProvider } from "../identity/file-identity-provider";
import { createPortalFixture } from "../testing/portal-fixture";
import { writeSessionFixture } from "../testing/session-fixture";
import { createPortalHandler } from "./portal-handler";

const servers: ReturnType<typeof createServer>[] = [];
afterEach(async () => Promise.all(servers.splice(0).map((server) => new Promise<void>((resolve) => server.close(() => resolve())))));

describe("Portal aggregate routes", () => {
  it("maps working copy, publication, releases and optional history through one Portal resource", async () => {
    const calls: { operation: string; payload: unknown }[] = [];
    const composer: PortalComposerPort = { async call(_principal, operation, payload) {
      calls.push({ operation, payload: JSON.parse(new TextDecoder().decode(payload)) as unknown });
      return new TextEncoder().encode('{"id":11}');
    } };
    const { origin, headers } = await startServer(composer);
    const requests: [string, "GET" | "POST" | "PUT" | "DELETE", string | undefined][] = [
      ["/v1/portals", "GET", undefined],
      ["/v1/portals", "POST", '{"portalId":"operations","configuration":{"route":"/operations"}}'],
      ["/v1/portals/operations/working-copy", "POST", '{"route":"/operations-v2"}'],
      ["/v1/portals/operations/working-copy", "PUT", '{"expectedRevision":2,"configuration":{"route":"/operations-v3"}}'],
      ["/v1/portals/operations/publications", "POST", '{"expectedWorkingRevision":3}'],
      ["/v1/portals/operations/publications/7/approve", "POST", `{"review":{"expectedDigest":"${"a".repeat(64)}","acknowledged":true,"reason":"reviewed"}}`],
      ["/v1/portals/operations/publications/7/publish", "POST", "{}"],
      ["/v1/portals/operations/publications/7/audit", "GET", undefined],
      ["/v1/portals/operations/releases", "POST", '{"publicationId":7,"expectedCurrentReleaseId":0}'],
      ["/v1/portals/operations/history", "GET", undefined],
      ["/v1/portals/operations/history/version-1", "GET", undefined],
      ["/v1/portals/operations/compare?left=version-1&right=version-2", "GET", undefined],
      ["/v1/portals/operations/history/version-1/restore", "POST", '{"expectedWorkingRevision":4}'],
      ["/v1/portals/operations/releases/9/rollback", "POST", '{"portalId":"forged","releaseId":77,"expectedCurrentReleaseId":10,"reason":"restore"}'],
    ];
    for (const [path, method, body] of requests) {
      const response = await fetch(`${origin}${path}`, { method, headers, ...(body === undefined ? {} : { body }) });
      expect(response.status, path).toBe(200);
    }
    expect(calls).toEqual([
      { operation: "portalGovernance", payload: {} },
      { operation: "createPortal", payload: { portalId: "operations", configuration: { route: "/operations" } } },
      { operation: "createPortalWorkingCopy", payload: { portalId: "operations", configuration: { route: "/operations-v2" } } },
      { operation: "savePortalWorkingCopy", payload: { portalId: "operations", workingCopy: { expectedRevision: 2, configuration: { route: "/operations-v3" } } } },
      { operation: "submitPortalPublication", payload: { portalId: "operations", publication: { expectedWorkingRevision: 3 } } },
      { operation: "approvePortalPublication", payload: { portalId: "operations", publicationId: 7, approval: { review: { expectedDigest: "a".repeat(64), acknowledged: true, reason: "reviewed" } } } },
      { operation: "publishPortalPublication", payload: { portalId: "operations", publicationId: 7 } },
      { operation: "audit", payload: { portalId: "operations", revisionId: 7 } },
      { operation: "releasePortalPublication", payload: { portalId: "operations", release: { publicationId: 7, expectedCurrentReleaseId: 0 } } },
      { operation: "portalVersionHistory", payload: { portalId: "operations" } },
      { operation: "readPortalVersion", payload: { portalId: "operations", versionId: "version-1" } },
      { operation: "comparePortalVersions", payload: { portalId: "operations", leftVersionId: "version-1", rightVersionId: "version-2" } },
      { operation: "restorePortalVersion", payload: { portalId: "operations", restore: { expectedWorkingRevision: 4, versionId: "version-1" } } },
      { operation: "rollbackPortalRelease", payload: { portalId: "operations", releaseId: 9, expectedCurrentReleaseId: 10, reason: "restore" } },
    ]);
  });

  it("rejects unknown children and invalid identities before Composer invocation", async () => {
    let calls = 0;
    const composer: PortalComposerPort = { async call() { calls += 1; return new TextEncoder().encode("{}"); } };
    const { origin, headers } = await startServer(composer);
    expect((await fetch(`${origin}/v1/portals/operations/profiles`, { headers })).status).toBe(404);
    const invalid = await fetch(`${origin}/v1/portals/operations/history/%2Fescape`, { headers });
    expect(invalid.status).toBe(400);
    expect(await invalid.json()).toEqual({ error: "invalid_version_id" });
    expect(calls).toBe(0);
  });

  it("preserves approval policy rejection instead of reporting generic forbidden", async () => {
		const composer: PortalComposerPort = { async call() { throw new CapabilityApplicationError("portal.approval.separation_required", "提交人不能自批"); } };
		const { origin, headers } = await startServer(composer);
		const response = await fetch(`${origin}/v1/portals/operations/publications/7/approve`, { method: "POST", headers, body: "{}" });
		expect(response.status).toBe(409);
		expect(await response.json()).toEqual({ error: "approval_separation_required" });
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
