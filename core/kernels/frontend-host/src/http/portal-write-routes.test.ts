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

describe("Portal aggregate write routes", () => {
  it("requires CSRF before creating a Portal", async () => {
    let calls = 0;
    const composer: PortalComposerPort = { async call() { calls += 1; return new TextEncoder().encode('{"id":"operations"}'); } };
    const origin = await startServer(composer);
    const rejected = await fetch(`${origin}/v1/portals`, { method: "POST", headers: { Cookie: "vastplan_session=browser-token", "Content-Type": "application/json" }, body: "{}" });
    expect(rejected.status).toBe(403);
    expect(await rejected.json()).toEqual({ error: "csrf_rejected" });
    expect(calls).toBe(0);
  });

  it("rejects malformed JSON and unsafe version identifiers", async () => {
    let calls = 0;
    const composer: PortalComposerPort = { async call() { calls += 1; return new TextEncoder().encode("{}"); } };
    const origin = await startServer(composer);
    const sessionCookie = "vastplan_session=browser-token";
    const csrf = await fetch(`${origin}/v1/csrf`, { headers: { Cookie: sessionCookie } });
    const token = (await csrf.json() as { token: string }).token;
    const headers = { Cookie: `${sessionCookie}; vastplan_csrf=${token}`, "X-VastPlan-CSRF": token, "Content-Type": "application/json" };
    expect((await fetch(`${origin}/v1/portals`, { method: "POST", headers, body: "{" })).status).toBe(400);
    const unsafe = await fetch(`${origin}/v1/portals/operations/versions/9007199254740992`, { method: "PUT", headers, body: "{}" });
    expect(unsafe.status).toBe(400);
    expect(await unsafe.json()).toEqual({ error: "invalid_portal_version" });
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
