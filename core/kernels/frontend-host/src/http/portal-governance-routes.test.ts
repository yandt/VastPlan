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

describe("Portal governance routes", () => {
  it("maps Profile, Binding and Activation workflows to isolated Composer operations", async () => {
    const calls: { operation: string; payload: unknown }[] = [];
    const composer: PortalComposerPort = { async call(_principal, operation, payload) {
      calls.push({ operation, payload: JSON.parse(new TextDecoder().decode(payload)) as unknown });
      return new TextEncoder().encode('{"id":11}');
    } };
    const { origin, headers } = await startServer(composer);
    const requests: [string, "POST" | "PUT", string][] = [
      ["/v1/portal-governance/profiles", "POST", '{"id":"standard"}'],
      ["/v1/portal-governance/profiles/3", "PUT", '{"id":"standard-v2"}'],
      ["/v1/portal-governance/profiles/3/approve", "POST", "{}"],
      ["/v1/portal-governance/bindings", "POST", '{"profileRevisionId":3,"binding":{"portalId":"admin"}}'],
      ["/v1/portal-governance/bindings/5", "PUT", '{"profileRevisionId":3,"binding":{"portalId":"ops"}}'],
      ["/v1/portal-governance/bindings/5/publish", "POST", "{}"],
      ["/v1/portal-governance/activations", "POST", '{"portalId":"admin","expectedCurrentId":0}'],
      ["/v1/portal-governance/activations/9/rollback", "POST", '{"sourceId":99,"expectedCurrentId":10,"reason":"restore"}'],
    ];
    for (const [path, method, body] of requests) {
      const response = await fetch(`${origin}${path}`, { method, headers, body });
      expect(response.status, path).toBe(200);
    }
    expect(calls).toEqual([
      { operation: "createProfileDraft", payload: { id: "standard" } },
      { operation: "updateProfileDraft", payload: { revisionId: 3, profile: { id: "standard-v2" } } },
      { operation: "transitionProfile", payload: { revisionId: 3, action: "approve" } },
      { operation: "createBindingDraft", payload: { profileRevisionId: 3, binding: { portalId: "admin" } } },
      { operation: "updateBindingDraft", payload: { revisionId: 5, draft: { profileRevisionId: 3, binding: { portalId: "ops" } } } },
      { operation: "transitionBinding", payload: { revisionId: 5, action: "publish" } },
      { operation: "activate", payload: { portalId: "admin", expectedCurrentId: 0 } },
      { operation: "rollbackActivation", payload: { sourceId: 9, expectedCurrentId: 10, reason: "restore" } },
    ]);
  });

  it("rejects unknown governance resources and invalid revision paths without invoking Composer", async () => {
    let calls = 0;
    const composer: PortalComposerPort = { async call() { calls += 1; return new TextEncoder().encode("{}"); } };
    const { origin, headers } = await startServer(composer);
    const unknown = await fetch(`${origin}/v1/portal-governance/secrets`, { headers });
    expect(unknown.status).toBe(404);
    const invalid = await fetch(`${origin}/v1/portal-governance/profiles/not-a-number`, { method: "PUT", headers, body: "{}" });
    expect(invalid.status).toBe(400);
    expect(await invalid.json()).toEqual({ error: "invalid_revision" });
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
