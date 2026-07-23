import assert from "node:assert/strict";
import test from "node:test";
import { SharedStateClient, isSharedStateConflict } from "../src/index.mjs";

test("uses identity-free kernel services and decodes an immutable entry", async () => {
  const calls = [];
  const plugin = { call: async (...args) => {
    calls.push(args);
    return { result: { status: "STATUS_OK" }, payload: Buffer.from(JSON.stringify({ protocol: "state.shared.v1", key: "active", value: "e30", revision: 1, updatedAt: "2026-07-23T00:00:00Z" })) };
  } };
  const client = new SharedStateClient(plugin, { scope: "tenant", namespace: "settings" });
  const entry = await client.create({ tenant_id: "trusted" }, "active", Buffer.from("{}"));
  assert.equal(entry.value.toString(), "{}");
  assert.equal(calls[0][0].capability, "kernel.state.shared.create");
  const request = JSON.parse(calls[0][2]);
  assert.deepEqual(Object.keys(request).sort(), ["key", "namespace", "scope", "value"]);
  assert.equal(Object.isFrozen(entry), true);
});

test("preserves stable conflict errors", async () => {
  const plugin = { call: async () => ({ result: { status: "STATUS_ERROR", error: { code: "state.conflict", message: "stale", retryable: true } } }) };
  const client = new SharedStateClient(plugin, { scope: "service", namespace: "ledger" });
  await assert.rejects(() => client.update({}, "active", Buffer.from("{}"), 1), isSharedStateConflict);
});
