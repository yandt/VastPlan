import { createHash } from "node:crypto";
import { CapabilityApplicationError } from "../capabilities/capability-invoker";

// Capability failures stay server-side: API callers receive stable public
// error codes while operators retain a bounded classification and correlation
// digest. Raw upstream messages are never logged because they may contain
// provider, endpoint or resource details.
export interface CapabilityFailureContext {
  operation: string;
  capability?: string;
  logicalService?: string;
}

export function capabilityFailureRecord(context: CapabilityFailureContext, error: unknown): Readonly<Record<string, unknown>> {
  const detail = error instanceof Error ? error.message : String(error);
  return {
    level: "error",
    message: "portal capability rejected",
    operation: context.operation,
    ...(context.capability === undefined ? {} : { capability: context.capability }),
    ...(context.logicalService === undefined ? {} : { logical_service: context.logicalService }),
    code: safeFailureCode(error),
    error_type: error instanceof Error ? error.name : "UnknownError",
    detail_digest: createHash("sha256").update(detail).digest("hex").slice(0, 16),
    ...(error instanceof CapabilityApplicationError && validTraceId(error.traceId) ? { trace_id: error.traceId } : {}),
    ...(isRetryable(error) ? { retryable: true } : {}),
  };
}

function validTraceId(value: string | undefined): value is string {
  return value !== undefined && /^[a-f0-9]{32}$/.test(value);
}

export function reportCapabilityFailure(context: CapabilityFailureContext, error: unknown): void {
  process.stderr.write(`${JSON.stringify(capabilityFailureRecord(context, error))}\n`);
}

function safeFailureCode(error: unknown): string {
  const code = error instanceof CapabilityApplicationError ? error.code : readStringProperty(error, "code");
  return code !== undefined && /^[a-z0-9][a-z0-9._-]{0,127}$/.test(code) ? code : "transport.failed";
}

function isRetryable(error: unknown): boolean {
  return typeof error === "object" && error !== null && "retryable" in error && error.retryable === true;
}

function readStringProperty(value: unknown, key: string): string | undefined {
  if (typeof value !== "object" || value === null || !(key in value)) return undefined;
  const candidate = value[key as keyof typeof value];
  return typeof candidate === "string" ? candidate : undefined;
}
