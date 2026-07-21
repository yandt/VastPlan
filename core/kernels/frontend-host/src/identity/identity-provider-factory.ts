import type { PortalIdentityConfig } from "../config/identity-config";
import { FileIdentityProvider } from "./file-identity-provider";
import type { IdentityProvider } from "./identity-provider";
import { OIDCIdentityProvider } from "./oidc-identity-provider";

export function openIdentityProvider(config: PortalIdentityConfig): Promise<IdentityProvider> {
  return config.kind === "file" ? FileIdentityProvider.open(config.sessionFile) : OIDCIdentityProvider.open(config);
}
