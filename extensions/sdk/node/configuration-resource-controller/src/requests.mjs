import { activeReference, exactKeys, id, normalizeCredentials, normalizeJSON, parseObject, sha256 } from "./json.mjs";

export function parseResourceControllerRequest(operation, payload) {
  const request = parseObject(payload, "configuration.resource.v1 请求");
  if (operation === "list") {
    exactKeys(request, ["collectionId", "cursor", "limit"], "list", ["collectionId"]);
    if (request.cursor !== undefined && (typeof request.cursor !== "string" || request.cursor.length < 1 || request.cursor.length > 512)) throw new Error("cursor 无效");
    if (request.limit !== undefined && (!Number.isSafeInteger(request.limit) || request.limit < 1 || request.limit > 256)) throw new Error("limit 无效");
    return Object.freeze({ collectionId: id(request.collectionId, "collection"), ...(request.cursor === undefined ? {} : { cursor: request.cursor }), ...(request.limit === undefined ? {} : { limit: request.limit }) });
  }
  if (operation === "get") {
    exactKeys(request, ["collectionId", "resourceId"], "get");
    return Object.freeze({ collectionId: id(request.collectionId, "collection"), resourceId: id(request.resourceId, "resource") });
  }
  if (operation === "prepare") return normalizePrepareRequest(request);
  if (operation === "commit" || operation === "abort") {
    exactKeys(request, ["candidateId", "requestDigest"], operation);
    return Object.freeze({ candidateId: id(request.candidateId, "candidate"), requestDigest: id(request.requestDigest, "digest") });
  }
  if (operation === "status") {
    exactKeys(request, ["collectionId", "resourceId", "candidateId", "requestDigest"], "status", ["collectionId", "resourceId"]);
    const hasCandidate = request.candidateId !== undefined;
    if (hasCandidate !== (request.requestDigest !== undefined)) throw new Error("status candidateId/requestDigest 必须同时提供");
    return Object.freeze({ collectionId: id(request.collectionId, "collection"), resourceId: id(request.resourceId, "resource"), ...(hasCandidate ? { candidateId: id(request.candidateId, "candidate"), requestDigest: id(request.requestDigest, "digest") } : {}) });
  }
  throw new Error(`不支持的 configuration.resource.v1 操作 ${operation}`);
}

export function normalizePrepareRequest(value) {
  const request = parseObject(value, "resource prepare 请求");
  const allowed = ["candidateId", "configurationId", "collectionId", "resourceId", "action", "catalogDigest", "schemaDigest", "artifactSha256", "expectedActive", "values", "managedCredentials"];
  exactKeys(request, allowed, "prepare", allowed.slice(0, 8));
  if (!["create", "update", "delete"].includes(request.action)) throw new Error("resource action 无效");
  const hasValues = request.values !== undefined;
  const hasActive = request.expectedActive !== undefined;
  const credentials = normalizeCredentials(request.managedCredentials);
  if (request.action === "create" && (hasActive || !hasValues)) throw new Error("create 必须提供 values 且不得提供 expectedActive");
  if (request.action === "update" && (!hasActive || !hasValues)) throw new Error("update 必须提供 expectedActive 与 values");
  if (request.action === "delete" && (!hasActive || hasValues || Object.keys(credentials).length > 0)) throw new Error("delete 只接受 expectedActive");
  const values = hasValues ? normalizeJSON(parseObject(request.values, "values")) : undefined;
  if (values !== undefined && Buffer.byteLength(JSON.stringify(values)) > 64 << 10) throw new Error("values 大小无效");
  return Object.freeze({
    candidateId: id(request.candidateId, "candidate"), configurationId: id(request.configurationId, "configuration"),
    collectionId: id(request.collectionId, "collection"), resourceId: id(request.resourceId, "resource"), action: request.action,
    catalogDigest: id(request.catalogDigest, "digest"), schemaDigest: id(request.schemaDigest, "digest"), artifactSha256: id(request.artifactSha256, "digest"),
    ...(hasActive ? { expectedActive: activeReference(request.expectedActive) } : {}), ...(hasValues ? { values } : {}),
    ...(Object.keys(credentials).length === 0 ? {} : { managedCredentials: credentials }),
  });
}

export function prepareResourceRequestDigest(request) {
  return sha256(JSON.stringify(normalizePrepareRequest(request)));
}

export function resourceConfigurationDigest(values, managedCredentials = {}) {
  const normalizedValues = normalizeJSON(parseObject(values, "values"));
  const credentials = normalizeCredentials(managedCredentials);
  return sha256(JSON.stringify({ values: normalizedValues, ...(Object.keys(credentials).length === 0 ? {} : { managedCredentials: credentials }) }));
}

export function deletedResourceDigest(resourceId) {
  return sha256(JSON.stringify({ deleted: true, resourceId: id(resourceId, "resource") }));
}
