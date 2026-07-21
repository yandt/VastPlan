import { createServer } from "node:http";
import type { AddressInfo } from "node:net";
import { join } from "node:path";
import { afterEach, describe, expect, it } from "vitest";
import { PortalAssets } from "../assets/portal-assets";
import { FileIdentityProvider } from "../identity/file-identity-provider";
import type { IdentityProvider } from "../identity/identity-provider";
import { createPortalFixture } from "../testing/portal-fixture";
import { writeSessionFixture } from "../testing/session-fixture";
import { createPortalHandler } from "./portal-handler";

const servers: ReturnType<typeof createServer>[] = [];
afterEach(async () => Promise.all(servers.splice(0).map((server) => new Promise<void>((resolve) => server.close(() => resolve())))));

describe("createPortalHandler", () => {
  it("serves the SPA shell securely while keeping API paths fail-closed", async () => {
    const origin = await startFixtureServer();
    const shell = await fetch(`${origin}/settings`);
    expect(shell.status).toBe(200);
    expect(shell.headers.get("content-security-policy")).toContain("frame-ancestors 'none'");
    expect(shell.headers.get("x-content-type-options")).toBe("nosniff");
    expect(await shell.text()).not.toContain("__VASTPLAN_CSP_NONCE__");

    const api = await fetch(`${origin}/v1/not-implemented`);
    expect(api.status).toBe(404);
    expect(await api.text()).toBe("");
    const apiWrite = await fetch(`${origin}/v1/not-implemented`, { method: "POST" });
    expect(apiWrite.status).toBe(404);
  });

  it("supports immutable identity checks and HEAD without response bodies", async () => {
    const origin = await startFixtureServer();
    const asset = await fetch(`${origin}/assets/app.js`);
    const etag = asset.headers.get("etag");
    expect(etag).toMatch(/^"sha256-/);
    expect(await asset.text()).toContain("ready");

    const cached = await fetch(`${origin}/assets/app.js`, { headers: { "If-None-Match": etag! } });
    expect(cached.status).toBe(304);
    const head = await fetch(`${origin}/assets/app.js`, { method: "HEAD" });
    expect(head.status).toBe(200);
    expect(await head.text()).toBe("");
  });

  it("issues CSRF only after session authentication", async () => {
    const root = await createPortalFixture();
    const sessionFile = join(root, "sessions.json");
    await writeSessionFixture(sessionFile, "browser-token", new Date(Date.now() + 60_000));
    const identity = await FileIdentityProvider.open(sessionFile);
    const origin = await startFixtureServer(identity);

    const unauthorized = await fetch(`${origin}/v1/csrf`);
    expect(unauthorized.status).toBe(401);
    expect(await unauthorized.json()).toEqual({ error: "session_required" });
    const authorized = await fetch(`${origin}/v1/csrf`, { headers: { Cookie: "vastplan_session=browser-token" } });
    expect(authorized.status).toBe(200);
    const body = await authorized.json() as { token: string };
    expect(body.token).toMatch(/^[a-f0-9]{64}$/);
    expect(authorized.headers.get("set-cookie")).toContain(`vastplan_csrf=${body.token}`);
  });
});

async function startFixtureServer(identity?: IdentityProvider): Promise<string> {
  const assets = await PortalAssets.load(await createPortalFixture());
  const server = createServer(createPortalHandler({ assets, identity, secureCookies: false }));
  servers.push(server);
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  const address = server.address() as AddressInfo;
  return `http://127.0.0.1:${address.port}`;
}
