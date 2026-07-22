import { chmod, mkdtemp, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { describe, expect, it } from "vitest";
import { FileAccessProfileCatalog } from "./file-access-profile-catalog";

describe("FileAccessProfileCatalog", () => {
  it("resolves host and longest route into an immutable public generation", async () => {
    const path = await writeCatalog([profile("root-login", "/"), profile("admin-login", "/admin")]);
    const catalog = await FileAccessProfileCatalog.open(path);
    const generation = await catalog.resolve("PORTAL.EXAMPLE.TEST.", "/admin/settings");
    expect(generation?.profile.id).toBe("admin-login");
    expect(generation?.id).toMatch(/^[a-f0-9]{64}$/);
    expect(generation?.bootstrap).toMatchObject({
      schemaVersion: "v1", accessTemplate: "access",
      localization: { defaultLocale: "zh-CN", supportedLocales: ["zh-CN", "en-US"] },
      authentication: { allowedMethods: ["password", "one-time-code"], defaultMethod: "password" },
    });
    expect(generation?.bootstrap).not.toHaveProperty("tenantId");
    expect(generation?.bootstrap).not.toHaveProperty("portalId");
    expect(generation?.bootstrap).not.toHaveProperty("platformProfile");
    expect(generation?.bootstrap.branding).not.toHaveProperty("logoSha256");
    expect(await catalog.resolve("attacker.example.test", "/")).toBeUndefined();
  });

  it("rejects writable and ambiguous trusted catalog files", async () => {
    const writable = await writeCatalog([profile("root-login", "/")]);
    await chmod(writable, 0o666);
    await expect(FileAccessProfileCatalog.open(writable)).rejects.toThrow(/不可由组或其他用户写入/);

    const duplicate = await writeCatalog([profile("one", "/"), profile("two", "/")]);
    await expect(FileAccessProfileCatalog.open(duplicate)).rejects.toThrow(/路由冲突/);
  });
});

async function writeCatalog(profiles: readonly ReturnType<typeof profile>[]): Promise<string> {
  const root = await mkdtemp(join(tmpdir(), "vastplan-access-catalog-"));
  const path = join(root, "catalog.json");
  await writeFile(path, JSON.stringify({ version: 1, revision: 1, id: "local-access", profiles }), { mode: 0o600 });
  await chmod(path, 0o600);
  return path;
}

function profile(id: string, route: string) {
  return {
    version: 1, revision: 1, id, tenantId: "acme", portalId: id, route, domains: ["portal.example.test"],
    platformProfile: { id: "portal-default", revision: 2, digest: "a".repeat(64) }, accessTemplate: "access",
    localization: { defaultLocale: "zh-CN", supportedLocales: ["zh-CN", "en-US"] },
    authentication: { allowedMethods: ["password", "one-time-code"], defaultMethod: "password", reuseIdentifier: true },
    branding: { productName: { "zh-CN": "VastPlan", "en-US": "VastPlan" }, logoAssetId: "vastplan", logoSha256: "b".repeat(64) },
  } as const;
}
