import { createHash } from "node:crypto";

const patterns = {
  digest: /^[a-f0-9]{64}$/,
  candidate: /^pcfg_[a-f0-9]{32}$/,
  configuration: /^cfg_[a-f0-9]{24}$/,
  collection: /^cfgc_[a-f0-9]{24}$/,
  resource: /^cfgp_[a-f0-9]{32}$/,
  field: /^[a-z][a-z0-9]*(?:[._-][a-z0-9]+)*$/,
  rfc3339: /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})$/,
};

export function parseObject(value, name) {
  if (Buffer.isBuffer(value) || typeof value === "string") {
    let parsed;
    try { parsed = JSON.parse(value.toString() || "{}"); } catch { throw new Error(`${name} 不是有效 JSON`); }
    return record(parsed, name);
  }
  return record(value, name);
}

export function record(value, name) {
  if (!value || typeof value !== "object" || Array.isArray(value)) throw new Error(`${name} 必须是对象`);
  return value;
}

export function exactKeys(value, allowed, name, required = allowed) {
  const keys = Object.keys(value);
  if (keys.some((key) => !allowed.includes(key)) || required.some((key) => value[key] === undefined)) throw new Error(`${name} 字段无效`);
}

export function normalizeJSON(value) {
  if (Array.isArray(value)) return Object.freeze(value.map(normalizeJSON));
  if (value && typeof value === "object") return Object.freeze(Object.fromEntries(Object.keys(value).sort(utf8Compare).map((key) => [key, normalizeJSON(value[key])])));
  if (typeof value === "number" && !Number.isFinite(value)) throw new Error("JSON number 无效");
  if (["string", "number", "boolean"].includes(typeof value) || value === null) return value;
  throw new Error("values 包含非 JSON 值");
}

export function normalizeCredentials(value) {
  if (value === undefined || value === null) return Object.freeze({});
  const source = record(value, "managedCredentials");
  const names = Object.keys(source).sort(utf8Compare);
  if (names.length > 64) throw new Error("managedCredentials 数量超限");
  return Object.freeze(Object.fromEntries(names.map((name) => {
    if (!patterns.field.test(name) || name.length > 80) throw new Error(`managedCredentials 字段 ${name} 无效`);
    const ref = record(source[name], `managedCredentials.${name}`);
    exactKeys(ref, ["handle", "scope", "owner", "purpose", "version", "name"], `managedCredentials.${name}`, ["handle", "scope", "owner", "purpose", "version"]);
    if (!String(ref.handle).startsWith("credential://managed/") || ref.scope !== "tenant" || !ref.owner || !ref.purpose || !Number.isSafeInteger(ref.version) || ref.version < 1) throw new Error(`managedCredentials.${name} 引用无效`);
    return [name, Object.freeze({ handle: String(ref.handle), scope: "tenant", owner: String(ref.owner), purpose: String(ref.purpose), version: ref.version, ...(ref.name === undefined ? {} : { name: String(ref.name) }) })];
  })));
}

export function activeReference(value) {
  const active = record(value, "active reference");
  exactKeys(active, ["revision", "digest"], "active reference");
  if (!Number.isSafeInteger(active.revision) || active.revision < 1) throw new Error("active revision 无效");
  return Object.freeze({ revision: active.revision, digest: id(value.digest, "digest", "active.digest") });
}

export function id(value, kind, name = kind) {
  if (typeof value !== "string" || !patterns[kind].test(value)) throw new Error(`${name} 无效`);
  return value;
}

export function rfc3339(value, name) {
  if (typeof value !== "string" || !patterns.rfc3339.test(value) || Number.isNaN(Date.parse(value))) throw new Error(`${name} 无效`);
  return value;
}

export function boundedString(value, max, name) {
  if (typeof value !== "string" || value.length < 1 || value.length > max) throw new Error(`${name} 无效`);
  return value;
}

export function sha256(value) { return createHash("sha256").update(value).digest("hex"); }
function utf8Compare(left, right) { return Buffer.compare(Buffer.from(left), Buffer.from(right)); }
