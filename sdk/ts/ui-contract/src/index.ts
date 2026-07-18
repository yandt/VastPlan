/** Serializable UI semantics shared by Web and Mobile renderers. */
export const uiContractVersion = "1.0.0" as const;
export const interactionContractVersion = "1.0.0" as const;

export type UICapability = "layout" | "menu" | "overlay" | "form" | "data" | "feedback" | "theme" | "approval" | "navigation";
export type FormFieldType = "text" | "textarea" | "number" | "boolean" | "select" | "multiSelect" | "date" | "object" | "array" | "secretRef";

export interface FormCondition { key: string; equals?: unknown; notEquals?: unknown; }
export interface FormValidation { required?: boolean; min?: number; max?: number; pattern?: string; message?: string; }
export interface FormOption { label: string; value: string | number | boolean; disabled?: boolean; }
export interface FormField {
  key: string;
  type: FormFieldType;
  title: string;
  help?: string;
  defaultValue?: unknown;
  options?: FormOption[];
  validation?: FormValidation;
  visibleWhen?: FormCondition;
  readOnly?: boolean;
  disabled?: boolean;
  fields?: FormField[];
}
export interface FormSchema { id: string; title?: string; fields: FormField[]; }

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
