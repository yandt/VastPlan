import {
  CONFIGURATION_RESOURCE_PROTOCOL,
} from "@vastplan/configuration-resource-controller-node";

import { normalizeAuthorizationRef, normalizeProfile, publicProfileValues } from "./config.mjs";

export const stateFormatVersion = 1;

export function emptyState(collectionId) {
  return { formatVersion: stateFormatVersion, collectionId, tenants: {} };
}

export function validateState(value, collectionId) {
	closed(value, ["formatVersion", "collectionId", "tenants"], "Webhook Profile state");
  if (!value || value.formatVersion !== stateFormatVersion || value.collectionId !== collectionId || !value.tenants || typeof value.tenants !== "object" || Array.isArray(value.tenants)) throw new Error("Webhook Profile 状态身份无效");
  for (const [tenantId, state] of Object.entries(value.tenants)) {
		closed(state, ["items", "candidates"], "Webhook Profile tenant state");
    if (!safeTenant(tenantId) || !state || typeof state !== "object" || !state.items || !state.candidates || Array.isArray(state.items) || Array.isArray(state.candidates)) throw new Error("Webhook Profile tenant 状态无效");
    if (Object.keys(state.items).length > 64 || Object.keys(state.candidates).length > 2048) throw new Error("Webhook Profile 状态数量超限");
    for (const [resourceId, item] of Object.entries(state.items)) validateItem(resourceId, item);
    for (const [candidateId, candidate] of Object.entries(state.candidates)) validateCandidate(candidateId, candidate, state.items);
  }
  return value;
}

export function tenantState(state, tenantId) {
  if (!safeTenant(tenantId)) throw new Error("Webhook Profile 缺少可信 tenant");
  state.tenants[tenantId] ??= { items: {}, candidates: {} };
  return state.tenants[tenantId];
}

export function cloneState(state) { return structuredClone(state); }
export function profileMapKey(tenantId, resourceId) { return `${tenantId}\0${resourceId}`; }

export function synchronizeProfiles(state, profiles) {
  profiles.clear();
  for (const [tenantId, tenant] of Object.entries(state.tenants)) {
    for (const [resourceId, item] of Object.entries(tenant.items)) {
      profiles.set(profileMapKey(tenantId, resourceId), normalizeProfile(resourceId, item.values, item.managedCredentials));
    }
  }
}

export function resourceView(resourceId, item, now = item.updatedAt) {
  const profile = normalizeProfile(resourceId, item.values, item.managedCredentials);
  return {
    resourceId,
    active: { revision: item.revision, digest: item.digest },
    values: publicProfileValues(profile),
    credentialStates: Object.entries(item.managedCredentials).sort(([left], [right]) => left.localeCompare(right)).map(([fieldId, ref]) => ({ fieldId, configured: true, version: ref.version })),
    updatedAt: now,
  };
}

export function observation(collectionId, resourceId, item, candidate, now) {
  return {
    protocol: CONFIGURATION_RESOURCE_PROTOCOL,
    collectionId,
    resourceId,
    ...(item ? { active: { revision: item.revision, digest: item.digest } } : {}),
    ...(candidate ? { candidate: {
      candidateId: candidate.candidateId, requestDigest: candidate.requestDigest, resultDigest: candidate.resultDigest,
      action: candidate.action, status: candidate.status, ready: candidate.ready,
      ...(candidate.errorCode ? { errorCode: candidate.errorCode } : {}),
      ...(candidate.errorMessage ? { errorMessage: candidate.errorMessage } : {}),
    } } : {}),
    observedAt: now,
  };
}

function validateItem(resourceId, item) {
	closed(item, ["revision", "digest", "values", "managedCredentials", "updatedAt"], "Webhook Profile item");
  if (!/^cfgp_[a-f0-9]{32}$/.test(resourceId) || !item || !Number.isSafeInteger(item.revision) || item.revision < 1 || !/^[a-f0-9]{64}$/.test(item.digest) || typeof item.updatedAt !== "string") throw new Error("Webhook Profile Active 状态无效");
  normalizeProfile(resourceId, item.values, item.managedCredentials);
}

function validateCandidate(candidateId, candidate, items) {
	closed(candidate, ["candidateId", "resourceId", "requestDigest", "resultDigest", "action", "status", "ready", "values", "managedCredentials", "retirePending", "createdAt", "updatedAt", "errorCode", "errorMessage"], "Webhook Profile candidate");
  if (!/^pcfg_[a-f0-9]{32}$/.test(candidateId) || candidate?.candidateId !== candidateId || !/^cfgp_[a-f0-9]{32}$/.test(candidate.resourceId) ||
      !/^[a-f0-9]{64}$/.test(candidate.requestDigest) || !/^[a-f0-9]{64}$/.test(candidate.resultDigest) ||
      !["create", "update", "delete"].includes(candidate.action) || !["Prepared", "Committed", "Aborted"].includes(candidate.status) || typeof candidate.ready !== "boolean") {
    throw new Error("Webhook Profile Candidate 状态无效");
  }
  if (candidate.action !== "delete") normalizeProfile(candidate.resourceId, candidate.values, candidate.managedCredentials);
	if (!Array.isArray(candidate.retirePending) || candidate.retirePending.length > 1) throw new Error("Webhook Profile Candidate 退役引用无效");
	for (const ref of candidate.retirePending) normalizeAuthorizationRef(ref, candidate.resourceId);
  if (candidate.status === "Committed" && ((candidate.action === "delete") === Boolean(items[candidate.resourceId]))) throw new Error("Webhook Profile Committed Candidate 未成为目标 Active 状态");
  if (candidate.status === "Aborted" && candidate.ready) throw new Error("Webhook Profile Aborted Candidate 不得 Ready");
}

function safeTenant(value) { return typeof value === "string" && /^[A-Za-z0-9][A-Za-z0-9._:@/-]{0,255}$/.test(value); }

function closed(value, allowed, name) {
	if (!value || typeof value !== "object" || Array.isArray(value) || Object.keys(value).some((key) => !allowed.includes(key))) throw new Error(`${name} 字段无效`);
}
