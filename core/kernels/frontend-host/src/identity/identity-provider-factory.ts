import type { PortalIdentityConfig } from "../config/identity-config";
import { FileIdentityProvider } from "./file-identity-provider";
import type { IdentityProvider } from "./identity-provider";
import { BrokerIdentityProvider } from "./broker-identity-provider";
import type { AccessCatalogPort } from "../access/access-catalog-port";
import type { AuthenticationBrokerPort } from "./authentication-broker-port";
import type { SessionAuthorizationPort } from "./session-authorization-port";

export interface BrokerIdentityDependencies {
  readonly access?: AccessCatalogPort;
  readonly broker?: AuthenticationBrokerPort;
  readonly authorization?: SessionAuthorizationPort;
}

export function openIdentityProvider(config: PortalIdentityConfig, dependencies: BrokerIdentityDependencies = {}): Promise<IdentityProvider> {
  if (config.kind === "file") return FileIdentityProvider.open(config.sessionFile);
  if (dependencies.access === undefined || dependencies.broker === undefined || dependencies.authorization === undefined) throw new Error("Broker 身份模式需要 Access Catalog、Authentication Broker 与 Authorization Session");
  return BrokerIdentityProvider.open(config, dependencies.access, dependencies.broker, dependencies.authorization);
}
