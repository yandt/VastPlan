import type { IncomingMessage, ServerResponse } from "node:http";
import type { SignedAuthenticationAssertion } from "./signed-authentication-assertion";

export interface Principal {
  id: string;
  tenantId: string;
  portalId?: string;
  roles: readonly string[];
  authenticationProfileId?: string;
  requiresCSRF?: boolean;
  system?: boolean;
}

export interface IdentityProvider {
	authenticate(request: IncomingMessage): Promise<Principal>;
	authenticationProof?(request: IncomingMessage): Promise<SignedAuthenticationAssertion | undefined>;
	authenticationTestProof?(request: IncomingMessage): Promise<SignedAuthenticationAssertion | undefined>;
	handle?(request: IncomingMessage, response: ServerResponse, path: string, secureCookies: boolean): Promise<boolean>;
	loginRedirect?(path: string): string;
}

export class SessionRejectedError extends Error {
  public constructor() {
    super("Portal session 无效或已过期");
    this.name = "SessionRejectedError";
  }
}
