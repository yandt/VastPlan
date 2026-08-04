import { SharedStateClient, isSharedStateNotFound, MAX_SHARED_STATE_VALUE_BYTES } from "@vastplan/shared-state";

const namespace = "authentication.delivery.webhook.profiles.v1";
const stateKey = "state";

export class SharedProfileStateStore {
  constructor(plugin) {
    this.client = new SharedStateClient(plugin, { scope: "tenant", namespace, fenced: true });
    this.revisions = new Map();
  }

  async load(callContext) {
    const tenantId = trustedTenant(callContext);
    try {
      const entry = await this.client.get(callContext, stateKey);
      this.revisions.set(tenantId, entry.revision);
      return JSON.parse(entry.value.toString("utf8"));
    } catch (error) {
      if (!isSharedStateNotFound(error)) throw error;
      this.revisions.set(tenantId, 0);
      return undefined;
    }
  }

  async save(state, callContext) {
    const tenantId = trustedTenant(callContext);
    const raw = Buffer.from(JSON.stringify(state));
    if (raw.length < 2 || raw.length > MAX_SHARED_STATE_VALUE_BYTES) throw new Error("Webhook Profile Shared State 超过大小上限");
    try {
      const revision = this.revisions.get(tenantId) ?? 0;
      const entry = revision === 0
        ? await this.client.create(callContext, stateKey, raw)
        : await this.client.update(callContext, stateKey, raw, revision);
      this.revisions.set(tenantId, entry.revision);
    } finally {
      raw.fill(0);
    }
  }
}

function trustedTenant(callContext) {
  const tenantId = callContext?.tenant_id;
  if (typeof tenantId !== "string" || !/^[A-Za-z0-9][A-Za-z0-9._:@/-]{0,255}$/.test(tenantId)) throw new Error("Webhook Profile 缺少可信 tenant");
  return tenantId;
}
