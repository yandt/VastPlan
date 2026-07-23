import { activeReference, boundedString, exactKeys, id, normalizeJSON, record, rfc3339 } from "./json.mjs";
import { CONFIGURATION_RESOURCE_PROTOCOL } from "./identities.mjs";

export function validateResourceControllerResponse(operation, value) {
  if (operation === "list") return validateListResponse(value);
  if (operation === "get") return validateGetResponse(value);
  return validateObservation(value);
}

export function validateListResponse(value) {
  const response = record(value, "resource list response");
  exactKeys(response, ["protocol", "collectionId", "items", "nextCursor", "observedAt"], "list response", ["protocol", "collectionId", "items", "observedAt"]);
  if (response.protocol !== CONFIGURATION_RESOURCE_PROTOCOL || !Array.isArray(response.items) || response.items.length > 256) throw new Error("resource list response 无效");
  const items = response.items.map(resourceView);
  if (new Set(items.map((item) => item.resourceId)).size !== items.length) throw new Error("resource list 返回重复资源");
  if (response.nextCursor !== undefined && (typeof response.nextCursor !== "string" || response.nextCursor.length < 1 || response.nextCursor.length > 512)) throw new Error("nextCursor 无效");
  return Object.freeze({ protocol: CONFIGURATION_RESOURCE_PROTOCOL, collectionId: id(response.collectionId, "collection"), items: Object.freeze(items), ...(response.nextCursor === undefined ? {} : { nextCursor: response.nextCursor }), observedAt: rfc3339(response.observedAt, "observedAt") });
}

export function validateGetResponse(value) {
  const response = record(value, "resource get response");
  exactKeys(response, ["protocol", "collectionId", "item", "observedAt"], "get response");
  if (response.protocol !== CONFIGURATION_RESOURCE_PROTOCOL) throw new Error("resource get response 无效");
  return Object.freeze({ protocol: CONFIGURATION_RESOURCE_PROTOCOL, collectionId: id(response.collectionId, "collection"), item: resourceView(response.item), observedAt: rfc3339(response.observedAt, "observedAt") });
}

export function validateObservation(value) {
  const observation = record(value, "resource observation");
  exactKeys(observation, ["protocol", "collectionId", "resourceId", "active", "candidate", "observedAt"], "observation", ["protocol", "collectionId", "resourceId", "observedAt"]);
  if (observation.protocol !== CONFIGURATION_RESOURCE_PROTOCOL) throw new Error("resource observation 协议无效");
  const normalized = Object.freeze({
    protocol: CONFIGURATION_RESOURCE_PROTOCOL, collectionId: id(observation.collectionId, "collection"), resourceId: id(observation.resourceId, "resource"),
    ...(observation.active === undefined ? {} : { active: activeReference(observation.active) }),
    ...(observation.candidate === undefined ? {} : { candidate: candidateObservation(observation.candidate) }),
    observedAt: rfc3339(observation.observedAt, "observedAt"),
  });
  const candidate = normalized.candidate;
  if (!candidate) return normalized;
  if (candidate.status === "Prepared" && !candidate.ready) throw new Error("Prepared resource candidate 尚未 Ready");
  if (candidate.status === "Committed" && (!candidate.ready || ((candidate.action === "delete") !== (normalized.active === undefined)) || (normalized.active && normalized.active.digest !== candidate.resultDigest))) throw new Error("Committed resource candidate 未成为目标 Active 状态");
  if (candidate.status === "Aborted" && candidate.ready) throw new Error("Aborted resource candidate 不得 Ready");
  return normalized;
}

function resourceView(value) {
  const item = record(value, "resource view");
  exactKeys(item, ["resourceId", "active", "values", "credentialStates", "updatedAt"], "resource view", ["resourceId", "active", "values", "updatedAt"]);
  const values = normalizeJSON(record(item.values, "resource values"));
  if (Buffer.byteLength(JSON.stringify(values)) > 64 << 10) throw new Error("resource values 大小无效");
  const states = item.credentialStates === undefined ? [] : item.credentialStates.map(credentialState);
  if (states.length > 64 || new Set(states.map((state) => state.fieldId)).size !== states.length) throw new Error("credentialStates 无效");
  return Object.freeze({ resourceId: id(item.resourceId, "resource"), active: activeReference(item.active), values, ...(states.length === 0 ? {} : { credentialStates: Object.freeze(states) }), updatedAt: rfc3339(item.updatedAt, "updatedAt") });
}

function credentialState(value) {
  const state = record(value, "credential state");
  exactKeys(state, ["fieldId", "configured", "version"], "credential state", ["fieldId", "configured"]);
  if (typeof state.configured !== "boolean" || state.configured !== (Number.isSafeInteger(state.version) && state.version >= 1)) throw new Error("credential state 无效");
  return Object.freeze({ fieldId: id(state.fieldId, "field"), configured: state.configured, ...(state.version === undefined ? {} : { version: state.version }) });
}

function candidateObservation(value) {
  const candidate = record(value, "resource candidate observation");
  exactKeys(candidate, ["candidateId", "requestDigest", "resultDigest", "action", "status", "ready", "errorCode", "errorMessage"], "candidate observation", ["candidateId", "requestDigest", "resultDigest", "action", "status", "ready"]);
  if (!["create", "update", "delete"].includes(candidate.action) || !["Prepared", "Committed", "Aborted"].includes(candidate.status) || typeof candidate.ready !== "boolean") throw new Error("resource candidate observation 状态无效");
  return Object.freeze({
    candidateId: id(candidate.candidateId, "candidate"), requestDigest: id(candidate.requestDigest, "digest"), resultDigest: id(candidate.resultDigest, "digest"),
    action: candidate.action, status: candidate.status, ready: candidate.ready,
    ...(candidate.errorCode === undefined ? {} : { errorCode: boundedString(candidate.errorCode, 160, "errorCode") }),
    ...(candidate.errorMessage === undefined ? {} : { errorMessage: boundedString(candidate.errorMessage, 1000, "errorMessage") }),
  });
}
