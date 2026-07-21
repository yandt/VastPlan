import { CapabilityApplicationError } from "../capabilities/capability-invoker";

// Capability failures stay server-side: API callers receive stable public
// error codes while operators retain the rejected operation and trusted
// service error needed to diagnose composition failures.
export function reportCapabilityFailure(operation: string, error: unknown): void {
  const detail = error instanceof Error ? error.message : String(error);
  const code = error instanceof CapabilityApplicationError ? error.code : "transport.failed";
  process.stderr.write(`${JSON.stringify({ level: "error", message: "portal capability rejected", operation, code, detail })}\n`);
}
