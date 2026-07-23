import path from "node:path";

export const pluginId = "cn.vastplan.platform.security.authentication.delivery.webhook";
export const profileCollectionKey = "delivery-profile";
export const authorizationPurpose = "authentication.delivery.webhook";

export function loadBootstrapConfiguration(raw = process.env.VASTPLAN_PLUGIN_CONFIG_JSON ?? "{}") {
  const document = JSON.parse(raw);
  exactObject(document, ["stateFile"], "Webhook Delivery 启动配置");
  if (typeof document.stateFile !== "string" || !path.isAbsolute(document.stateFile) || path.normalize(document.stateFile) !== document.stateFile) throw new Error("Webhook Delivery stateFile 必须是规范绝对路径");
  return Object.freeze({ stateFile: document.stateFile });
}

export function normalizeProfile(resourceId, values, managedCredentials) {
  exactObject(values, ["displayName", "endpoint", "channels", "timeoutMs"], `Webhook Delivery Profile ${resourceId}`);
  if (typeof values.displayName !== "string" || values.displayName.trim().length < 1 || values.displayName.length > 160) throw new Error(`Webhook Delivery Profile ${resourceId} displayName 无效`);
  const endpoint = new URL(String(values.endpoint));
  if (endpoint.protocol !== "https:" || endpoint.username || endpoint.password || endpoint.hash) throw new Error(`Webhook Delivery Profile ${resourceId} endpoint 必须是无凭据 HTTPS URL`);
  if (!Array.isArray(values.channels)) throw new Error(`Webhook Delivery Profile ${resourceId} channels 无效`);
  const channels = [...new Set(values.channels)];
  if (channels.length < 1 || channels.length > 2 || channels.some((item) => item !== "email" && item !== "sms")) throw new Error(`Webhook Delivery Profile ${resourceId} channels 无效`);
  const timeoutMs = Number(values.timeoutMs ?? 5000);
  if (!Number.isSafeInteger(timeoutMs) || timeoutMs < 500 || timeoutMs > 15000) throw new Error(`Webhook Delivery Profile ${resourceId} timeoutMs 无效`);
  exactObject(managedCredentials, ["authorization"], `Webhook Delivery Profile ${resourceId} credentials`);
  const authorizationRef = normalizeAuthorizationRef(managedCredentials.authorization, resourceId);
  return Object.freeze({ id: resourceId, displayName: values.displayName.trim(), endpoint: endpoint.toString(), channels: Object.freeze(channels), timeoutMs, authorizationRef });
}

export function publicProfileValues(profile) {
  return Object.freeze({ displayName: profile.displayName, endpoint: profile.endpoint, channels: [...profile.channels], timeoutMs: profile.timeoutMs });
}

export function normalizeAuthorizationRef(value, id) {
  exactObject(value, ["handle", "scope", "owner", "purpose", "version", "name"], `Webhook Delivery Profile ${id} authorizationRef`, ["name"]);
  if (!String(value.handle).startsWith("credential://managed/") || value.scope !== "tenant" || value.owner !== pluginId || value.purpose !== authorizationPurpose || !Number.isSafeInteger(value.version) || value.version < 1) throw new Error(`Webhook Delivery Profile ${id} authorizationRef 无效`);
  return Object.freeze({ handle: String(value.handle), scope: "tenant", owner: pluginId, purpose: authorizationPurpose, version: value.version, ...(value.name === undefined ? {} : { name: String(value.name) }) });
}

function exactObject(value, keys, name, optional = []) {
  if (!value || typeof value !== "object" || Array.isArray(value)) throw new Error(`${name} 必须是对象`);
  const allowed = new Set(keys);
  for (const key of Object.keys(value)) if (!allowed.has(key)) throw new Error(`${name} 存在未知字段 ${key}`);
  for (const key of keys) if (!optional.includes(key) && value[key] === undefined) throw new Error(`${name} 缺少字段 ${key}`);
}
