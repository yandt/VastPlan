import { generateKeyPairSync, randomBytes, sign } from "node:crypto";
import { mkdtemp, writeFile } from "node:fs/promises";
import { createServer } from "node:http";
import type { AddressInfo } from "node:net";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, describe, expect, it } from "vitest";
import { createAccessGeneration } from "../access/access-generation";
import { PortalAssets } from "../assets/portal-assets";
import { createPortalHandler } from "../http/portal-handler";
import { createPortalFixture } from "../testing/portal-fixture";
import type { AuthenticationBrokerPort } from "./authentication-broker-port";
import { BrokerIdentityProvider } from "./broker-identity-provider";
import type { SessionAuthorizationPort } from "./session-authorization-port";
import type { AuthenticationAssertion } from "./signed-authentication-assertion";

const closeCallbacks: Array<() => Promise<void>> = [];
afterEach(async () => { await Promise.allSettled(closeCallbacks.splice(0).map((close) => close())); });

describe("BrokerIdentityProvider", () => {
  it("turns a one-use Broker Assertion and authorization snapshot into a sealed session", async () => {
    const root = await mkdtemp(join(tmpdir(), "vastplan-broker-identity-"));
    const sessionKeyFile = join(root, "session.key"), trustFile = join(root, "assertion-trust.json");
    await writeFile(sessionKeyFile, randomBytes(32), { mode: 0o600 });
    const { publicKey, privateKey } = generateKeyPairSync("ed25519");
    const publicJWK = publicKey.export({ format: "jwk" });
    const rawPublic = Buffer.from(publicJWK.x!, "base64url");
    await writeFile(trustFile, JSON.stringify({ version: 1, keys: [{ keyId: "broker-key.1", publicKey: rawPublic.toString("base64").replace(/=+$/, "") }] }), { mode: 0o600 });

    const consumed = new Set<string>();
    const routes = new Map<string, { audience: string; profile: string }>();
    const broker: AuthenticationBrokerPort = { async call(_tenant, operation, payload) {
      const request = payload as Record<string, unknown>;
      if (operation === "describe") return { protocol: "authentication.method.v1", methods: [{ methodId: "password", providerId: "database", kind: "password", interaction: "form", displayName: { "zh-CN": "密码登录" }, amr: ["pwd"], acr: "aal1", supportsResend: false }] };
      if (operation === "begin" || operation === "beginProviderTest") {
        routes.set(String(request.transactionId), { audience: operation === "begin" ? `portal:127.0.0.1:operations` : "authentication-provider-test", profile: operation === "begin" ? "enterprise-users" : String(request.providerProfileId) });
        return { result: { state: "challenge", step: { stepId: "s".repeat(32), kind: "password", expiresAt: new Date(Date.now() + 60_000).toISOString() } } };
      }
      if (operation === "continue") {
        const route = routes.get(String(request.transactionId))!;
        const now = new Date(), assertion: AuthenticationAssertion = {
          schemaVersion: "v1", assertionId: `assertion.${String(request.transactionId).slice(0, 16)}`, transactionId: String(request.transactionId), providerId: "database", providerProfileId: route.profile,
          subject: { id: "alice", issuer: "urn:vastplan:database-users" }, tenantId: "acme", portalId: "operations", audience: route.audience,
          amr: ["pwd"], acr: "aal1", issuedAt: now.toISOString(), expiresAt: new Date(now.getTime() + 30_000).toISOString(), nonce: "n".repeat(32),
        };
        const signature = sign(null, Buffer.from(canonicalAssertion(assertion)), privateKey).toString("base64url");
        return { result: { state: "authenticated", evidence: { transactionId: assertion.transactionId } }, assertion: { payload: assertion, signature: { algorithm: "Ed25519", keyId: "broker-key.1", value: signature } } };
      }
      if (operation === "consumeAssertion") {
        const id = String((request.assertion as { payload: { assertionId: string } }).payload.assertionId);
        if (consumed.has(id)) throw new Error("assertion replayed");
        consumed.add(id); return { consumed: true };
      }
      throw new Error(`unexpected operation ${operation}`);
    } };
    const authorization: SessionAuthorizationPort = { async resolve(assertion) { return { subjectId: "enterprise.alice", tenantId: assertion.tenantId, roles: ["platform.settings.read", "foundation.security.authentication.providers.test"], policy: { id: "platform.root", revision: 7, digest: "a".repeat(64) }, expiresAt: new Date(Date.now() + 300_000).toISOString() }; } };
    const generation = createAccessGeneration({
      version: 1, revision: 1, id: "operations-access", tenantId: "acme", portalId: "operations", route: "/", domains: ["127.0.0.1"],
      platformProfile: { id: "portal", revision: 1, digest: "b".repeat(64) }, accessTemplate: "access",
      localization: { defaultLocale: "zh-CN", supportedLocales: ["zh-CN"] }, authentication: { allowedMethods: ["password"], defaultMethod: "password", reuseIdentifier: true }, branding: { productName: { "zh-CN": "VastPlan" } },
    });
    const identity = await BrokerIdentityProvider.open({ kind: "broker", assertionTrustFile: trustFile, sessionKeyFile, sessionMaxAgeSeconds: 300 }, { async resolve() { return generation; } }, broker, authorization);
    const assets = await PortalAssets.load(await createPortalFixture());
    const server = createServer(createPortalHandler({ assets, identity, secureCookies: false }));
    await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
    closeCallbacks.push(() => new Promise((resolve, reject) => server.close((error) => error === undefined ? resolve() : reject(error))));
    const address = server.address() as AddressInfo, portal = `http://127.0.0.1:${address.port}`;
    const protectedPage = await fetch(`${portal}/operations`, { redirect: "manual" });
    expect(protectedPage.headers.get("location")).toBe("/auth/access?returnTo=%2Foperations");
    expect((await fetch(`${portal}/auth/access?returnTo=%2Foperations`)).status).toBe(200);

    const csrfResponse = await fetch(`${portal}/auth/v1/csrf`), csrf = (await csrfResponse.json() as { token: string }).token;
    const csrfCookie = cookieFrom(csrfResponse.headers, "vastplan_csrf");
    const headers = { Origin: portal, "Sec-Fetch-Site": "same-origin", "Content-Type": "application/json", "X-VastPlan-CSRF": csrf, Cookie: `vastplan_csrf=${csrfCookie}` };
    const begin = await fetch(`${portal}/auth/v1/transactions`, { method: "POST", headers, body: JSON.stringify({ methodId: "password", locale: "zh-CN", returnTo: "/operations" }) });
    expect(begin.status).toBe(201);
    const transactionId = (await begin.json() as { transactionId: string }).transactionId;
    const transaction = cookieFrom(begin.headers, "vastplan_auth_tx");
    const complete = await fetch(`${portal}/auth/v1/transactions/${transactionId}/continue`, { method: "POST", headers: { ...headers, Cookie: `vastplan_csrf=${csrfCookie}; vastplan_auth_tx=${transaction}` }, body: JSON.stringify({ stepId: "s".repeat(32), responses: [{ fieldId: "password", value: "never-logged" }] }) });
    expect(complete.status).toBe(200);
    expect(await complete.json()).toMatchObject({ result: { state: "authenticated" }, returnTo: "/operations" });
    const session = cookieFrom(complete.headers, "vastplan_session");
    expect(session).not.toContain("alice");
    const current = await fetch(`${portal}/auth/session`, { headers: { Cookie: `vastplan_session=${session}` } });
    expect(await current.json()).toEqual({ authenticated: true, subject: "enterprise.alice", tenantId: "acme", roles: ["platform.settings.read", "foundation.security.authentication.providers.test"] });

    const testHeaders = { ...headers, Cookie: `vastplan_session=${session}; vastplan_csrf=${csrfCookie}` };
    const beginTest = await fetch(`${portal}/auth/v1/provider-tests`, { method: "POST", headers: testHeaders, body: JSON.stringify({ providerProfileId: "draft-provider", methodId: "password", locale: "zh-CN", returnTo: "/settings/providers?providerTestReceipt=draft-provider" }) });
    expect(beginTest.status).toBe(201);
    const testTransactionId = (await beginTest.json() as { transactionId: string }).transactionId;
    const testTransaction = cookieFrom(beginTest.headers, "vastplan_auth_tx");
    const completeTest = await fetch(`${portal}/auth/v1/transactions/${testTransactionId}/continue`, { method: "POST", headers: { ...testHeaders, Cookie: `vastplan_session=${session}; vastplan_csrf=${csrfCookie}; vastplan_auth_tx=${testTransaction}` }, body: JSON.stringify({ stepId: "s".repeat(32), responses: [{ fieldId: "password", value: "test-only" }] }) });
    expect(completeTest.status).toBe(200);
    expect(cookieFrom(completeTest.headers, "vastplan_auth_test")).not.toContain("alice");
    expect(consumed.size).toBe(2);
  });
});

function canonicalAssertion(value: AuthenticationAssertion): string {
  return JSON.stringify({ schemaVersion: value.schemaVersion, assertionId: value.assertionId, transactionId: value.transactionId, providerId: value.providerId, providerProfileId: value.providerProfileId, subject: { id: value.subject.id, issuer: value.subject.issuer }, tenantId: value.tenantId, portalId: value.portalId, audience: value.audience, amr: [...value.amr].sort(), acr: value.acr, issuedAt: value.issuedAt, expiresAt: value.expiresAt, nonce: value.nonce });
}
function cookieFrom(headers: Headers, name: string): string { for (const value of headers.getSetCookie()) { const match = new RegExp(`(?:^|;)\\s*${name}=([^;]*)`).exec(value); if (match !== null) return match[1]; } throw new Error(`缺少 cookie: ${name}`); }
