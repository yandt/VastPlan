/** Serializable UI semantics shared by Web and Mobile renderers. */
export const uiContractVersion = "2.0.0" as const;
export const interactionContractVersion = "1.0.0" as const;
export const jsonSchemaDialect = "http://json-schema.org/draft-07/schema#" as const;
export * from "./i18n.js";

export type UICapability = "layout" | "menu" | "overlay" | "form" | "data" | "feedback" | "theme" | "approval" | "navigation";

export type JSONPrimitive = string | number | boolean | null;
export type JSONValue = JSONPrimitive | readonly JSONValue[] | { readonly [key: string]: JSONValue };
/** Package-neutral JSON Schema document. V1 accepts Draft 7 only. */
export type JSONSchema = Readonly<Record<string, JSONValue>>;
/** Serializable RJSF-compatible presentation hints; never contains components or functions. */
export type FormUISchema = Readonly<Record<string, JSONValue>>;
export interface FormSchema {
  id: string;
  schema: JSONSchema;
  uiSchema?: FormUISchema;
  /** JSON Pointer -> localized text. Keeps the validation schema standards-compliant. */
  localization?: Readonly<Record<string, import("./i18n.js").LocalizedText>>;
  /** JSON Pointer -> localized presentation hint, applied only to uiSchema. */
  uiLocalization?: Readonly<Record<string, import("./i18n.js").LocalizedText>>;
}

export interface FormValidationIssue {
  path: string;
  code: string;
  message?: string;
  schemaPath?: string;
}
export interface FormValidationResult { valid: boolean; issues: FormValidationIssue[]; }

export type InteractionKind = "confirm" | "form" | "approval" | "notification" | "progress";
export type InteractionSurface = "frontend" | "mobile" | "runner.local";
export interface InteractionSource { workflowRunId?: string; capability: string; operation?: string; }
export interface InteractionRequest {
  id: string;
  contractVersion: typeof interactionContractVersion;
  kind: InteractionKind;
  source: InteractionSource;
  tenantId: string;
  eligibleSubjects: string[];
  allowedSurfaces: InteractionSurface[];
  fallback?: "expire" | "runner.local-if-allowed";
  expiresAt: string;
  title?: string;
  message?: string;
  form?: FormSchema;
}
export interface InteractionResponse {
  interactionId: string;
  decision: "answered" | "rejected";
  values?: Record<string, unknown>;
  credentialRefs?: Record<string, string>;
}

export type InteractionState = "created" | "presented" | "answered" | "rejected" | "cancelled" | "expired";
export interface InteractionAuditEvent { action: string; actorId: string; surface?: string; at: string; }

/** Persisted Broker view; it stays serializable and contains no renderer code. */
export interface InteractionRecord {
  request: InteractionRequest;
  state: InteractionState;
  response?: InteractionResponse;
  createdAt: string;
  updatedAt: string;
  presentedBy?: string;
  audit: InteractionAuditEvent[];
}
