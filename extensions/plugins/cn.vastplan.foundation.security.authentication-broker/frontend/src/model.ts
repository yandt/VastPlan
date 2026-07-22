import type { ManagedAuthenticationProvider } from "@vastplan/platform-admin";

export const namespace =
  "cn.vastplan.foundation.security.authentication-broker";

export type ProviderRow = ManagedAuthenticationProvider & {
  generation: number;
  id: string;
  contributionId: string;
  state: string;
  readiness: string;
  updatedAt: string;
} & Record<string, unknown>;
