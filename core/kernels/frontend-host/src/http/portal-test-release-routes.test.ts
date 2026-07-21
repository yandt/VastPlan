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

describe("Portal frontend test release routes", () => {
  it("maps target bindings and test releases without trusting path-owned IDs from JSON", async () => {
    const calls: { operation: string; payload: unknown }[] = [];
    const composer: PortalComposerPort = { async call(_principal, operation, payload) {
      calls.push({ operation, payload: JSON.parse(new TextDecoder().decode(payload)) as unknown });
      return new TextEncoder().encode("[]");
    } };
    const { origin, readHeaders, writeHeaders } = await startServer(composer);
    expect((await fetch(`${origin}/v1/portal-governance/test-target-bindings`, { headers: readHeaders })).status).toBe(200);
    expect((await fetch(`${origin}/v1/portal-governance/test-target-bindings/admin`, { method: "PUT", headers: writeHeaders, body: '{"id":"forged","enabled":true}' })).status).toBe(200);
    expect((await fetch(`${origin}/v1/portal-governance/test-releases`, { headers: readHeaders })).status).toBe(200);
    expect((await fetch(`${origin}/v1/portal-governance/test-releases`, { method: "POST", headers: writeHeaders, body: '{"pluginId":"cn.vastplan.demo"}' })).status).toBe(200);
    expect((await fetch(`${origin}/v1/portal-governance/test-releases/12/rollback`, { method: "POST", headers: writeHeaders, body: '{"id":99}' })).status).toBe(200);
    expect(calls).toEqual([
      { operation: "listTestTargetBindings", payload: {} },
      { operation: "putTestTargetBinding", payload: { id: "admin", binding: { id: "forged", enabled: true } } },
      { operation: "listTestReleases", payload: {} },
      { operation: "createTestRelease", payload: { pluginId: "cn.vastplan.demo" } },
      { operation: "rollbackTestRelease", payload: { id: 12 } },
    ]);
  });

  it("rejects invalid target IDs and release revisions before invoking Composer", async () => {
    let calls = 0;
    const composer: PortalComposerPort = { async call() { calls += 1; return new TextEncoder().encode("{}"); } };
    const { origin, writeHeaders } = await startServer(composer);
    const target = await fetch(`${origin}/v1/portal-governance/test-target-bindings/Admin`, { method: "PUT", headers: writeHeaders, body: "{}" });
    expect(target.status).toBe(404);
    const release = await fetch(`${origin}/v1/portal-governance/test-releases/0/rollback`, { method: "POST", headers: writeHeaders, body: "{}" });
    expect(release.status).toBe(400);
    expect(calls).toBe(0);
  });
});

async function startServer(composer: PortalComposerPort): Promise<{ origin: string; readHeaders: Record<string, string>; writeHeaders: Record<string, string> }> {
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
  return {
    origin,
    readHeaders: { Cookie: sessionCookie },
    writeHeaders: { Cookie: `${sessionCookie}; vastplan_csrf=${token}`, "X-VastPlan-CSRF": token, "Content-Type": "application/json" },
  };
}
