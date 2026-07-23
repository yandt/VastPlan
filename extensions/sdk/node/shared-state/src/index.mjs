export const SHARED_STATE_PROTOCOL = "state.shared.v1";
export const SHARED_STATE_KERNEL_PREFIX = "kernel.state.shared.";
export const MAX_SHARED_STATE_VALUE_BYTES = 1 << 20;
export const MAX_SHARED_STATE_PAGE_SIZE = 200;

export class SharedStateError extends Error {
  constructor(code, message, retryable = false) {
    super(`${code}: ${message}`);
    this.name = "SharedStateError";
    this.code = code;
    this.retryable = retryable;
  }
}

export class SharedStateClient {
  constructor(plugin, { scope, namespace }) {
    if (!plugin || typeof plugin.call !== "function" || !["tenant", "service"].includes(scope) || !validNamespace(namespace)) {
      throw new Error("Shared State client 配置无效");
    }
    this.plugin = plugin;
    this.scope = scope;
    this.namespace = namespace;
  }

  async get(callContext, key) { key = validKey(key); return this.#entry("get", callContext, { key }, key); }
  async create(callContext, key, value) { key = validKey(key); return this.#entry("create", callContext, { key, value: encodeValue(value) }, key); }
  async update(callContext, key, value, expectedRevision) {
    if (!positiveRevision(expectedRevision)) throw new Error("Shared State expectedRevision 无效");
    key = validKey(key);
    return this.#entry("update", callContext, { key, value: encodeValue(value), expectedRevision }, key);
  }
  async delete(callContext, key, expectedRevision) {
    if (!positiveRevision(expectedRevision)) throw new Error("Shared State expectedRevision 无效");
    const response = await this.#call("delete", callContext, { key: validKey(key), expectedRevision });
    const ack = parseObject(response.payload, "Shared State ack");
    exactKeys(ack, ["protocol"]);
    if (ack.protocol !== SHARED_STATE_PROTOCOL) throw new Error("Shared State ack 无效");
  }
  async list(callContext, { prefix = "", limit = 100, pageCursor } = {}) {
    if ((prefix && !validKeyValue(prefix)) || !Number.isSafeInteger(limit) || limit < 1 || limit > MAX_SHARED_STATE_PAGE_SIZE ||
        (pageCursor !== undefined && !validKeyValue(pageCursor))) throw new Error("Shared State list 请求无效");
    const response = await this.#call("list", callContext, { prefix, limit, ...(pageCursor === undefined ? {} : { pageCursor }) });
    const page = parseObject(response.payload, "Shared State page");
    exactKeys(page, ["protocol", "items", "nextPageCursor"], ["protocol", "items"]);
    if (page.protocol !== SHARED_STATE_PROTOCOL || !Array.isArray(page.items) || page.items.length > MAX_SHARED_STATE_PAGE_SIZE ||
        (page.nextPageCursor !== undefined && !validKeyValue(page.nextPageCursor))) throw new Error("Shared State page 无效");
    const items = page.items.map(parseEntry);
    if (items.some((item, index) => !item.key.startsWith(prefix) || item.key <= (index === 0 ? (pageCursor ?? "") : items[index - 1].key))) throw new Error("Shared State page 顺序或范围无效");
    return Object.freeze({ protocol: page.protocol, items: Object.freeze(items), ...(page.nextPageCursor === undefined ? {} : { nextPageCursor: page.nextPageCursor }) });
  }

  async #entry(operation, callContext, request, expectedKey) {
    const response = await this.#call(operation, callContext, request);
    const entry = parseEntry(parseObject(response.payload, "Shared State entry"));
    if (entry.key !== expectedKey) throw new Error("Shared State entry key 与请求不一致");
    return entry;
  }
  async #call(operation, callContext, request) {
    const payload = Buffer.from(JSON.stringify({ scope: this.scope, namespace: this.namespace, ...request }));
    const response = await this.plugin.call({ extension_point: "kernel.service", capability: SHARED_STATE_KERNEL_PREFIX + operation }, callContext, payload);
    if (response?.result?.status !== "STATUS_OK") {
      const error = response?.result?.error;
      throw new SharedStateError(error?.code ?? "state.unavailable", error?.message ?? "Shared State 调用失败", Boolean(error?.retryable));
    }
    return response;
  }
}

export function parseEntry(value) {
  const entry = typeof value === "object" && !Buffer.isBuffer(value) ? value : parseObject(value, "Shared State entry");
  exactKeys(entry, ["protocol", "key", "value", "revision", "updatedAt"]);
  if (entry.protocol !== SHARED_STATE_PROTOCOL || !validKeyValue(entry.key) || !positiveRevision(entry.revision) || !validTime(entry.updatedAt)) {
    throw new Error("Shared State entry 无效");
  }
  return Object.freeze({ ...entry, value: decodeValue(entry.value) });
}

export function isSharedStateConflict(error) { return error instanceof SharedStateError && error.code === "state.conflict"; }
export function isSharedStateNotFound(error) { return error instanceof SharedStateError && error.code === "state.not_found"; }

function encodeValue(value) {
  if (!Buffer.isBuffer(value) && !(value instanceof Uint8Array)) throw new Error("Shared State value 必须是 bytes");
  const buffer = Buffer.from(value);
  if (buffer.length > MAX_SHARED_STATE_VALUE_BYTES) throw new Error("Shared State value 超限");
  return buffer.toString("base64url");
}
function decodeValue(value) {
  if (typeof value !== "string") throw new Error("Shared State value 无效");
  const decoded = Buffer.from(value, "base64url");
  if (decoded.length > MAX_SHARED_STATE_VALUE_BYTES || decoded.toString("base64url") !== value) throw new Error("Shared State value 无效");
  return decoded;
}
function parseObject(value, name) {
  try {
    const parsed = Buffer.isBuffer(value) || typeof value === "string" ? JSON.parse(value.toString()) : value;
    if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) throw new Error();
    return parsed;
  } catch { throw new Error(`${name} 不是有效对象`); }
}
function exactKeys(value, allowed, required = allowed) {
  if (Object.keys(value).some((key) => !allowed.includes(key)) || required.some((key) => value[key] === undefined)) throw new Error("Shared State 响应字段无效");
}
function validNamespace(value) { return typeof value === "string" && /^[a-z][a-z0-9._-]{0,119}$/.test(value); }
function validKey(value) { if (!validKeyValue(value)) throw new Error("Shared State key 无效"); return value; }
function validKeyValue(value) { return typeof value === "string" && [...value].length >= 1 && [...value].length <= 320 && value.trim() === value && !/[\0\r\n]/.test(value); }
function positiveRevision(value) { return Number.isSafeInteger(value) && value >= 1; }
function validTime(value) { return typeof value === "string" && /^\d{4}-\d{2}-\d{2}T/.test(value) && !Number.isNaN(Date.parse(value)); }
