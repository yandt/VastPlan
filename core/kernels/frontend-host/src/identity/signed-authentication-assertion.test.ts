import { generateKeyPairSync, randomBytes, sign } from "node:crypto";
import { mkdtemp, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { describe, expect, it } from "vitest";
import { AuthenticationAssertionVerifier, type AuthenticationAssertion } from "./signed-authentication-assertion";

describe("AuthenticationAssertionVerifier", () => {
  it("enforces clock skew, expiry and every Portal binding", async () => {
    const now = Date.now(), { verifier, privateKey } = await fixture(now);
    const base = assertion(now);
    expect(verifier.verify(signed(base, privateKey), expected(base)).payload.assertionId).toBe(base.assertionId);
    expect(() => verifier.verify(signed({ ...base, issuedAt:new Date(now + 5001).toISOString(), expiresAt:new Date(now + 25000).toISOString() }, privateKey), expected(base))).toThrow(/time|时间/i);
    expect(() => verifier.verify(signed({ ...base, issuedAt:new Date(now - 31000).toISOString(), expiresAt:new Date(now - 1000).toISOString() }, privateKey), expected(base))).toThrow(/time|时间/i);
    expect(() => verifier.verify(signed(base, privateKey), { ...expected(base), portalId:"forged" })).toThrow(/binding|绑定/i);
  });
});

async function fixture(now: number) {
  const root = await mkdtemp(join(tmpdir(), "vastplan-assertion-verifier-")), trust = join(root,"trust.json");
  const {publicKey,privateKey} = generateKeyPairSync("ed25519"), jwk = publicKey.export({format:"jwk"});
  await writeFile(trust,JSON.stringify({version:1,keys:[{keyId:"broker.1",publicKey:Buffer.from(jwk.x!,"base64url").toString("base64").replace(/=+$/,"")}]}),{mode:0o600});
  return {verifier:await AuthenticationAssertionVerifier.open(trust,()=>now),privateKey};
}
function assertion(now:number):AuthenticationAssertion { return {schemaVersion:"v1",assertionId:"assertion.12345678",transactionId:"t".repeat(32),providerId:"oidc",providerProfileId:"corporate",subject:{id:"alice",issuer:"https://identity.example"},tenantId:"tenant-a",portalId:"operations",audience:"portal:operations",amr:["oidc"],acr:"aal1",issuedAt:new Date(now-1000).toISOString(),expiresAt:new Date(now+29000).toISOString(),nonce:randomBytes(24).toString("base64url")}; }
function expected(value:AuthenticationAssertion){return {audience:value.audience,tenantId:value.tenantId,portalId:value.portalId,transactionId:value.transactionId};}
function signed(value:AuthenticationAssertion,privateKey:ReturnType<typeof generateKeyPairSync>["privateKey"]){const canonical=JSON.stringify({schemaVersion:value.schemaVersion,assertionId:value.assertionId,transactionId:value.transactionId,providerId:value.providerId,providerProfileId:value.providerProfileId,subject:{id:value.subject.id,issuer:value.subject.issuer},tenantId:value.tenantId,portalId:value.portalId,audience:value.audience,amr:[...value.amr].sort(),acr:value.acr,issuedAt:value.issuedAt,expiresAt:value.expiresAt,nonce:value.nonce});return {payload:value,signature:{algorithm:"Ed25519" as const,keyId:"broker.1",value:sign(null,Buffer.from(canonical),privateKey).toString("base64url")}};}
