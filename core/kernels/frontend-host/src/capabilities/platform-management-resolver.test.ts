import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";
import type { PortalComposerPort } from "./portal-composer-client";
import { ManagementResolutionError, PlatformManagementResolver } from "./platform-management-resolver";

const fixturePath = resolve(fileURLToPath(new URL("../../../../../contracts/testdata/management-binding-v1.json", import.meta.url)));

describe("PlatformManagementResolver", () => {
  it("resolves only the current tenant, domain, audience and digest-bound service", async () => {
    const fixture = JSON.parse(readFileSync(fixturePath, "utf8")) as { binding: Record<string, unknown>; digest: string };
    const composer = composerWith([activation(fixture.binding, fixture.digest)]);
    const resolver = new PlatformManagementResolver(composer);
    const target = await resolver.resolve({ id: "alice", tenantId: "tenant-a", roles: ["platform.settings.read"] }, "operations", "settings", "portal.example");
    expect(target.service).toMatchObject({ id: "settings", logicalService: "platform.settings.primary", routingDomain: "platform" });
  });

  it("fails closed for audience and binding digest drift", async () => {
    const fixture = JSON.parse(readFileSync(fixturePath, "utf8")) as { binding: Record<string, unknown>; digest: string };
    const principal = { id: "alice", tenantId: "tenant-a", roles: [] as string[] };
    const denied = new PlatformManagementResolver(composerWith([activation(fixture.binding, fixture.digest, ["platform.settings.read"])]));
    await expect(denied.resolve(principal, "operations", "settings", "portal.example")).rejects.toEqual(expect.objectContaining<Partial<ManagementResolutionError>>({ code: "portal_audience_forbidden" }));
    const tampered = new PlatformManagementResolver(composerWith([activation(fixture.binding, "b".repeat(64))]));
    await expect(tampered.resolve(principal, "operations", "settings", "portal.example")).rejects.toEqual(expect.objectContaining<Partial<ManagementResolutionError>>({ code: "portal_management_binding_rejected" }));
  });
});

function composerWith(activations: unknown[]): PortalComposerPort {
  return { async call(_principal, operation) {
    expect(operation).toBe("listActivations");
    return new TextEncoder().encode(JSON.stringify(activations));
  } };
}

function activation(binding: Record<string, unknown>, digest: string, audience: string[] = []): unknown {
  return {
    tenantId: "tenant-a", portalId: "operations", status: "Current",
    resolved: {
      id: "operations", tenantId: "tenant-a", domains: ["portal.example"], audience,
      management: binding,
      resolution: { platformProfile: binding.platformProfile, managementBindingDigest: digest },
    },
  };
}
