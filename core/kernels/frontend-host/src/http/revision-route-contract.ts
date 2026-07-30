const lifecycleActionNames = ["submit", "approve", "publish"] as const;
export type LifecycleAction = (typeof lifecycleActionNames)[number];

export function parseLifecycleAction(value: string | undefined): LifecycleAction | undefined {
  return lifecycleActionNames.includes(value as LifecycleAction) ? value as LifecycleAction : undefined;
}

export function parseRevisionID(value: string | undefined): number | undefined {
  if (value === undefined || !/^[0-9]+$/.test(value)) return undefined;
  const parsed = Number(value);
  return Number.isSafeInteger(parsed) && parsed > 0 ? parsed : undefined;
}

const encoder = new TextEncoder();
export function encodeCapabilityPayload(value: unknown): Uint8Array {
  return encoder.encode(JSON.stringify(value));
}
