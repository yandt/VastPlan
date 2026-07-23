import { createHash } from "node:crypto";

export const CONFIGURATION_CONTROLLER_PROTOCOL = "configuration.v1";
export const CONFIGURATION_CONTROLLER_EXTENSION_POINT = "configuration.controller";

const digestPattern = /^[a-f0-9]{64}$/;
const candidatePattern = /^pcfg_[a-f0-9]{32}$/;
const configurationPattern = /^cfg_[a-f0-9]{24}$/;
const fieldPattern = /^[a-z][a-z0-9]*(?:[._-][a-z0-9]+)*$/;
const rfc3339Pattern = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})$/;

export function configurationControllerCapability(pluginId) {
  if (typeof pluginId !== "string" || pluginId.trim() === "") throw new Error("配置控制器缺少插件身份");
  return `configuration.${createHash("sha256").update(pluginId.trim()).digest("hex").slice(0, 32)}`;
}

export function parseControllerRequest(operation, payload) {
  const request = parseObject(payload, "configuration.v1 请求");
  if (operation === "prepare") return normalizePrepareRequest(request);
  if (operation === "commit" || operation === "abort") {
    exactKeys(request, ["candidateId", "requestDigest"], operation);
    return Object.freeze({ candidateId: candidateId(request.candidateId), requestDigest: digest(request.requestDigest, "requestDigest") });
  }
  if (operation === "status") {
    exactKeys(request, ["configurationId", "candidateId", "requestDigest"], operation, ["configurationId"]);
    const hasCandidate = request.candidateId !== undefined;
    if (hasCandidate !== (request.requestDigest !== undefined)) throw new Error("status candidateId/requestDigest 必须同时提供");
    return Object.freeze({
      configurationId: configurationId(request.configurationId),
      ...(hasCandidate ? { candidateId: candidateId(request.candidateId), requestDigest: digest(request.requestDigest, "requestDigest") } : {}),
    });
  }
  throw new Error(`不支持的 configuration.v1 操作 ${operation}`);
}

export function normalizePrepareRequest(value) {
  const request = parseObject(value, "prepare 请求");
  exactKeys(request, ["candidateId", "configurationId", "catalogDigest", "schemaDigest", "artifactSha256", "expectedActive", "values", "managedCredentials"], "prepare", ["candidateId", "configurationId", "catalogDigest", "schemaDigest", "artifactSha256", "expectedActive", "values"]);
  const values = normalizeJSON(record(request.values, "values"));
  const valuesBytes = Buffer.byteLength(JSON.stringify(values));
  if (valuesBytes === 0 || valuesBytes > 64 << 10) throw new Error("configuration.v1 values 大小无效");
  const credentials = normalizeCredentials(request.managedCredentials);
  return Object.freeze({
    candidateId: candidateId(request.candidateId),
    configurationId: configurationId(request.configurationId),
    catalogDigest: digest(request.catalogDigest, "catalogDigest"),
    schemaDigest: digest(request.schemaDigest, "schemaDigest"),
    artifactSha256: digest(request.artifactSha256, "artifactSha256"),
    expectedActive: activeReference(request.expectedActive),
    values,
    ...(Object.keys(credentials).length === 0 ? {} : { managedCredentials: credentials }),
  });
}

export function prepareRequestDigest(request) {
  return sha256(JSON.stringify(normalizePrepareRequest(request)));
}

export function configurationDigest(values, managedCredentials = {}) {
  const normalizedValues = normalizeJSON(record(values, "values"));
  const credentials = normalizeCredentials(managedCredentials);
  return sha256(JSON.stringify({ values: normalizedValues, ...(Object.keys(credentials).length === 0 ? {} : { managedCredentials: credentials }) }));
}

export function validateObservation(value) {
  const observation = record(value, "configuration.v1 observation");
  exactKeys(observation, ["protocol", "configurationId", "active", "candidate", "observedAt"], "observation", ["protocol", "configurationId", "active", "observedAt"]);
  if (observation.protocol !== CONFIGURATION_CONTROLLER_PROTOCOL || typeof observation.observedAt !== "string" ||
      !rfc3339Pattern.test(observation.observedAt) || Number.isNaN(Date.parse(observation.observedAt))) {
    throw new Error("configuration.v1 observation 身份无效");
  }
  const normalized = {
    protocol: CONFIGURATION_CONTROLLER_PROTOCOL,
    configurationId: configurationId(observation.configurationId),
    active: activeReference(observation.active),
    ...(observation.candidate === undefined ? {} : { candidate: candidateObservation(observation.candidate) }),
    observedAt: String(observation.observedAt),
  };
  if (normalized.candidate?.status === "Committed" && (!normalized.candidate.ready || normalized.active.digest !== normalized.candidate.configurationDigest)) throw new Error("Committed 配置候选未成为 Active");
  if (normalized.candidate?.status === "Aborted" && normalized.candidate.ready) throw new Error("Aborted 配置候选不得 Ready");
  return Object.freeze(normalized);
}

