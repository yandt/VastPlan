import { createHash } from "node:crypto";

export const SCOPED_CONFIGURATION_PROTOCOL = "configuration.scoped.v1";
export const SCOPED_CONFIGURATION_EXTENSION_POINT = "configuration.scoped-resolver";
export const SCOPED_CONFIGURATION_CAPABILITY = "configuration.scoped";
export const MAX_SCOPED_WATCH_TIMEOUT_MS = 30_000;

const patterns = {
  configurationId: /^cfg_[a-f0-9]{24}$/,
  digest: /^[a-f0-9]{64}$/,
  rfc3339: /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})$/,
};

export class ScopedConfigurationClient {
  constructor(plugin) {
    if (!plugin || typeof plugin.call !== "function") throw new Error("Scoped Configuration client 缺少插件宿主");
    this.plugin = plugin;
  }

  async resolve(callContext) {
    const response = await this.plugin.call(target("resolve"), callContext, Buffer.from("{}"));
    return parseScopedResolution(okPayload(response));
  }

  async watchRevision(callContext, { afterRevision, afterDigest, timeoutMs } = {}) {
    if (!Number.isSafeInteger(afterRevision) || afterRevision < 0 || !patterns.digest.test(afterDigest) ||
        (timeoutMs !== undefined && (!Number.isSafeInteger(timeoutMs) || timeoutMs < 1 || timeoutMs > MAX_SCOPED_WATCH_TIMEOUT_MS))) {
      throw new Error("Scoped Configuration watchRevision 请求无效");
    }
    const request = { afterRevision, afterDigest, ...(timeoutMs === undefined ? {} : { timeoutMs }) };
    const response = await this.plugin.call(
      target("watchRevision"), callContext, Buffer.from(JSON.stringify(request)),
      Math.min((timeoutMs ?? MAX_SCOPED_WATCH_TIMEOUT_MS) + 5_000, 35_000),
    );
    return parseRevisionObservation(okPayload(response));
  }
}

export function parseScopedResolution(payload) {
  const value = parseObject(payload, "Scoped Configuration resolution");
  exactKeys(value, ["protocol", "configurationId", "scope", "revision", "digest", "schemaDigest", "artifactSha256", "values", "source", "observedAt"]);
  const values = normalizeJSON(record(value.values, "values"));
  if (Buffer.byteLength(canonicalJSON(values)) > 64 << 10 || value.protocol !== SCOPED_CONFIGURATION_PROTOCOL ||
      !patterns.configurationId.test(value.configurationId) || !["tenant", "user"].includes(value.scope) ||
      !Number.isSafeInteger(value.revision) || value.revision < 0 || !patterns.digest.test(value.digest) ||
      !patterns.digest.test(value.schemaDigest) || !patterns.digest.test(value.artifactSha256) ||
      !["seed", "active"].includes(value.source) || (value.revision === 0) !== (value.source === "seed") ||
      !validTime(value.observedAt) || digestScopedValues(values) !== value.digest) {
    throw new Error("Scoped Configuration resolution 无效");
  }
  return Object.freeze({ ...value, values });
}

export function parseRevisionObservation(payload) {
  const value = parseObject(payload, "Scoped Configuration revision observation");
  exactKeys(value, ["protocol", "configurationId", "changed", "revision", "digest", "observedAt"]);
  if (value.protocol !== SCOPED_CONFIGURATION_PROTOCOL || !patterns.configurationId.test(value.configurationId) ||
      typeof value.changed !== "boolean" || !Number.isSafeInteger(value.revision) || value.revision < 0 ||
      !patterns.digest.test(value.digest) || !validTime(value.observedAt)) {
    throw new Error("Scoped Configuration revision observation 无效");
  }
  return Object.freeze({ ...value });
}

export function digestScopedValues(values) {
  const normalized = normalizeJSON(record(values, "values"));
  const canonical = canonicalJSON(normalized);
  if (Buffer.byteLength(canonical) > 64 << 10) throw new Error("Scoped Configuration values 大小无效");
  return createHash("sha256").update(canonical).digest("hex");
}

export function canonicalJSON(value) {
  const encoded = JSON.stringify(normalizeJSON(value));
  return encoded.replace(/[<>&\u2028\u2029]/g, (character) => ({
    "<": "\\u003c", ">": "\\u003e", "&": "\\u0026", "\u2028": "\\u2028", "\u2029": "\\u2029",
  })[character]);
}

function target(operation) { return { extension_point: SCOPED_CONFIGURATION_EXTENSION_POINT, capability: SCOPED_CONFIGURATION_CAPABILITY, operation }; }
function okPayload(response) {
  if (response?.result?.status !== "STATUS_OK") throw new Error(response?.result?.error?.message ?? "Scoped Configuration resolver 拒绝请求");
  return Buffer.from(response.payload ?? []);
}
function parseObject(value, name) {
  if (Buffer.isBuffer(value) || typeof value === "string") {
    try { return record(JSON.parse(value.toString()), name); } catch (error) { if (error instanceof SyntaxError) throw new Error(`${name} 不是有效 JSON`); throw error; }
  }
  return record(value, name);
}
function record(value, name) { if (!value || typeof value !== "object" || Array.isArray(value)) throw new Error(`${name} 必须是对象`); return value; }
function exactKeys(value, allowed) { if (Object.keys(value).length !== allowed.length || Object.keys(value).some((key) => !allowed.includes(key))) throw new Error("Scoped Configuration 响应字段无效"); }
function validTime(value) { return typeof value === "string" && patterns.rfc3339.test(value) && !Number.isNaN(Date.parse(value)); }
function normalizeJSON(value) {
  if (Array.isArray(value)) return Object.freeze(value.map(normalizeJSON));
  if (value && typeof value === "object") return Object.freeze(Object.fromEntries(Object.keys(value).sort(utf8Compare).map((key) => [key, normalizeJSON(value[key])])));
  if (typeof value === "number" && !Number.isFinite(value)) throw new Error("Scoped Configuration number 无效");
  if (["string", "number", "boolean"].includes(typeof value) || value === null) return value;
  throw new Error("Scoped Configuration values 包含非 JSON 值");
}
function utf8Compare(left, right) { return Buffer.compare(Buffer.from(left), Buffer.from(right)); }
