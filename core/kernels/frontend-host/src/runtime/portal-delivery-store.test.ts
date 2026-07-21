import { readFile, writeFile } from "node:fs/promises";
import { describe, expect, it } from "vitest";
import { objectPath, snapshotPath } from "./portal-delivery-paths";
import { PortalDeliveryStore } from "./portal-delivery-store";
import { createPortalDeliveryFixture, writePortalDeliveryRevision } from "../testing/portal-delivery-fixture";

describe("PortalDeliveryStore", () => {
  it("cold-fills a complete immutable revision and verifies raw and gzip bytes", async () => {
    const fixture = await createPortalDeliveryFixture();
    const revision = await writePortalDeliveryRevision(fixture, { revision: 7, id: "operations", tenantId: "tenant-a", route: "/operations" });
    const store = await PortalDeliveryStore.open(fixture.cache, fixture.origin);

    const runtime = await store.runtime("tenant-a", revision.spec);
    expect(runtime.modules?.[0]?.sha256).toBe(revision.digest);
    const object = await store.object("tenant-a", revision.spec, revision.digest);
    expect(new TextDecoder().decode(object.content)).toBe("export const ready = true;\n");
    expect(object.gzipContent).toBeDefined();

    await expect(readFile(objectPath(fixture.cache, revision.digest))).resolves.toEqual(Buffer.from(revision.content));
    await expect(readFile(snapshotPath(fixture.cache, "tenant-a", "operations", 7))).resolves.toBeInstanceOf(Buffer);
  });

  it("rejects snapshots not bound to the trusted active Portal and unlisted objects", async () => {
    const fixture = await createPortalDeliveryFixture();
    const revision = await writePortalDeliveryRevision(fixture, { revision: 7, id: "operations", tenantId: "tenant-a", route: "/operations" });
    const store = await PortalDeliveryStore.open(fixture.cache, fixture.origin);
    await expect(store.runtime("tenant-a", { ...revision.spec, route: "/forged" })).rejects.toThrow(/解析锁/);
    await expect(store.object("tenant-a", revision.spec, "a".repeat(64))).rejects.toThrow(/未授权/);
  });

  it("rejects corrupt content before exposing it through the cache", async () => {
    const fixture = await createPortalDeliveryFixture();
    const revision = await writePortalDeliveryRevision(fixture, { revision: 7, id: "operations", tenantId: "tenant-a", route: "/operations" });
    await writeFile(objectPath(fixture.origin, revision.digest), "tampered");
    const store = await PortalDeliveryStore.open(fixture.cache, fixture.origin);
    await expect(store.runtime("tenant-a", revision.spec)).rejects.toThrow(/摘要失配/);
    await expect(readFile(snapshotPath(fixture.cache, "tenant-a", "operations", 7))).rejects.toMatchObject({ code: "ENOENT" });
  });
});
