import { createServer } from "node:http";
import type { AddressInfo } from "node:net";
import { join } from "node:path";
import { PortalAssets } from "../assets/portal-assets";
import type { TrustedCapabilityInvoker } from "../capabilities/capability-invoker";
import { managementBindingDigest, parseManagementBinding } from "../capabilities/management-binding";
import { AddressingPlatformManagementClient } from "../capabilities/platform-management-client";
import { PlatformManagementResolver } from "../capabilities/platform-management-resolver";
import type { PortalComposerPort } from "../capabilities/portal-composer-client";
import { FileIdentityProvider } from "../identity/file-identity-provider";
import { createPortalHandler } from "../http/portal-handler";
import { createPortalFixture } from "./portal-fixture";
import { writeSessionFixture } from "./session-fixture";

export interface PlatformInvocation { capability: string; operation: string; payload: unknown; logicalService?: string }

export function recordingPlatformInvoker(calls: PlatformInvocation[], response: (capability: string, operation: string) => unknown = () => ({})): TrustedCapabilityInvoker {
  return { async invoke(_principal, route, operation, payload) {
    calls.push({ capability: route.capability, operation, payload: JSON.parse(new TextDecoder().decode(payload)) as unknown, ...(route.logicalService === undefined ? {} : { logicalService: route.logicalService }) });
    return new TextEncoder().encode(JSON.stringify(response(route.capability, operation)));
  } };
}

export async function startPlatformManagementTestServer(invoker: TrustedCapabilityInvoker, roles: string[], rawBinding: Record<string, unknown>): Promise<{
  origin: string; readHeaders: Record<string, string>; writeHeaders: Record<string, string>; close(): Promise<void>;
}> {
  const binding = parseManagementBinding(rawBinding);
  const activation = { tenantId: "tenant-a", portalId: "operations", status: "Current", resolved: {
    id: "operations", tenantId: "tenant-a", domains: ["127.0.0.1"], management: rawBinding,
    resolution: { platformProfile: rawBinding.platformProfile, managementBindingDigest: managementBindingDigest(binding) },
  } };
  const composer: PortalComposerPort = { async call() { return new TextEncoder().encode(JSON.stringify([activation])); } };
  const root = await createPortalFixture();
  const sessionFile = join(root, "sessions.json");
  await writeSessionFixture(sessionFile, "browser-token", new Date(Date.now() + 60_000), roles);
  const assets = await PortalAssets.load(root);
  const identity = await FileIdentityProvider.open(sessionFile);
  const platform = { resolver: new PlatformManagementResolver(composer), client: new AddressingPlatformManagementClient(invoker) };
  const server = createServer(createPortalHandler({ assets, identity, platform, secureCookies: false }));
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  const origin = `http://127.0.0.1:${(server.address() as AddressInfo).port}`;
  const sessionCookie = "vastplan_session=browser-token";
  const csrf = await fetch(`${origin}/v1/csrf`, { headers: { Cookie: sessionCookie } });
  const token = (await csrf.json() as { token: string }).token;
  return {
    origin,
    readHeaders: { Cookie: sessionCookie },
    writeHeaders: { Cookie: `${sessionCookie}; vastplan_csrf=${token}`, "X-VastPlan-CSRF": token, "Content-Type": "application/json" },
    close: () => new Promise<void>((resolve) => server.close(() => resolve())),
  };
}

export function managementBinding(capabilities: unknown[]): Record<string, unknown> {
  return { tenantId: "tenant-a", portalId: "operations", platformProfile: { id: "profile", revision: 1, digest: "a".repeat(64) }, services: [{
    id: "core", logicalService: "platform.core.primary", routingDomain: "platform", capabilities,
  }] };
}
