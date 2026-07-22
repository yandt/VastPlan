import assert from "node:assert/strict";
import {
  createCipheriv,
  createHmac,
  createPublicKey,
  diffieHellman,
  generateKeyPairSync,
  randomBytes,
} from "node:crypto";
import test from "node:test";

import { MaterialLeaseClient } from "../src/index.mjs";

const prefix = Buffer.from("302a300506032b656e032100", "hex");
const ref = {
  handle: "credential://managed/oidc",
  scope: "tenant",
  owner: "cn.vastplan.foundation.security.authentication.provider.oidc",
  purpose: "oidc.client-secret",
  version: 1,
};

function seal(request, now) {
  const recipientRaw = Buffer.from(request.recipientPublicKey, "base64url");
  const recipient = createPublicKey({
    key: Buffer.concat([prefix, recipientRaw]),
    format: "der",
    type: "spki",
  });
  const sender = generateKeyPairSync("x25519");
  const shared = diffieHellman({
    privateKey: sender.privateKey,
    publicKey: recipient,
  });
  const salt = randomBytes(32);
  const prk = createHmac("sha256", salt).update(shared).digest();
  const key = createHmac("sha256", prk)
    .update("vastplan/material-lease/v1")
    .update(Buffer.from([1]))
    .digest();
  const nonce = randomBytes(12);
  const senderDer = sender.publicKey.export({ format: "der", type: "spki" });
  const envelope = {
    version: 1,
    leaseId: "lease-test",
    tenantId: "acme",
    audience: "node-runtime.test",
    ref,
    issuedAtUnixMs: now,
    expiresAtUnixMs: now + 15_000,
    senderPublicKey: senderDer
      .subarray(senderDer.length - 32)
      .toString("base64url"),
    salt: salt.toString("base64url"),
    nonce: nonce.toString("base64url"),
  };
  const aad = Buffer.from(
    JSON.stringify({
      version: envelope.version,
      leaseId: envelope.leaseId,
      tenantId: envelope.tenantId,
      audience: envelope.audience,
      ref: envelope.ref,
      issuedAtUnixMs: envelope.issuedAtUnixMs,
      expiresAtUnixMs: envelope.expiresAtUnixMs,
    }),
  );
  const cipher = createCipheriv("aes-256-gcm", key, nonce);
  cipher.setAAD(aad);
  const encrypted = Buffer.concat([
    cipher.update("confidential-secret"),
    cipher.final(),
    cipher.getAuthTag(),
  ]);
  envelope.ciphertext = encrypted.toString("base64url");
  return envelope;
}

test("Node Material Lease opens a bound envelope and clears the callback buffer", async () => {
  const now = Date.UTC(2026, 6, 23);
  let material;
  const plugin = {
    async call(_target, context, payload) {
      assert.equal(context.tenant_id, "acme");
      const request = JSON.parse(Buffer.from(payload).toString());
      return {
        result: { status: "STATUS_OK" },
        payload: Buffer.from(JSON.stringify(seal(request, now))),
      };
    },
  };
  const client = new MaterialLeaseClient(plugin, {
    audience: "node-runtime.test",
    now: () => now,
  });
  const value = await client.withMaterial(
    ref,
    "acme",
    undefined,
    async (secret) => {
      material = secret;
      return secret.toString();
    },
  );
  assert.equal(value, "confidential-secret");
  assert.ok(material.every((byte) => byte === 0));
});

test("Node Material Lease rejects audience tampering", async () => {
  const now = Date.UTC(2026, 6, 23);
  const plugin = {
    async call(_target, _context, payload) {
      const envelope = seal(JSON.parse(Buffer.from(payload).toString()), now);
      envelope.audience = "attacker";
      return {
        result: { status: "STATUS_OK" },
        payload: Buffer.from(JSON.stringify(envelope)),
      };
    },
  };
  const client = new MaterialLeaseClient(plugin, {
    audience: "node-runtime.test",
    now: () => now,
  });
  await assert.rejects(
    () => client.withMaterial(ref, "acme", undefined, async () => {}),
    /claims/,
  );
});
