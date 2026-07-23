const safeId = /^[A-Za-z0-9][A-Za-z0-9._:@/-]{0,511}$/;

export class WebhookDelivery {
  constructor(profiles, { materialLease, fetcher = fetch }) {
    this.profiles = profiles;
    this.materialLease = materialLease;
    this.fetcher = fetcher;
  }

  health() { return { ready: this.profiles.size > 0 }; }

  async deliver(input, context, signal) {
    const request = parseRequest(input);
    const path = Array.isArray(context.call_path) ? context.call_path : [];
    if (!path.some((item) => String(item).startsWith("authentication.provider/enterprise-one-time-code#"))) throw new Error("Delivery 只接受 OTP Provider 调用链");
    const tenantId = String(context.tenant_id ?? "");
    if (!safeId.test(tenantId)) throw new Error("Delivery 缺少可信 tenant")
    const profile = this.profiles.get(`${tenantId}\0${request.deliveryProfileId}`);
    if (!profile || !profile.channels.includes(request.channel)) throw new Error("Delivery Profile 不可用");
    const body = Buffer.from(JSON.stringify({ protocol:"authentication.delivery.v1", ...request }));
    try {
      return await this.materialLease.withMaterial(profile.authorizationRef, tenantId, signal, async (material) => {
        if (material.length < 16 || material.length > 4096 || material.includes(10) || material.includes(13)) throw new Error("Webhook authorization material 无效");
        const controller = new AbortController();
        const timeout = setTimeout(() => controller.abort(), profile.timeoutMs);
        const abort = () => controller.abort(); signal?.addEventListener?.("abort", abort, { once:true });
        try {
          const response = await this.fetcher(profile.endpoint, { method:"POST", redirect:"error", cache:"no-store", headers:{ accept:"application/json", authorization:`Bearer ${material.toString("utf8")}`, "content-type":"application/json", "x-vastplan-delivery-protocol":"authentication.delivery.v1" }, body, signal:controller.signal });
          if (!response.ok) throw new Error("Webhook Delivery 上游拒绝");
          if (!/^application\/json(?:;|$)/i.test(response.headers.get("content-type") ?? "")) throw new Error("Webhook Delivery 响应类型无效");
          const declared = Number(response.headers.get("content-length") ?? 0);
          if (declared > 4096) throw new Error("Webhook Delivery 响应过大");
          const bytes = Buffer.from(await response.arrayBuffer());
          if (bytes.length > 4096) throw new Error("Webhook Delivery 响应过大");
          return parseResult(JSON.parse(bytes.toString("utf8")));
        } finally {
          clearTimeout(timeout); signal?.removeEventListener?.("abort", abort);
        }
      });
    } finally { body.fill(0); }
  }
}

function parseRequest(value) {
  exact(value, ["challengeId","deliveryProfileId","channel","identifier","locale","code","expiresAt"]);
  const expiresAt = Date.parse(value.expiresAt), now = Date.now();
  if (!safeId.test(String(value.challengeId)) || !safeId.test(String(value.deliveryProfileId)) || (value.channel !== "email" && value.channel !== "sms") || typeof value.identifier !== "string" || value.identifier.length < 1 || value.identifier.length > 320 || typeof value.locale !== "string" || !/^[0-9]{4,32}$/.test(String(value.code)) || !Number.isFinite(expiresAt) || expiresAt <= now - 5000 || expiresAt > now + 600000) throw new Error("Delivery 请求无效");
  return Object.freeze({ challengeId:String(value.challengeId), deliveryProfileId:String(value.deliveryProfileId), channel:value.channel, identifier:value.identifier, locale:value.locale, code:String(value.code), expiresAt:new Date(value.expiresAt).toISOString() });
}
function parseResult(value) { exact(value, ["accepted","subjectId"]); if (typeof value.accepted !== "boolean" || value.accepted !== (typeof value.subjectId === "string" && safeId.test(value.subjectId))) throw new Error("Webhook Delivery 响应无效"); return Object.freeze({ accepted:value.accepted, ...(value.accepted ? {subjectId:value.subjectId} : {}) }); }
function exact(value, allowedKeys) { if (!value || typeof value !== "object" || Array.isArray(value) || Object.keys(value).some((key) => !allowedKeys.includes(key))) throw new Error("Delivery 消息字段无效"); }
