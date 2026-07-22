export interface MethodDescriptor {
  readonly methodId: string;
  readonly providerId: string;
  readonly kind: "password" | "one-time-code" | "redirect" | "passkey";
  readonly interaction: "form" | "redirect" | "native";
  readonly displayName: Readonly<Record<string, string>>;
  readonly amr: readonly string[];
  readonly acr: string;
  readonly supportsResend: boolean;
}

export interface AuthenticationStep {
  readonly stepId: string;
  readonly kind: "identifier" | "password" | "one-time-code" | "redirect" | "context-selection";
  readonly redirectUri?: string;
  readonly expiresAt: string;
  readonly [key: string]: unknown;
}

export interface BrokerMethodResult {
  readonly state: "challenge" | "authenticated" | "rejected" | "locked" | "expired" | "cancelled";
  readonly step?: AuthenticationStep;
  readonly reason?: string;
  readonly evidence?: Readonly<Record<string, unknown>>;
}

export interface BrokerContinueResponse {
  readonly result: BrokerMethodResult;
  readonly assertion?: unknown;
}

export function parseDescribe(value: unknown): readonly MethodDescriptor[] {
  if (!isRecord(value) || value.protocol !== "authentication.method.v1" || !Array.isArray(value.methods) || value.methods.length > 32) throw new Error("Authentication Broker describe 响应无效");
  return Object.freeze(value.methods.map(parseMethod));
}

export function parseResultEnvelope(value: unknown, allowAssertion: boolean): BrokerContinueResponse {
  if (!isRecord(value) || !isRecord(value.result) || (!allowAssertion && Object.hasOwn(value, "assertion"))) throw new Error("Authentication Broker 响应无效");
  const result = parseResult(value.result);
  if (result.state === "authenticated" ? !Object.hasOwn(value, "assertion") : Object.hasOwn(value, "assertion")) throw new Error("Authentication Broker Assertion 状态无效");
  return Object.freeze({ result, ...(Object.hasOwn(value, "assertion") ? { assertion: value.assertion } : {}) });
}

export function parseContinueInput(value: unknown): { stepId: string; responses?: readonly { fieldId: string; value: string }[]; redirect?: Readonly<Record<string, string>> } {
  if (!isRecord(value) || !hasOnlyFrom(value, ["stepId", "responses", "redirect"]) || !token(value.stepId)) throw new Error("认证响应无效");
  const hasResponses = Object.hasOwn(value, "responses"), hasRedirect = Object.hasOwn(value, "redirect");
  if (hasResponses === hasRedirect) throw new Error("认证响应必须且只能包含表单或 redirect");
  if (hasResponses) {
    if (!Array.isArray(value.responses) || value.responses.length < 1 || value.responses.length > 16) throw new Error("认证字段响应无效");
    const responses = value.responses.map((item) => {
      if (!isRecord(item) || !hasOnly(item, ["fieldId", "value"]) || !fieldID(item.fieldId) || typeof item.value !== "string" || item.value.length > 4096) throw new Error("认证字段响应无效");
      return Object.freeze({ fieldId: item.fieldId, value: item.value });
    });
    if (new Set(responses.map(({ fieldId }) => fieldId)).size !== responses.length) throw new Error("认证字段重复");
    return Object.freeze({ stepId: value.stepId, responses: Object.freeze(responses) });
  }
  if (!isRecord(value.redirect) || !hasOnlyFrom(value.redirect, ["code", "state", "error", "errorDescription"]) || !token(value.redirect.state)) throw new Error("redirect 响应无效");
  const code = value.redirect.code, error = value.redirect.error;
  if ((typeof code === "string") === (typeof error === "string") || (typeof code === "string" && (code.length < 1 || code.length > 4096)) || (typeof error === "string" && !fieldID(error))) throw new Error("redirect 响应无效");
  if (value.redirect.errorDescription !== undefined && (typeof value.redirect.errorDescription !== "string" || value.redirect.errorDescription.length > 1024)) throw new Error("redirect 响应无效");
  return Object.freeze({ stepId: value.stepId, redirect: Object.freeze(value.redirect as Record<string, string>) });
}

function parseMethod(value: unknown): MethodDescriptor {
  if (!isRecord(value) || !safeID(value.methodId) || !safeID(value.providerId) || !new Set(["password", "one-time-code", "redirect", "passkey"]).has(String(value.kind))
    || !new Set(["form", "redirect", "native"]).has(String(value.interaction)) || !isRecord(value.displayName) || typeof value.supportsResend !== "boolean"
    || !Array.isArray(value.amr) || value.amr.some((item) => typeof item !== "string") || typeof value.acr !== "string") throw new Error("Authentication Method 描述无效");
  return Object.freeze({ ...value, displayName: Object.freeze({ ...value.displayName }), amr: Object.freeze([...(value.amr as string[])]) }) as unknown as MethodDescriptor;
}

function parseResult(value: Record<string, unknown>): BrokerMethodResult {
  if (!new Set(["challenge", "authenticated", "rejected", "locked", "expired", "cancelled"]).has(String(value.state))) throw new Error("Authentication Method 状态无效");
  if (value.state === "challenge") {
    if (!isRecord(value.step) || !token(value.step.stepId) || !new Set(["identifier", "password", "one-time-code", "redirect", "context-selection"]).has(String(value.step.kind)) || typeof value.step.expiresAt !== "string") throw new Error("Authentication Step 无效");
  }
  return Object.freeze({ ...value, ...(isRecord(value.step) ? { step: Object.freeze({ ...value.step }) as AuthenticationStep } : {}) }) as BrokerMethodResult;
}

function token(value: unknown): value is string { return typeof value === "string" && /^[A-Za-z0-9_-]{32,256}$/.test(value); }
function fieldID(value: unknown): value is string { return typeof value === "string" && /^[A-Za-z][A-Za-z0-9._-]{0,127}$/.test(value); }
function safeID(value: unknown): value is string { return typeof value === "string" && /^[A-Za-z0-9][A-Za-z0-9._:@/-]{0,255}$/.test(value); }
function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === "object" && value !== null && !Array.isArray(value); }
function hasOnly(value: Record<string, unknown>, keys: readonly string[]): boolean { return Object.keys(value).length === keys.length && keys.every((key) => Object.hasOwn(value, key)); }
function hasOnlyFrom(value: Record<string, unknown>, keys: readonly string[]): boolean { return Object.keys(value).every((key) => keys.includes(key)); }
