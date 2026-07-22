const pluginId = "cn.vastplan.platform.security.authentication.delivery.webhook";

export function loadConfiguration(raw = process.env.VASTPLAN_PLUGIN_CONFIG_JSON ?? "{}") {
  const document = JSON.parse(raw);
  exactObject(document, ["profiles"], "Webhook Delivery 配置");
  if (!document.profiles || typeof document.profiles !== "object" || Array.isArray(document.profiles)) throw new Error("Webhook Delivery profiles 不能为空");
  const profiles = new Map();
  for (const [id, value] of Object.entries(document.profiles)) {
    exactObject(value, ["endpoint", "authorizationRef", "channels", "timeoutMs"], `Webhook Delivery Profile ${id}`);
    if (!/^[A-Za-z0-9][A-Za-z0-9._:@/-]{0,255}$/.test(id)) throw new Error(`Webhook Delivery Profile ${id} ID 无效`);
    const endpoint = new URL(String(value.endpoint));
    if (endpoint.protocol !== "https:" || endpoint.username || endpoint.password || endpoint.hash) throw new Error(`Webhook Delivery Profile ${id} endpoint 必须是无凭据 HTTPS URL`);
    const channels = [...new Set(value.channels ?? ["email", "sms"])];
    if (channels.length < 1 || channels.some((item) => item !== "email" && item !== "sms")) throw new Error(`Webhook Delivery Profile ${id} channels 无效`);
    const timeoutMs = Number(value.timeoutMs ?? 5000);
    if (!Number.isSafeInteger(timeoutMs) || timeoutMs < 500 || timeoutMs > 15000) throw new Error(`Webhook Delivery Profile ${id} timeoutMs 无效`);
    profiles.set(id, Object.freeze({ id, endpoint: endpoint.toString(), authorizationRef: credentialRef(value.authorizationRef, id), channels: Object.freeze(channels), timeoutMs }));
  }
  if (profiles.size < 1 || profiles.size > 64) throw new Error("Webhook Delivery profiles 数量必须为 1-64");
  return profiles;
}

function credentialRef(value, id) {
  exactObject(value, ["handle", "scope", "owner", "purpose", "version"], `Webhook Delivery Profile ${id} authorizationRef`);
  if (!String(value.handle).startsWith("credential://managed/") || value.scope !== "tenant" || value.owner !== pluginId || value.purpose !== "authentication.delivery.webhook" || !Number.isSafeInteger(value.version) || value.version < 1) throw new Error(`Webhook Delivery Profile ${id} authorizationRef 无效`);
  return Object.freeze({ handle:String(value.handle), scope:"tenant", owner:pluginId, purpose:"authentication.delivery.webhook", version:value.version });
}

function exactObject(value, keys, name) {
  if (!value || typeof value !== "object" || Array.isArray(value)) throw new Error(`${name} 必须是对象`);
  const allowed = new Set(keys);
  for (const key of Object.keys(value)) if (!allowed.has(key)) throw new Error(`${name} 存在未知字段 ${key}`);
}
