import {
  CONFIGURATION_RESOURCE_PROTOCOL,
  deletedResourceDigest,
  prepareResourceRequestDigest,
  resourceConfigurationDigest,
} from "@vastplan/configuration-resource-controller-node";

import { normalizeProfile, publicProfileValues } from "./config.mjs";
import { cloneState, emptyState, observation, profileMapKey, resourceView, synchronizeProfiles, tenantState, validateState } from "./profile-state.mjs";

const maximumProfiles = 64;
const minimumProfiles = 1;

export class WebhookProfileController {
  constructor({ collectionId, store, materialLease, profiles = new Map(), now = () => new Date().toISOString() }) {
    if (!/^cfgc_[a-f0-9]{24}$/.test(collectionId) || !store || !materialLease) throw new Error("Webhook Profile Controller 依赖无效");
    this.collectionId = collectionId;
    this.store = store;
    this.materialLease = materialLease;
    this.profiles = profiles;
    this.now = now;
    this.state = validateState(store.load() ?? emptyState(collectionId), collectionId);
    synchronizeProfiles(this.state, this.profiles);
    this.tail = Promise.resolve();
  }

  list(request, runtime) {
    return this.#serial(async () => {
      this.#collection(request.collectionId);
      const tenant = tenantState(this.state, runtime.context.tenant_id);
      const ids = Object.keys(tenant.items).sort();
      const start = request.cursor === undefined ? 0 : ids.findIndex((id) => id > request.cursor);
      const offset = start < 0 ? ids.length : start;
      const limit = request.limit ?? maximumProfiles;
      const selected = ids.slice(offset, offset + limit);
      return { protocol: CONFIGURATION_RESOURCE_PROTOCOL, collectionId: this.collectionId, items: selected.map((id) => resourceView(id, tenant.items[id])), ...(offset + limit < ids.length ? { nextCursor: selected.at(-1) } : {}), observedAt: this.now() };
    });
  }

  get(request, runtime) {
    return this.#serial(async () => {
      this.#collection(request.collectionId);
      const item = tenantState(this.state, runtime.context.tenant_id).items[request.resourceId];
      if (!item) throw new Error("Webhook Delivery Profile 不存在");
      return { protocol: CONFIGURATION_RESOURCE_PROTOCOL, collectionId: this.collectionId, item: resourceView(request.resourceId, item), observedAt: this.now() };
    });
  }

  prepare(request, runtime) {
    return this.#serial(async () => {
      this.#collection(request.collectionId);
      const tenantId = runtime.context.tenant_id;
      const tenant = tenantState(this.state, tenantId);
      const requestDigest = prepareResourceRequestDigest(request);
      const existing = tenant.candidates[request.candidateId];
      if (existing) {
        if (existing.requestDigest !== requestDigest) throw new Error("Webhook Profile Candidate 摘要冲突");
        return observation(this.collectionId, request.resourceId, tenant.items[request.resourceId], existing, this.now());
      }
      if (Object.values(tenant.candidates).some((candidate) => candidate.resourceId === request.resourceId && candidate.status === "Prepared")) throw new Error("Webhook Profile 已有待提交 Candidate");
      const active = tenant.items[request.resourceId];
      this.#activeCAS(request, active, Object.keys(tenant.items).length);
      const managedCredentials = request.action === "delete" ? {} : { ...(active?.managedCredentials ?? {}), ...(request.managedCredentials ?? {}) };
      const profile = request.action === "delete" ? undefined : normalizeProfile(request.resourceId, request.values, managedCredentials);
      if (profile) await this.#probeCredential(profile.authorizationRef, tenantId, runtime.invocation.signal.signal);
      const resultDigest = request.action === "delete" ? deletedResourceDigest(request.resourceId) : resourceConfigurationDigest(publicProfileValues(profile), managedCredentials);
      const retirePending = active ? Object.values(active.managedCredentials).filter((ref) => request.action === "delete" || managedCredentials.authorization.handle !== ref.handle) : [];
      const candidate = {
        candidateId: request.candidateId, resourceId: request.resourceId, requestDigest, resultDigest, action: request.action,
        status: "Prepared", ready: true, ...(profile ? { values: publicProfileValues(profile), managedCredentials } : {}),
        retirePending, createdAt: this.now(), updatedAt: this.now(),
      };
      const previous = cloneState(this.state);
      tenant.candidates[request.candidateId] = candidate;
      try { this.store.save(this.state); } catch (error) { this.state = previous; throw error; }
      return observation(this.collectionId, request.resourceId, active, candidate, this.now());
    });
  }

  commit(request, runtime) {
    return this.#serial(async () => {
      const tenantId = runtime.context.tenant_id;
      const tenant = tenantState(this.state, tenantId);
      const candidate = this.#candidate(tenant, request);
      if (candidate.status === "Aborted") throw new Error("已终止 Webhook Profile Candidate 不得提交");
      if (candidate.status === "Prepared") {
        const previous = cloneState(this.state);
        if (candidate.action === "delete") delete tenant.items[candidate.resourceId];
        else {
          const current = tenant.items[candidate.resourceId];
          tenant.items[candidate.resourceId] = { revision: (current?.revision ?? 0) + 1, digest: candidate.resultDigest, values: candidate.values, managedCredentials: candidate.managedCredentials, updatedAt: this.now() };
        }
        candidate.status = "Committed";
        candidate.updatedAt = this.now();
        try { this.store.save(this.state); } catch (error) { this.state = previous; throw error; }
        synchronizeProfiles(this.state, this.profiles);
      }
      await this.#retireBestEffort(tenantId, candidate, runtime.host);
      return observation(this.collectionId, candidate.resourceId, tenant.items[candidate.resourceId], candidate, this.now());
    });
  }

  abort(request, runtime) {
    return this.#serial(async () => {
      const tenant = tenantState(this.state, runtime.context.tenant_id);
      const candidate = this.#candidate(tenant, request);
      if (candidate.status === "Committed") throw new Error("已提交 Webhook Profile Candidate 不得终止");
      if (candidate.status === "Prepared") {
        const previous = cloneState(this.state);
        candidate.status = "Aborted";
        candidate.ready = false;
        candidate.updatedAt = this.now();
        try { this.store.save(this.state); } catch (error) { this.state = previous; throw error; }
      }
      return observation(this.collectionId, candidate.resourceId, tenant.items[candidate.resourceId], candidate, this.now());
    });
  }

  status(request, runtime) {
    return this.#serial(async () => {
      this.#collection(request.collectionId);
      const tenantId = runtime.context.tenant_id;
      const tenant = tenantState(this.state, tenantId);
      let candidate;
      if (request.candidateId) {
        candidate = this.#candidate(tenant, request);
        if (candidate.resourceId !== request.resourceId) throw new Error("Webhook Profile Candidate 资源不匹配");
        if (candidate.status === "Committed") await this.#retireBestEffort(tenantId, candidate, runtime.host);
      }
      return observation(this.collectionId, request.resourceId, tenant.items[request.resourceId], candidate, this.now());
    });
  }

  health() {
    let profiles = 0, retirementPending = 0;
    for (const tenant of Object.values(this.state.tenants)) {
      profiles += Object.keys(tenant.items).length;
      for (const candidate of Object.values(tenant.candidates)) retirementPending += candidate.retirePending?.length ?? 0;
    }
    return { ready: profiles > 0, profiles, retirementPending };
  }

  #activeCAS(request, active, count) {
    if (request.action === "create") {
      if (active || count >= maximumProfiles) throw new Error("Webhook Profile create CAS 冲突或数量已满");
      return;
    }
    if (!active || !request.expectedActive || active.revision !== request.expectedActive.revision || active.digest !== request.expectedActive.digest) throw new Error("Webhook Profile Active CAS 冲突");
    if (request.action === "delete" && count <= minimumProfiles) throw new Error("Webhook Profile 至少保留一个 Active Profile");
  }

  #candidate(tenant, request) {
    const candidate = tenant.candidates[request.candidateId];
    if (!candidate || candidate.requestDigest !== request.requestDigest) throw new Error("Webhook Profile Candidate 不存在或摘要不匹配");
    return candidate;
  }

  async #probeCredential(ref, tenantId, signal) {
    await this.materialLease.withMaterial(ref, tenantId, signal, async (material) => {
      if (material.length < 16 || material.length > 4096 || material.includes(10) || material.includes(13)) throw new Error("Webhook authorization material 无效");
    });
  }

  async #retireBestEffort(tenantId, candidate, host) {
    if (!candidate.retirePending?.length || !host) return;
    const pending = [];
    for (const ref of candidate.retirePending) {
      try {
        const response = await host.call({ extension_point: "tool.package", capability: "platform.credentials", operation: "retireManaged", logical_service: "platform.credentials", routing_domain: "platform" }, { tenant_id: tenantId }, Buffer.from(JSON.stringify({ handle: ref.handle })));
        if (response?.result?.status !== "STATUS_OK") throw new Error("凭证退役被拒绝");
      } catch { pending.push(ref); }
    }
    if (pending.length !== candidate.retirePending.length) {
      candidate.retirePending = pending;
      candidate.updatedAt = this.now();
      this.store.save(this.state);
    }
  }

  #collection(value) { if (value !== this.collectionId) throw new Error("Webhook Profile 集合不匹配"); }

  #serial(task) {
    const result = this.tail.then(task, task);
    this.tail = result.then(() => undefined, () => undefined);
    return result;
  }
}

export function resolveProfile(profiles, tenantId, resourceId) {
  return profiles.get(profileMapKey(tenantId, resourceId));
}
