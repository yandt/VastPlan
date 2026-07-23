const patterns = {
  handle: /^credential:\/\/managed\/[A-Za-z0-9._~-]+$/,
  owner: /^[a-z0-9]+(?:[.-][a-z0-9]+)+$/,
  purpose: /^[a-z][a-z0-9]*(?:[._-][a-z0-9]+)+$/,
  field: /^[a-z][a-z0-9]*(?:[._-][a-z0-9]+)*$/,
};

export function normalizeManagedCredentialRef(value, { allowedScopes = ["tenant", "service"] } = {}) {
  const ref = record(value, "Managed CredentialRef");
  exactKeys(ref, ["handle", "scope", "owner", "purpose", "version", "name"], ["handle", "scope", "owner", "purpose", "version"]);
  if (!boundedPattern(ref.handle, patterns.handle, 256) || !allowedScopes.includes(ref.scope) ||
      !boundedPattern(ref.owner, patterns.owner, 160) || !boundedPattern(ref.purpose, patterns.purpose, 160) ||
      !Number.isSafeInteger(ref.version) || ref.version < 1 ||
      (ref.name !== undefined && (typeof ref.name !== "string" || codePoints(ref.name) < 1 || codePoints(ref.name) > 160))) {
    throw new Error("Managed CredentialRef 无效");
  }
  return Object.freeze({
    handle: ref.handle, scope: ref.scope, owner: ref.owner, purpose: ref.purpose, version: ref.version,
    ...(ref.name === undefined ? {} : { name: ref.name }),
  });
}

export function normalizeManagedCredentialRefs(value, { allowedScopes = ["tenant"], maximum = 64 } = {}) {
  if (value === undefined || value === null) return Object.freeze({});
  const source = record(value, "managedCredentials");
  const names = Object.keys(source).sort(utf8Compare);
  if (!Number.isSafeInteger(maximum) || maximum < 1 || names.length > maximum) throw new Error("managedCredentials 数量超限");
  return Object.freeze(Object.fromEntries(names.map((name) => {
    if (!boundedPattern(name, patterns.field, 80)) throw new Error(`managedCredentials 字段 ${name} 无效`);
    return [name, normalizeManagedCredentialRef(source[name], { allowedScopes })];
  })));
}

export function sameManagedCredentialRef(left, right) {
  const a = normalizeManagedCredentialRef(left);
  const b = normalizeManagedCredentialRef(right);
  return a.handle === b.handle && a.scope === b.scope && a.owner === b.owner && a.purpose === b.purpose && a.version === b.version && a.name === b.name;
}

function record(value, name) {
  if (!value || typeof value !== "object" || Array.isArray(value)) throw new Error(`${name} 必须是对象`);
  return value;
}
function exactKeys(value, allowed, required) {
  if (Object.keys(value).some((key) => !allowed.includes(key)) || required.some((key) => value[key] === undefined)) throw new Error("Managed CredentialRef 字段无效");
}
function boundedPattern(value, pattern, maximum) { return typeof value === "string" && codePoints(value) <= maximum && pattern.test(value); }
function codePoints(value) { return [...value].length; }
function utf8Compare(left, right) { return Buffer.compare(Buffer.from(left), Buffer.from(right)); }
