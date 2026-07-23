import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import { ScopedConfigurationClient, digestScopedValues, parseScopedResolution } from "../src/index.mjs";

const id = `cfg_${"a".repeat(24)}`;
const vector = JSON.parse(readFileSync(new URL("../../../../../contracts/testdata/sdk-interop-v1.json", import.meta.url), "utf8"));
const values = vector.scopedValues;
assert.equal(digestScopedValues(values), vector.scopedValuesDigest);
const resolution = {
  protocol: "configuration.scoped.v1", configurationId: id, scope: "tenant", revision: 0,
  digest: digestScopedValues(values), schemaDigest: "b".repeat(64), artifactSha256: "c".repeat(64),
  values, source: "seed", observedAt: "2026-07-23T00:00:00Z",
};

test("strictly validates and freezes a scoped resolution", () => {
  const parsed = parseScopedResolution(Buffer.from(JSON.stringify(resolution)));
  assert.equal(Object.isFrozen(parsed.values), true);
  assert.equal(parsed.digest, vector.scopedValuesDigest);
  assert.throws(() => parseScopedResolution({ ...resolution, tenantId: "forged" }), /字段/);
  assert.throws(() => parseScopedResolution({ ...resolution, digest: "d".repeat(64) }), /无效/);
});

test("calls only the identity-free scoped resolver operations", async () => {
  const calls = [];
  const plugin = { async call(target, context, payload, timeout) {
    calls.push({ target, context, payload: payload.toString(), timeout });
    const body = target.operation === "resolve" ? resolution : {
      protocol: "configuration.scoped.v1", configurationId: id, changed: false, revision: 0,
      digest: resolution.digest, observedAt: "2026-07-23T00:00:01Z",
    };
    return { result: { status: "STATUS_OK" }, payload: Buffer.from(JSON.stringify(body)) };
  } };
  const client = new ScopedConfigurationClient(plugin);
  await client.resolve({ tenant_id: "trusted-context" });
  await client.watchRevision({ tenant_id: "trusted-context" }, { afterRevision: 0, afterDigest: resolution.digest, timeoutMs: 1000 });
  assert.deepEqual(calls.map((call) => call.target), [
    { extension_point: "configuration.scoped-resolver", capability: "configuration.scoped", operation: "resolve" },
    { extension_point: "configuration.scoped-resolver", capability: "configuration.scoped", operation: "watchRevision" },
  ]);
  assert.equal(calls[0].payload, "{}");
  assert.equal(calls[1].payload.includes("tenant"), false);
});
