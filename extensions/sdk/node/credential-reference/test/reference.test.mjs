import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import { normalizeManagedCredentialRef, normalizeManagedCredentialRefs, sameManagedCredentialRef } from "../src/index.mjs";

const vector = JSON.parse(readFileSync(new URL("../../../../../contracts/testdata/sdk-interop-v1.json", import.meta.url), "utf8"));
const valid = vector.managedCredentialRef;

test("normalizes a closed immutable Managed CredentialRef", () => {
  const ref = normalizeManagedCredentialRef(valid);
  assert.equal(Object.isFrozen(ref), true);
  assert.deepEqual(ref, valid);
  assert.equal(sameManagedCredentialRef(ref, { ...valid }), true);
});

test("rejects unknown, malformed and disallowed references", () => {
  assert.throws(() => normalizeManagedCredentialRef({ ...valid, plaintext: "secret" }), /字段无效/);
  assert.throws(() => normalizeManagedCredentialRef({ ...valid, handle: "credential://named/demo" }), /无效/);
  assert.throws(() => normalizeManagedCredentialRef({ ...valid, scope: "service" }, { allowedScopes: ["tenant"] }), /无效/);
  assert.throws(() => normalizeManagedCredentialRefs({ "Bad Field": valid }), /字段/);
});
