import { createUser, type KeyPair } from "@nats-io/nkeys";
import type { TransportIdentity } from "./types.js";

export interface TestTransportIdentity {
  pair: KeyPair;
  seed: Uint8Array;
  identity: TransportIdentity;
}

export function createTestTransportIdentity(name: string, nodeId: string, overrides: Partial<TransportIdentity> = {}): TestTransportIdentity {
  const pair = createUser();
  const identity: TransportIdentity = {
    name, role: "frontend", publicKey: pair.getPublicKey(), nodeId,
    serviceRoles: ["backend"], logicalServices: ["*"], allowedCapabilities: ["*"], allowGlobal: true, allowDelegation: true,
    ...overrides,
  };
  return { pair, seed: pair.getSeed(), identity };
}
