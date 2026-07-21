import { randomBytes } from "node:crypto";
import { mkdtemp, writeFile } from "node:fs/promises";
import { createServer } from "node:http";
import type { AddressInfo } from "node:net";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, describe, expect, it } from "vitest";
import { PortalAssets } from "../assets/portal-assets";
import type { Principal } from "./identity-provider";
import { createPortalHandler } from "../http/portal-handler";
import { createPortalFixture } from "../testing/portal-fixture";
import { startOIDCTestProvider } from "../testing/oidc-test-provider";
import { OIDCIdentityProvider } from "./oidc-identity-provider";

const closeCallbacks: Array<() => Promise<void>> = [];
afterEach(async () => { await Promise.allSettled(closeCallbacks.splice(0).map((close) => close())); });

describe("OIDCIdentityProvider", () => {
  it("executes Authorization Code + PKCE and emits only an encrypted BFF session", async () => {
    const clientId = "vastplan-portal";
    const oidc = await startOIDCTestProvider(clientId);
    closeCallbacks.push(oidc.close);
    const privateRoot = await mkdtemp(join(tmpdir(), "vastplan-oidc-"));
    const sessionKeyFile = join(privateRoot, "session.key");
    await writeFile(sessionKeyFile, randomBytes(32), { mode: 0o600 });
    const identity = await OIDCIdentityProvider.open({
      kind: "oidc", issuer: oidc.issuer, clientId, clientAuthMethod: "none", redirectURI: "http://portal.invalid/auth/callback",
      sessionKeyFile, tenantClaim: "tenant_id", rolesClaim: "roles", scopes: "openid profile", sessionMaxAgeSeconds: 300, allowInsecure: true,
    });
    const assets = await PortalAssets.load(await createPortalFixture());
    const projected: Principal[] = [];
    const composer = { async call(principal: Principal, operation: string) {
      projected.push(principal);
      if (operation !== "list") throw new Error("unexpected operation");
      return new TextEncoder().encode("[]");
    } };
    const server = createServer(createPortalHandler({ assets, identity, composer, secureCookies: false }));
    await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
    closeCallbacks.push(() => new Promise((resolve, reject) => server.close((error) => error === undefined ? resolve() : reject(error))));
    const address = server.address() as AddressInfo;
    const portal = `http://127.0.0.1:${address.port}`;
    const protectedPage = await fetch(`${portal}/operations?view=active`, { redirect: "manual" });
    expect(protectedPage.status).toBe(302);
    expect(protectedPage.headers.get("location")).toBe("/auth/login?returnTo=%2Foperations%3Fview%3Dactive");

    const login = await fetch(`${portal}/auth/login?returnTo=%2Foperations`, { redirect: "manual" });
    expect(login.status).toBe(302);
    const transaction = cookieFrom(login.headers, "vastplan_oidc_tx");
    expect(transaction).toBeDefined();
    expect(transaction).not.toContain("alice");
    const authorize = await fetch(login.headers.get("location")!, { redirect: "manual" });
    expect(authorize.status).toBe(302);
    const callbackURL = new URL(authorize.headers.get("location")!);
    const callback = await fetch(`${portal}${callbackURL.pathname}${callbackURL.search}`, { redirect: "manual", headers: { Cookie: `vastplan_oidc_tx=${transaction}` } });
    expect(callback.status).toBe(303);
    expect(callback.headers.get("location")).toBe("/operations");
    const session = cookieFrom(callback.headers, "vastplan_session");
    expect(session).toBeDefined();
    expect(session).not.toContain("alice");
    expect(callback.headers.getSetCookie().join(";")).not.toContain("opaque-access-token");

    const current = await fetch(`${portal}/auth/session`, { headers: { Cookie: `vastplan_session=${session}` } });
    expect(await current.json()).toEqual({ authenticated: true, subject: "alice", tenantId: "tenant-a", roles: ["portal.read", "platform.admin"] });
    const tampered = await fetch(`${portal}/auth/session`, { headers: { Cookie: `vastplan_session=${session}x` } });
    expect(tampered.status).toBe(401);
    const page = await fetch(`${portal}/operations`, { redirect: "manual", headers: { Cookie: `vastplan_session=${session}` } });
    expect(page.status).toBe(200);
    const drafts = await fetch(`${portal}/v1/portal-drafts`, { headers: { Cookie: `vastplan_session=${session}` } });
    expect(drafts.status).toBe(200);
    expect(projected).toEqual([{ id: "alice", tenantId: "tenant-a", roles: ["portal.read", "platform.admin"] }]);

    const csrf = await fetch(`${portal}/v1/csrf`, { headers: { Cookie: `vastplan_session=${session}` } });
    const csrfBody = await csrf.json() as { token: string };
    const logout = await fetch(`${portal}/auth/logout`, { method: "POST", headers: {
      Cookie: `vastplan_session=${session}; vastplan_csrf=${csrfBody.token}`, "X-VastPlan-CSRF": csrfBody.token,
    } });
    expect(logout.status).toBe(204);
    expect(logout.headers.getSetCookie().join(";")).toContain("vastplan_session=; Path=/; Max-Age=0");
  });
});

function cookieFrom(headers: Headers, name: string): string {
  for (const value of headers.getSetCookie()) {
    const match = new RegExp(`(?:^|;)\\s*${name}=([^;]*)`).exec(value);
    if (match !== null) return match[1];
  }
  throw new Error(`缺少 cookie: ${name}`);
}
