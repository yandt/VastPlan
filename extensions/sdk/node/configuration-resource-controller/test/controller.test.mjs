import assert from "node:assert/strict";
import test from "node:test";

import {
  configurationResourceCollectionId,
  configurationResourceControllerCapability,
  configurationResourceControllerContribution,
  deletedResourceDigest,
  prepareResourceRequestDigest,
  resourceConfigurationDigest,
  validateGetResponse,
} from "../src/index.mjs";

const prepare = {
  candidateId: `pcfg_${"a".repeat(32)}`,
  configurationId: `cfg_${"9".repeat(24)}`,
  collectionId: `cfgc_${"1".repeat(24)}`,
  resourceId: `cfgp_${"2".repeat(32)}`,
  action: "create",
  catalogDigest: "c".repeat(64), schemaDigest: "d".repeat(64), artifactSha256: "e".repeat(64),
  values: { endpoint: "https://delivery.example.test", displayName: "Enterprise Mail" },
  managedCredentials: { authorization: { handle: "credential://managed/opaque", scope: "tenant", owner: "cn.vastplan.demo", purpose: "demo.authorization", version: 1 } },
};

test("derives Go-compatible opaque identities and prepare digest", () => {
  assert.equal(configurationResourceControllerCapability("cn.vastplan.demo-resource-controller"), "configuration.resource.04542ba916aaa20c436092fd98f2bdbd");
  assert.equal(configurationResourceCollectionId("cn.vastplan.demo-resource-controller", "delivery-profile"), "cfgc_88a57d6eb44c31080cd5d2b9");
  assert.equal(prepareResourceRequestDigest(prepare), "c00b448b09303ea0d4764706a2e6e5ddae2b01fc17dfee9aed967596b8e5424d");
  assert.equal(resourceConfigurationDigest(prepare.values, prepare.managedCredentials), "13f3e6cda2ac6eb08697aec48ac96c4afeeec471891b6366e7677b93d22eed6d");
  assert.equal(deletedResourceDigest(prepare.resourceId), "8e4e709267c0aada3b915401047fcf63d7c3ca2a7f46590d80105eb7dbd3e679");
  assert.equal(prepareResourceRequestDigest({ ...prepare, values: { displayName: "Enterprise Mail", endpoint: "https://delivery.example.test" } }), prepareResourceRequestDigest(prepare));
});

test("query view rejects credential handles and reports only status", () => {
  const response = validateGetResponse({
    protocol: "configuration.resource.v1", collectionId: prepare.collectionId, observedAt: "2026-07-23T00:00:00Z",
    item: { resourceId: prepare.resourceId, active: { revision: 1, digest: "f".repeat(64) }, values: prepare.values, credentialStates: [{ fieldId: "authorization", configured: true, version: 1 }], updatedAt: "2026-07-23T00:00:00Z" },
  });
  assert.equal(JSON.stringify(response).includes("credential://"), false);
  assert.throws(() => validateGetResponse({ ...response, item: { ...response.item, credentialHandle: "credential://managed/leak" } }), /字段无效/);
});

test("contribution enforces the authenticated plugin-settings caller", async () => {
  const controller = {
    async list() { return { protocol: "configuration.resource.v1", collectionId: prepare.collectionId, items: [], observedAt: "2026-07-23T00:00:00Z" }; },
    async get() { throw new Error("unused"); }, async prepare() { throw new Error("unused"); }, async commit() { throw new Error("unused"); }, async abort() { throw new Error("unused"); }, async status() { throw new Error("unused"); },
  };
  const contribution = configurationResourceControllerContribution("cn.vastplan.demo-resource-controller", controller);
  const invocation = { throwIfCancelled() {} };
  const denied = await contribution.handlers.get("list")(invocation, {}, { tenant_id: "tenant-a", caller: { kind: "CALLER_KIND_USER", id: "alice" } }, Buffer.from(JSON.stringify({ collectionId: prepare.collectionId })));
  assert.equal(denied.result.error.code, "configuration.resource.permission_denied");
  const accepted = await contribution.handlers.get("list")(invocation, {}, { tenant_id: "tenant-a", caller: { kind: "CALLER_KIND_PLUGIN", id: "cn.vastplan.platform.configuration.plugin-settings" } }, Buffer.from(JSON.stringify({ collectionId: prepare.collectionId })));
  assert.equal(accepted.result.status, "STATUS_OK");
});
