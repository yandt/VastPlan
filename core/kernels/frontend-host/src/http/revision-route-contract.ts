export const lifecycleActions = new Set(["submit", "approve", "publish"]);

export function parseRevisionID(value: string | undefined): number | undefined {
  if (value === undefined || !/^[0-9]+$/.test(value)) return undefined;
  const parsed = Number(value);
  return Number.isSafeInteger(parsed) && parsed > 0 ? parsed : undefined;
}

const encoder = new TextEncoder();
export function encodeCapabilityPayload(value: unknown): Uint8Array {
  return encoder.encode(JSON.stringify(value));
}
