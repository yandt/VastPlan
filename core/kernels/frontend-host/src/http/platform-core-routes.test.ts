import { createServer } from "node:http";
import type { AddressInfo } from "node:net";
import { join } from "node:path";
import { afterEach, describe, expect, it } from "vitest";
import { PortalAssets } from "../assets/portal-assets";
import type { TrustedCapabilityInvoker } from "../capabilities/capability-invoker";
import { managementBindingDigest, parseManagementBinding } from "../capabilities/management-binding";
import { AddressingPlatformManagementClient } from "../capabilities/platform-management-client";
import { PlatformManagementResolver } from "../capabilities/platform-management-resolver";
import type { PortalComposerPort } from "../capabilities/portal-composer-client";
import { FileIdentityProvider } from "../identity/file-identity-provider";
import { createPortalFixture } from "../testing/portal-fixture";
import { writeSessionFixture } from "../testing/session-fixture";
import { createPortalHandler } from "./portal-handler";

const servers: ReturnType<typeof createServer>[] = [];
afterEach(async () => Promise.all(servers.splice(0).map((server) => new Promise<void>((resolve) => server.close(() => resolve())))));

describe("Platform core management routes", () => {
  it("routes Settings, Credentials and Database through verified server-owned targets", async () => {
    const calls: { capability: string; operation: string; payload: unknown; logicalService?: string }[] = [];
    const invoker = fakeInvoker(calls);
    const { origin, readHeaders, writeHeaders } = await startServer(invoker, ["platform.admin"], fullBinding());
    expect((await fetch(`${origin}/v1/portals/operations/platform/services/core/settings?prefix=ui.`, { headers: readHeaders })).status).toBe(200);
    expect((await fetch(`${origin}/v1/portals/operations/platform/services/core/settings/ui.theme`, { method: "PUT", headers: writeHeaders, body: '{"key":"forged","value":"dark","ifVersion":2}' })).status).toBe(200);
    expect((await fetch(`${origin}/v1/portals/operations/platform/services/core/settings/ui.theme?ifVersion=2`, { method: "DELETE", headers: writeHeaders })).status).toBe(200);
    expect((await fetch(`${origin}/v1/portals/operations/platform/services/core/credentials/vault.db`, { method: "PUT", headers: writeHeaders, body: '{"name":"forged","value":"secret"}' })).status).toBe(200);
    expect((await fetch(`${origin}/v1/portals/operations/platform/services/core/credentials/vault.db/rotate`, { method: "POST", headers: writeHeaders, body: "{}" })).status).toBe(200);
    expect((await fetch(`${origin}/v1/portals/operations/platform/services/core/database-connections/main`, { method: "PUT", headers: writeHeaders, body: '{"name":"forged","providerId":"postgres","endpoint":"db:5432","options":{}}' })).status).toBe(200);
    expect((await fetch(`${origin}/v1/portals/operations/platform/services/core/database-connections/main/probe`, { method: "POST", headers: writeHeaders, body: "{}" })).status).toBe(200);
    expect(calls).toEqual([
      { capability: "platform.settings", operation: "list", payload: { prefix: "ui." }, logicalService: "platform.core.primary" },
      { capability: "platform.settings", operation: "put", payload: { key: "ui.theme", value: "dark", ifVersion: 2 }, logicalService: "platform.core.primary" },
      { capability: "platform.settings", operation: "delete", payload: { key: "ui.theme", ifVersion: 2 }, logicalService: "platform.core.primary" },
      { capability: "platform.credentials", operation: "put", payload: { name: "vault.db", value: "secret" }, logicalService: "platform.core.primary" },
      { capability: "platform.credentials", operation: "rotate", payload: { name: "vault.db" }, logicalService: "platform.core.primary" },
      { capability: "platform.database", operation: "define", payload: { name: "main", providerId: "postgres", endpoint: "db:5432", options: {} }, logicalService: "platform.core.primary" },
      { capability: "platform.database", operation: "probe", payload: { name: "main" }, logicalService: "platform.core.primary" },
    ]);
  });

  it("enforces Binding grants before roles and rejects malformed path resources", async () => {
    const calls: { capability: string; operation: string; payload: unknown }[] = [];
    const binding = fullBinding();
    const service = (binding.services as Record<string, unknown>[])[0]!;
    service.capabilities = [{ capability: "platform.settings", read: ["list"] }];
    const { origin, writeHeaders } = await startServer(fakeInvoker(calls), ["platform.admin"], binding);
    const denied = await fetch(`${origin}/v1/portals/operations/platform/services/core/settings/ui.theme`, { method: "PUT", headers: writeHeaders, body: '{"value":"dark"}' });
    expect(denied.status).toBe(403);
    expect(await denied.json()).toEqual({ error: "management_binding_forbidden" });
    const invalid = await fetch(`${origin}/v1/portals/operations/platform/services/core/credentials/%2Fsecret`, { method: "PUT", headers: writeHeaders, body: '{"value":"x"}' });
    expect(invalid.status).toBe(400);
    expect(calls).toEqual([]);
  });
});

function fakeInvoker(calls: { capability: string; operation: string; payload: unknown; logicalService?: string }[]): TrustedCapabilityInvoker {
  return { async invoke(_principal, route, operation, payload) {
    calls.push({ capability: route.capability, operation, payload: JSON.parse(new TextDecoder().decode(payload)) as unknown, ...(route.logicalService === undefined ? {} : { logicalService: route.logicalService }) });
    const value = route.capability === "platform.settings" && operation === "list" ? { items: [] } : {};
    return new TextEncoder().encode(JSON.stringify(value));
  } };
}

async function startServer(invoker: TrustedCapabilityInvoker, roles: string[], rawBinding: Record<string, unknown>): Promise<{ origin: string; readHeaders: Record<string, string>; writeHeaders: Record<string, string> }> {
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
  servers.push(server);
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  const origin = `http://127.0.0.1:${(server.address() as AddressInfo).port}`;
  const sessionCookie = "vastplan_session=browser-token";
  const csrf = await fetch(`${origin}/v1/csrf`, { headers: { Cookie: sessionCookie } });
  const token = (await csrf.json() as { token: string }).token;
  return { origin, readHeaders: { Cookie: sessionCookie }, writeHeaders: { Cookie: `${sessionCookie}; vastplan_csrf=${token}`, "X-VastPlan-CSRF": token, "Content-Type": "application/json" } };
}

function fullBinding(): Record<string, unknown> {
  return { tenantId: "tenant-a", portalId: "operations", platformProfile: { id: "profile", revision: 1, digest: "a".repeat(64) }, services: [{
    id: "core", logicalService: "platform.core.primary", routingDomain: "platform", capabilities: [
      { capability: "platform.settings", read: ["list"], write: ["put", "delete"] },
      { capability: "platform.credentials", read: ["list"], write: ["put", "rotate", "revoke"] },
      { capability: "platform.database", read: ["list"], write: ["define", "remove", "probe"] },
    ],
  }] };
}
