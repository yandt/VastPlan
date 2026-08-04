import assert from "node:assert/strict";
import test from "node:test";

import { SharedProfileStateStore } from "../state-store.mjs";

class SharedStatePlugin {
  constructor() { this.entries = new Map(); }
  async call(target, context, payload) {
    const request = JSON.parse(payload.toString("utf8"));
    const operation = target.capability.split(".").at(-1);
    const tenant = context.tenant_id;
    const current = this.entries.get(tenant);
    if (operation === "get") {
      if (!current) return { result:{status:"STATUS_ERROR",error:{code:"state.not_found"}} };
      return ok(current);
    }
    if (operation === "create" && current || operation === "update" && request.expectedRevision !== current?.revision) {
      return { result:{status:"STATUS_ERROR",error:{code:"state.conflict",retryable:true}} };
    }
    const entry = { key:request.key, value:request.value, revision:(current?.revision ?? 0)+1, updatedAt:new Date().toISOString() };
    this.entries.set(tenant, entry);
    return ok(entry);
  }
}

const ok = (entry) => ({ result:{status:"STATUS_OK"}, payload:Buffer.from(JSON.stringify({protocol:"state.shared.v1",...entry})) });

test("Shared Profile State Store persists tenant-isolated CAS values", async () => {
  const plugin = new SharedStatePlugin();
  const store = new SharedProfileStateStore(plugin);
  const tenantA = {tenant_id:"tenant-a"}, tenantB = {tenant_id:"tenant-b"};
  assert.equal(await store.load(tenantA), undefined);
  await store.save({formatVersion:1,collectionId:"cfgc_"+"a".repeat(24),tenants:{}}, tenantA);
  assert.equal(plugin.entries.get("tenant-a").revision, 1);
  assert.equal(await store.load(tenantB), undefined);
  const restarted = new SharedProfileStateStore(plugin);
  assert.equal((await restarted.load(tenantA)).formatVersion, 1);
  await restarted.save({formatVersion:1,collectionId:"cfgc_"+"a".repeat(24),tenants:{}}, tenantA);
  assert.equal(plugin.entries.get("tenant-a").revision, 2);
  assert.equal(plugin.entries.has("tenant-b"), false);
});
