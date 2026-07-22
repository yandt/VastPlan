import assert from "node:assert/strict";
import { generateKeyPairSync, sign } from "node:crypto";
import test from "node:test";

import { loadConfiguration } from "../config.mjs";
import { OIDCProvider } from "../provider.mjs";

const now = Date.UTC(2026, 6, 23, 0, 0, 0);
const profiles = loadConfiguration(
  JSON.stringify({
    profiles: {
      corporate: {
        issuer: "https://identity.example.test",
        clientId: "vastplan",
        authorizationEndpoint: "https://identity.example.test/authorize",
        tokenEndpoint: "https://identity.example.test/token",
        jwksUri: "https://identity.example.test/jwks",
        redirectUri: "https://portal.example.test/auth/callback",
        scopes: ["openid", "profile"],
      },
    },
  }),
);

const jwt = (privateKey, header, claims) => {
  const left = Buffer.from(JSON.stringify(header)).toString("base64url");
  const middle = Buffer.from(JSON.stringify(claims)).toString("base64url");
  const signature = sign(
    "sha256",
    Buffer.from(`${left}.${middle}`),
    privateKey,
  ).toString("base64url");
  return `${left}.${middle}.${signature}`;
};

test("OIDC Provider completes PKCE and verifies signed identity", async () => {
  const { publicKey, privateKey } = generateKeyPairSync("rsa", {
    modulusLength: 2048,
  });
  const jwk = publicKey.export({ format: "jwk" });
  Object.assign(jwk, { kid: "key-1", alg: "RS256", use: "sig" });
  let tokenRequest;
  let pendingNonce;
  const fetchImpl = async (url, options = {}) => {
    if (url.endsWith("/jwks"))
      return { ok: true, json: async () => ({ keys: [jwk] }) };
    tokenRequest = String(options.body);
    return {
      ok: true,
      json: async () => ({
        id_token: jwt(
          privateKey,
          { alg: "RS256", kid: "key-1" },
          {
            iss: "https://identity.example.test",
            aud: "vastplan",
            sub: "alice-stable",
            nonce: pendingNonce,
            exp: Math.floor(now / 1000) + 300,
            iat: Math.floor(now / 1000),
            amr: ["mfa"],
            acr: "aal2",
          },
        ),
      }),
    };
  };
  const provider = new OIDCProvider(profiles, { fetchImpl, now: () => now });
  const begin = provider.begin({
    transactionId: "t".repeat(32),
    methodId: "oidc",
    providerProfileId: "corporate",
  });
  const redirect = new URL(begin.result.step.redirectUri);
  pendingNonce = redirect.searchParams.get("nonce");
  assert.equal(redirect.searchParams.get("code_challenge_method"), "S256");
  assert.equal(redirect.searchParams.get("client_id"), "vastplan");
  const completed = await provider.continue({
    transactionId: "t".repeat(32),
    stepId: begin.result.step.stepId,
    redirect: { code: "code-1", state: redirect.searchParams.get("state") },
  });
  assert.equal(completed.result.state, "authenticated");
  assert.deepEqual(completed.result.evidence.subject, {
    id: "alice-stable",
    issuer: "https://identity.example.test",
  });
  assert.match(tokenRequest, /code_verifier=/);
  assert.ok(!JSON.stringify(completed).includes("id_token"));
  const replay = await provider.continue({
    transactionId: "t".repeat(32),
    stepId: begin.result.step.stepId,
    redirect: { code: "code-1", state: redirect.searchParams.get("state") },
  });
  assert.equal(replay.result.state, "expired");
});

test("OIDC Provider rejects callback state mismatch before token exchange", async () => {
  let calls = 0;
  const provider = new OIDCProvider(profiles, {
    fetchImpl: async () => {
      calls += 1;
      throw new Error("must not call");
    },
    now: () => now,
  });
  const begin = provider.begin({
    transactionId: "x".repeat(32),
    methodId: "oidc",
    providerProfileId: "corporate",
  });
  const result = await provider.continue({
    transactionId: "x".repeat(32),
    stepId: begin.result.step.stepId,
    redirect: { code: "code", state: "wrong-state-value-that-is-long-enough" },
  });
  assert.equal(result.result.state, "rejected");
  assert.equal(calls, 0);
});

test("OIDC configuration rejects secrets and insecure endpoints", () => {
  assert.throws(() =>
    loadConfiguration(
      JSON.stringify({
        profiles: {
          bad: {
            issuer: "http://idp.test",
            clientId: "x",
            authorizationEndpoint: "https://idp.test/a",
            tokenEndpoint: "https://idp.test/t",
            jwksUri: "https://idp.test/j",
            redirectUri: "https://portal.test/c",
          },
        },
      }),
    ),
  );
  assert.throws(() =>
    loadConfiguration(
      JSON.stringify({
        profiles: {
          bad: {
            issuer: "https://idp.test",
            clientId: "x",
            clientSecret: "forbidden",
            authorizationEndpoint: "https://idp.test/a",
            tokenEndpoint: "https://idp.test/t",
            jwksUri: "https://idp.test/j",
            redirectUri: "https://portal.test/c",
          },
        },
      }),
    ),
  );
});

test("confidential OIDC obtains client secret only through Material Lease", async () => {
  const confidential = loadConfiguration(
    JSON.stringify({
      profiles: {
        corporate: {
          issuer: "https://identity.example.test",
          clientId: "vastplan",
          authorizationEndpoint: "https://identity.example.test/authorize",
          tokenEndpoint: "https://identity.example.test/token",
          jwksUri: "https://identity.example.test/jwks",
          redirectUri: "https://portal.example.test/auth/callback",
          clientSecretRef: {
            handle: "credential://managed/oidc",
            scope: "tenant",
            owner:
              "cn.vastplan.foundation.security.authentication.provider.oidc",
            purpose: "oidc.client-secret",
            version: 1,
          },
        },
      },
    }),
  );
  let tokenBody = "";
  let leaseCalls = 0;
  let material;
  const materialLease = {
    async withMaterial(ref, tenant, _signal, use) {
      leaseCalls += 1;
      assert.equal(ref.purpose, "oidc.client-secret");
      assert.equal(tenant, "acme");
      material = Buffer.from("leased-secret");
      try {
        return await use(material);
      } finally {
        material.fill(0);
      }
    },
  };
  const provider = new OIDCProvider(confidential, {
    now: () => now,
    materialLease,
    fetchImpl: async (_url, options) => {
      tokenBody = String(options.body);
      return { ok: true, json: async () => ({ id_token: "opaque" }) };
    },
    verifier: {
      async verify() {
        return {
          iss: "https://identity.example.test",
          sub: "alice",
          amr: ["mfa"],
          acr: "aal2",
        };
      },
    },
  });
  const begin = provider.begin({
    transactionId: "c".repeat(32),
    methodId: "oidc",
    providerProfileId: "corporate",
    tenantId: "acme",
  });
  const redirect = new URL(begin.result.step.redirectUri);
  const result = await provider.continue({
    transactionId: "c".repeat(32),
    stepId: begin.result.step.stepId,
    redirect: { code: "code", state: redirect.searchParams.get("state") },
  });
  assert.equal(result.result.state, "authenticated");
  assert.equal(leaseCalls, 1);
  assert.match(tokenBody, /client_secret=leased-secret/);
  assert.ok(material.every((byte) => byte === 0));
});
