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

describe("Portal draft write routes", () => {
  it("requires CSRF and projects server-owned revision envelopes", async () => {
    const calls: { operation: string; payload: unknown }[] = [];
    const composer: PortalComposerPort = { async call(_principal, operation, payload) {
      calls.push({ operation, payload: JSON.parse(new TextDecoder().decode(payload)) as unknown });
      return new TextEncoder().encode('{"id":7}');
    } };
    const origin = await startServer(composer);
    const sessionCookie = "vastplan_session=browser-token";

    const rejected = await fetch(`${origin}/v1/portal-drafts`, { method: "POST", headers: { Cookie: sessionCookie, "Content-Type": "application/json" }, body: "{}" });
    expect(rejected.status).toBe(403);
    expect(await rejected.json()).toEqual({ error: "csrf_rejected" });
    expect(calls).toEqual([]);

    const csrf = await fetch(`${origin}/v1/csrf`, { headers: { Cookie: sessionCookie } });
    const token = (await csrf.json() as { token: string }).token;
    const headers = { Cookie: `${sessionCookie}; vastplan_csrf=${token}`, "X-VastPlan-CSRF": token, "Content-Type": "application/json" };
    expect((await fetch(`${origin}/v1/portal-drafts`, { method: "POST", headers, body: '{"version":1}' })).status).toBe(200);
    expect((await fetch(`${origin}/v1/portal-drafts/7`, { method: "PUT", headers, body: '{"version":2}' })).status).toBe(200);
    expect((await fetch(`${origin}/v1/portal-drafts/7/submit`, { method: "POST", headers, body: "{}" })).status).toBe(200);
    expect((await fetch(`${origin}/v1/portal-drafts/7/publish`, { method: "POST", headers, body: '{"revisionId":99,"breakGlassReason":"approved"}' })).status).toBe(200);
    expect((await fetch(`${origin}/v1/portal-drafts/7/audit`, { headers: { Cookie: sessionCookie } })).status).toBe(200);

    expect(calls).toEqual([
      { operation: "createDraft", payload: { version: 1 } },
      { operation: "updateDraft", payload: { revisionId: 7, composition: { version: 2 } } },
      { operation: "submit", payload: { revisionId: 7 } },
      { operation: "publish", payload: { revisionId: 7, breakGlassReason: "approved" } },
      { operation: "audit", payload: { revisionId: 7 } },
    ]);
  });

  it("rejects invalid JSON and unsafe revision identifiers before capability invocation", async () => {
    let calls = 0;
    const composer: PortalComposerPort = { async call() { calls += 1; return new TextEncoder().encode("{}"); } };
    const origin = await startServer(composer);
    const sessionCookie = "vastplan_session=browser-token";
    const csrf = await fetch(`${origin}/v1/csrf`, { headers: { Cookie: sessionCookie } });
    const token = (await csrf.json() as { token: string }).token;
    const headers = { Cookie: `${sessionCookie}; vastplan_csrf=${token}`, "X-VastPlan-CSRF": token, "Content-Type": "application/json" };

    const invalid = await fetch(`${origin}/v1/portal-drafts`, { method: "POST", headers, body: "{" });
    expect(invalid.status).toBe(400);
    expect(await invalid.json()).toEqual({ error: "invalid_json" });
    const unsafe = await fetch(`${origin}/v1/portal-drafts/9007199254740992`, { method: "PUT", headers, body: "{}" });
    expect(unsafe.status).toBe(400);
    expect(await unsafe.json()).toEqual({ error: "invalid_revision" });
    expect(calls).toBe(0);
  });
});

async function startServer(composer: PortalComposerPort): Promise<string> {
  const root = await createPortalFixture();
  const sessionFile = join(root, "sessions.json");
  await writeSessionFixture(sessionFile, "browser-token", new Date(Date.now() + 60_000));
  const assets = await PortalAssets.load(root);
  const identity = await FileIdentityProvider.open(sessionFile);
  const server = createServer(createPortalHandler({ assets, identity, composer, secureCookies: false }));
  servers.push(server);
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  return `http://127.0.0.1:${(server.address() as AddressInfo).port}`;
}