function candidateObservation(value) {
  const candidate = record(value, "candidate observation");
  exactKeys(candidate, ["candidateId", "requestDigest", "configurationDigest", "status", "ready", "errorCode", "errorMessage"], "candidate observation", ["candidateId", "requestDigest", "configurationDigest", "status", "ready"]);
  if (!["Prepared", "Committed", "Aborted"].includes(candidate.status) || typeof candidate.ready !== "boolean") throw new Error("candidate observation 状态无效");
  return Object.freeze({
    candidateId: candidateId(candidate.candidateId), requestDigest: digest(candidate.requestDigest, "requestDigest"),
    configurationDigest: digest(candidate.configurationDigest, "configurationDigest"), status: candidate.status, ready: candidate.ready,
    ...(candidate.errorCode === undefined ? {} : { errorCode: boundedString(candidate.errorCode, 160, "errorCode") }),
    ...(candidate.errorMessage === undefined ? {} : { errorMessage: boundedString(candidate.errorMessage, 1000, "errorMessage") }),
  });
}

function normalizeCredentials(value) {
  if (value === undefined || value === null) return Object.freeze({});
  const source = record(value, "managedCredentials");
  const names = Object.keys(source).sort(utf8Compare);
  if (names.length > 64) throw new Error("managedCredentials 数量超限");
  return Object.freeze(Object.fromEntries(names.map((name) => {
    if (!fieldPattern.test(name) || name.length > 80) throw new Error(`managedCredentials 字段 ${name} 无效`);
    const ref = record(source[name], `managedCredentials.${name}`);
    exactKeys(ref, ["handle", "scope", "owner", "purpose", "version", "name"], `managedCredentials.${name}`, ["handle", "scope", "owner", "purpose", "version"]);
    if (!String(ref.handle).startsWith("credential://managed/") || ref.scope !== "tenant" || !ref.owner || !ref.purpose || !Number.isSafeInteger(ref.version) || ref.version < 1) throw new Error(`managedCredentials.${name} 引用无效`);
    return [name, Object.freeze({ handle: String(ref.handle), scope: "tenant", owner: String(ref.owner), purpose: String(ref.purpose), version: ref.version, ...(ref.name === undefined ? {} : { name: String(ref.name) }) })];
  })));
}

function activeReference(value) {
  const active = record(value, "active reference");
  exactKeys(active, ["revision", "digest"], "active reference", ["revision", "digest"]);
  if (!Number.isSafeInteger(active.revision) || active.revision < 1) throw new Error("active revision 无效");
  return Object.freeze({ revision: active.revision, digest: digest(active.digest, "active.digest") });
}

function normalizeJSON(value) {
  if (Array.isArray(value)) return Object.freeze(value.map(normalizeJSON));
  if (value && typeof value === "object") return Object.freeze(Object.fromEntries(Object.keys(value).sort(utf8Compare).map((key) => [key, normalizeJSON(value[key])])));
  if (typeof value === "number" && !Number.isFinite(value)) throw new Error("JSON number 无效");
  if (["string", "number", "boolean"].includes(typeof value) || value === null) return value;
  throw new Error("values 包含非 JSON 值");
}

function parseObject(value, name) {
  if (Buffer.isBuffer(value) || typeof value === "string") {
    let parsed;
    try { parsed = JSON.parse(value.toString() || "{}"); } catch { throw new Error(`${name} 不是有效 JSON`); }
    return record(parsed, name);
  }
  return record(value, name);
}
function record(value, name) { if (!value || typeof value !== "object" || Array.isArray(value)) throw new Error(`${name} 必须是对象`); return value; }
function exactKeys(value, allowed, name, required = allowed) { const keys = Object.keys(value); if (keys.some((key) => !allowed.includes(key)) || required.some((key) => value[key] === undefined)) throw new Error(`${name} 字段无效`); }
function digest(value, name) { if (typeof value !== "string" || !digestPattern.test(value)) throw new Error(`${name} 无效`); return value; }
function candidateId(value) { if (typeof value !== "string" || !candidatePattern.test(value)) throw new Error("candidateId 无效"); return value; }
function configurationId(value) { if (typeof value !== "string" || !configurationPattern.test(value)) throw new Error("configurationId 无效"); return value; }
function boundedString(value, max, name) { if (typeof value !== "string" || value.length < 1 || value.length > max) throw new Error(`${name} 无效`); return value; }
function utf8Compare(left, right) { return Buffer.compare(Buffer.from(left), Buffer.from(right)); }
function sha256(value) { return createHash("sha256").update(value).digest("hex"); }
