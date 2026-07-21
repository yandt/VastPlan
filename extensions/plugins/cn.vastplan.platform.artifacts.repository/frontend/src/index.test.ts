import { describe, expect, it, vi } from "vitest";
import type { PlatformAdminClient } from "@vastplan/platform-admin";
import { createArtifactRepositoryPages } from "./index.js";

function clientStub() {
  const listArtifactCatalog = vi.fn(async () => ({ revision: 3, total: 1, page: 1, pageSize: 20, items: [{
    ref: { pluginId: "cn.vastplan.example.demo", version: "1.0.0", channel: "stable" }, sha256: "a".repeat(64), size: 1024,
    publisher: "vastplan", keyId: "release", signedAt: "2026-07-21T00:00:00Z", publishedAt: "2026-07-21T00:00:00Z",
    repositoryRevision: 3, name: "Demo", description: "", namespace: "cn.vastplan.example", targets: ["backend"], lifecycleStatus: "active" as const,
  }] }));
  const planArtifactGarbageCollection = vi.fn(async () => ({ schemaVersion: "v1" as const, planId: "b".repeat(64), ready: true, createdAt: "2026-07-21T00:00:00Z", catalogRevision: 3, referenceRevision: 2, candidates: [{ ref: { pluginId: "cn.vastplan.example.old", version: "1.0.0", channel: "stable" }, sha256: "c".repeat(64), size: 100, lifecycle: "yanked" as const }], bytes: 100 }));
  const quarantineArtifacts = vi.fn(async () => ({ revision: 1, items: [] }));
  return {
    value: {
      listArtifactCatalog,
      artifactRepositoryStatus: vi.fn(async () => ({ ready: true, catalog: { revision: 3, artifacts: 1 } })),
      artifactRepositoryCapacity: vi.fn(async () => ({ catalogRevision: 3, gcRevision: 1, activeArtifacts: 1, activeBytes: 1024, quarantinedArtifacts: 0, quarantinedBytes: 0, sweptArtifacts: 0, reclaimedBytes: 0, storedBytes: 1024, buckets: [], quotas: [] })),
      listArtifactReferences: vi.fn(async () => ({ revision: 1, items: [] })),
      artifactGarbageCollectionStatus: vi.fn(async () => ({ revision: 1, items: [] })),
      planArtifactGarbageCollection,
      quarantineArtifacts,
      sweepArtifacts: vi.fn(async () => ({ revision: 2, items: [] })),
    } as unknown as PlatformAdminClient,
    listArtifactCatalog, planArtifactGarbageCollection, quarantineArtifacts,
  };
}

describe("artifact repository Workbench", () => {
  it("registers only governed collection pages and maps catalog filters", async () => {
    const stub = clientStub();
    const pages = createArtifactRepositoryPages(stub.value, "artifacts");
    expect(pages.map((page) => page.collection.id)).toEqual([
      "platform.artifact-repository.artifacts.catalog.collection",
      "platform.artifact-repository.artifacts.capacity.collection",
      "platform.artifact-repository.artifacts.references.collection",
      "platform.artifact-repository.artifacts.gc.collection",
    ]);
    const result = await pages[0]!.load({ mode: "page", page: 1, pageSize: 20, filters: { pluginPrefix: "cn.vastplan", target: "backend", lifecycle: "active" } }, new AbortController().signal);
    expect(result.total).toBe(1);
    expect(stub.listArtifactCatalog).toHaveBeenCalledWith(expect.objectContaining({ pluginPrefix: "cn.vastplan", target: "backend", lifecycle: "active", page: 1, pageSize: 20 }));
  });

  it("regenerates the GC plan immediately before quarantine", async () => {
    const stub = clientStub();
    const page = createArtifactRepositoryPages(stub.value, "artifacts")[3]!;
    await page.runAction?.({ action: page.collection.actions![0]!, selected: [], refresh: () => undefined }, new AbortController().signal);
    expect(stub.planArtifactGarbageCollection).toHaveBeenCalledOnce();
    expect(stub.quarantineArtifacts).toHaveBeenCalledWith("b".repeat(64), 72);
  });

  it("treats legacy null GC collections as empty during rolling upgrades", async () => {
    const stub = clientStub();
    Object.assign(stub.value, {
      artifactGarbageCollectionStatus: vi.fn(async () => ({ revision: 1, items: null })),
      planArtifactGarbageCollection: vi.fn(async () => ({ schemaVersion: "v1", ready: false, createdAt: "2026-07-21T00:00:00Z", catalogRevision: 0, referenceRevision: 0, candidates: null, bytes: 0, blockers: [] })),
    });
    const page = createArtifactRepositoryPages(stub.value, "artifacts")[3]!;
    await expect(page.load({ mode: "page", page: 1, pageSize: 20, filters: {} }, new AbortController().signal)).resolves.toEqual({ items: [], total: 0 });
    await expect(page.loadSummary?.(new AbortController().signal)).resolves.toMatchObject({ metrics: expect.arrayContaining([expect.objectContaining({ id: "candidates", value: 0 })]) });
  });
});
