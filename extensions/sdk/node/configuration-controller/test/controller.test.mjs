import assert from "node:assert/strict";
import test from "node:test";

import {
  configurationControllerCapability,
  configurationControllerContribution,
  configurationDigest,
  mergeManagedCredentials,
  replacedManagedCredentials,
  validateObservation,
  prepareRequestDigest,
} from "../src/index.mjs";

const request = {
  candidateId: `pcfg_${"a".repeat(32)}`,
  configurationId: `cfg_${"9".repeat(24)}`,
  catalogDigest: "c".repeat(64), schemaDigest: "d".repeat(64), artifactSha256: "e".repeat(64),
  expectedActive: { revision: 1, digest: "f".repeat(64) }, values: { capacity: 10 },
  managedCredentials: { token: { handle: "credential://managed/opaque", scope: "tenant", owner: "cn.vastplan.demo", purpose: "demo.token", version: 1 } },
};

test("derives an opaque controller capability and stable normalized digests", () => {
  assert.equal(configurationControllerCapability("cn.vastplan.example.hot-controller"), "configuration.3c183e12decc8e57e3ea513837dc8708");
  assert.equal(prepareRequestDigest(request), "1f14aa9cf75025b0480065230c7ae1c34f81117072b846a07b8619df6323c744");
  assert.equal(configurationDigest(request.values, request.managedCredentials), "0f4e0e9504882ed26dffe20c7d6cb101c7a4178dc73d7f58b1d1fd0a59d40210");
  assert.equal(prepareRequestDigest({ ...request, values: { z: true, capacity: 10 } }), prepareRequestDigest({ ...request, values: { capacity: 10, z: true } }));
  assert.equal(configurationDigest({ b: 2, a: 1 }), configurationDigest({ a: 1, b: 2 }));
});

test("retains omitted managed credentials and selects only replaced refs for retirement", () => {
  const retained = { handle: "credential://managed/retained", scope: "tenant", owner: "cn.vastplan.demo", purpose: "demo.token", version: 1 };
  const oldRef = { ...retained, handle: "credential://managed/old" };
  const newRef = { ...retained, handle: "credential://managed/new", version: 2 };
  const merged = mergeManagedCredentials({ retained, token: oldRef }, { token: newRef });
  assert.equal(merged.retained.handle, retained.handle);
  assert.equal(merged.token.version, 2);
  assert.deepEqual(replacedManagedCredentials({ retained, token: oldRef }, merged), [oldRef]);
});

test("requires an RFC 3339 observation timestamp", () => {
  const base = {
    protocol: "configuration.v1",
    configurationId: request.configurationId,
    active: request.expectedActive,
    observedAt: "2026-07-23T00:00:00Z",
  };
  assert.equal(validateObservation(base).observedAt, base.observedAt);
  assert.throws(() => validateObservation({ ...base, observedAt: "2026-07-23" }), /observation/);
});

test("contribution rejects direct users and returns digest-only observations", async () => {
  const controller = {
    async prepare(value) {
      return {
        protocol: "configuration.v1", configurationId: value.configurationId, active: value.expectedActive,
        candidate: { candidateId: value.candidateId, requestDigest: prepareRequestDigest(value), configurationDigest: configurationDigest(value.values, value.managedCredentials), status: "Prepared", ready: true },
        observedAt: "2026-07-23T00:00:00Z",
      };
    },
    async commit() { throw new Error("unused"); }, async abort() { throw new Error("unused"); }, async status() { throw new Error("unused"); },
  };
  const contribution = configurationControllerContribution("cn.vastplan.example.hot-controller", controller);
  const invocation = { throwIfCancelled() {} };
  const denied = await contribution.handlers.get("prepare")(invocation, {}, { tenant_id: "tenant-a", caller: { kind: "CALLER_KIND_USER", id: "alice" } }, Buffer.from(JSON.stringify(request)));
  assert.equal(denied.result.error.code, "configuration.controller.permission_denied");
  const accepted = await contribution.handlers.get("prepare")(invocation, {}, { tenant_id: "tenant-a", caller: { kind: "CALLER_KIND_PLUGIN", id: "cn.vastplan.platform.configuration.plugin-settings" } }, Buffer.from(JSON.stringify(request)));
  assert.equal(accepted.result.status, "STATUS_OK");
  const observation = JSON.parse(accepted.payload);
  assert.equal(observation.candidate.status, "Prepared");
  assert.equal(JSON.stringify(observation).includes("credential://"), false);
});
