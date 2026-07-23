import { describe, expect, it, vi } from "vitest";
import type { PlatformAdminClient } from "@vastplan/platform-admin";
import { createArtifactRepositoryPages } from "./index.js";

function clientStub() {
  const listArtifactCatalog = vi.fn(async () => ({ revision: 3, total: 1, page: 1, pageSize: 20, items: [{
    ref: { pluginId: "cn.vastplan.example.demo", version: "1.0.0", channel: "stable" }, sha256: "a".repeat(64), size: 1024,
    publisher: "vastplan", keyId: "release", signedAt: "2026-07-21T00:00:00Z", publishedAt: "2026-07-21T00:00:00Z",
    repositoryRevision: 3, name: "Demo", description: "", namespace: "cn.vastplan.example", targets: ["backend"], lifecycleStatus: "active" as const,
    sbom: { format: "cyclonedx-json" as const, specVersion: "1.5" as const, sha256: "f".repeat(64) },
  }] }));
  const planArtifactGarbageCollection = vi.fn(async () => ({ schemaVersion: "v1" as const, planId: "b".repeat(64), ready: true, createdAt: "2026-07-21T00:00:00Z", catalogRevision: 3, referenceRevision: 2, candidates: [{ ref: { pluginId: "cn.vastplan.example.old", version: "1.0.0", channel: "stable" }, sha256: "c".repeat(64), size: 100, lifecycle: "yanked" as const }], bytes: 100 }));
  const quarantineArtifacts = vi.fn(async () => ({ revision: 1, items: [] }));
  const setArtifactLifecycle = vi.fn(async () => ({ revision: 4, entry: { ref: { pluginId: "cn.vastplan.example.demo", version: "1.0.0", channel: "stable" }, lifecycleStatus: "deprecated", lifecycleRevision: 4 } }));
  const migration = { migrationId: "move-1", phase: "synced", sourceProvider: "file", sourceVolumeId: "primary", targetProvider: "file", targetVolumeId: "next", files: 3, bytes: 4096, digest: "verified", configuredActive: false, canRollback: true, canFinalize: false, canRelease: false };
  const syncArtifactMigration = vi.fn(async () => migration);
  const cutoverArtifactMigration = vi.fn(async () => ({ ...migration, phase: "observing" }));
  const publications = { revision: 2, items: [{ id: "d".repeat(64), revision: 2, status: "PendingApproval" as const, source: { pluginId: "cn.vastplan.example.demo", version: "1.0.0", channel: "testing" }, target: { pluginId: "cn.vastplan.example.demo", version: "1.0.0", channel: "stable" }, sha256: "a".repeat(64), publisher: "vastplan", keyId: "release", sourceAttestationSha256: "e".repeat(64), reason: "ready", submittedBy: "alice", submittedAt: "2026-07-21T00:00:00Z", expiresAt: "2026-07-28T00:00:00Z" }] };
  const submitArtifactPublication = vi.fn(async () => ({ revision: 3, entry: publications.items[0]! }));
  const approveArtifactPublication = vi.fn(async () => ({ revision: 3, entry: { ...publications.items[0]!, status: "Approved" as const } }));
  const rejectArtifactPublication = vi.fn(async () => ({ revision: 3, entry: { ...publications.items[0]!, status: "Rejected" as const } }));
  const cancelArtifactPublication = vi.fn(async () => ({ revision: 3, entry: { ...publications.items[0]!, status: "Cancelled" as const } }));
  return {
    value: {
      listArtifactCatalog,
      artifactRepositoryStatus: vi.fn(async () => ({ ready: true, storageProvider: "file", storageVolumeId: "primary", catalog: { revision: 3, artifacts: 1 } })),
      artifactRepositoryCapacity: vi.fn(async () => ({ catalogRevision: 3, gcRevision: 1, activeArtifacts: 1, activeBytes: 1024, quarantinedArtifacts: 0, quarantinedBytes: 0, sweptArtifacts: 0, reclaimedBytes: 0, storedBytes: 1024, buckets: [], quotas: [] })),
      listArtifactReferences: vi.fn(async () => ({ revision: 1, items: [] })),
      artifactGarbageCollectionStatus: vi.fn(async () => ({ revision: 1, items: [] })),
      planArtifactGarbageCollection,
      quarantineArtifacts,
      sweepArtifacts: vi.fn(async () => ({ revision: 2, items: [] })),
      setArtifactLifecycle,
      artifactMigrationStatus: vi.fn(async () => migration),
      prepareArtifactMigration: vi.fn(async () => migration),
      syncArtifactMigration,
      cutoverArtifactMigration,
      rollbackArtifactMigration: vi.fn(async () => ({ ...migration, phase: "rolled-back" })),
      finalizeArtifactMigration: vi.fn(async () => ({ ...migration, phase: "finalized" })),
      releaseArtifactMigration: vi.fn(async () => ({ ...migration, phase: "released" })),
      listArtifactPublications: vi.fn(async () => publications),
      submitArtifactPublication,
      approveArtifactPublication,
      rejectArtifactPublication,
      cancelArtifactPublication,
      artifactSupplyChainEvidence: vi.fn(async () => ({ ref: { pluginId: "cn.vastplan.example.demo", version: "1.0.0", channel: "stable" }, sha256: "a".repeat(64), size: 1024, publisher: "vastplan", keyId: "release", signedAt: "2026-07-21T00:00:00Z", attestationSha256: "e".repeat(64), verification: "verified", name: "Demo", description: "", targets: ["backend"], engines: { backend: "^0.1" }, repositoryRevision: 3, lifecycleStatus: "active", publications: publications.items })),
    } as unknown as PlatformAdminClient,
    listArtifactCatalog, planArtifactGarbageCollection, quarantineArtifacts, setArtifactLifecycle, syncArtifactMigration, cutoverArtifactMigration, submitArtifactPublication, approveArtifactPublication, rejectArtifactPublication, cancelArtifactPublication,
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
      "platform.artifact-repository.artifacts.migration.collection",
      "platform.artifact-repository.artifacts.publications.collection",
    ]);
    const result = await pages[0]!.load({ mode: "page", page: 1, pageSize: 20, filters: { pluginPrefix: "cn.vastplan", target: "backend", lifecycle: "active" } }, new AbortController().signal);
    expect(result.total).toBe(1);
    expect(result.items[0]).toMatchObject({ sbom: "bound" });
    expect(stub.listArtifactCatalog).toHaveBeenCalledWith(expect.objectContaining({ pluginPrefix: "cn.vastplan", target: "backend", lifecycle: "active", page: 1, pageSize: 20 }));
  });

  it("binds publication submission and approval to current workflow revisions", async () => {
    const stub = clientStub();
    const catalog = createArtifactRepositoryPages(stub.value, "artifacts")[0]!;
    const result = await catalog.load({ mode: "page", page: 1, pageSize: 20, filters: {} }, new AbortController().signal);
    const form = catalog.forms!.find((candidate) => candidate.id === "publication")!;
    const prepared = await form.prepare?.(result.items, new AbortController().signal);
    await form.submit({ value: { reason: "release", expectedRevision: prepared?.initialValue?.expectedRevision }, selected: [{ ...result.items[0]!, channel: "testing" }] }, new AbortController().signal);
    expect(stub.submitArtifactPublication).toHaveBeenCalledWith(expect.objectContaining({ targetChannel: "stable", expectedRevision: 2 }));
    const approvals = createArtifactRepositoryPages(stub.value, "artifacts")[5]!;
    const page = await approvals.load({ mode: "page", page: 1, pageSize: 20, filters: {} }, new AbortController().signal);
    await approvals.runAction?.({ action: approvals.collection.actions![0]!, selected: page.items, refresh: () => undefined }, new AbortController().signal);
    expect(stub.approveArtifactPublication).toHaveBeenCalledWith("d".repeat(64), 2);
    const reject = approvals.forms!.find((form) => form.id === "reject")!;
    await reject.submit({ value: { reason: "risk" }, selected: page.items }, new AbortController().signal);
    expect(stub.rejectArtifactPublication).toHaveBeenCalledWith("d".repeat(64), 2, "risk");
    const cancel = approvals.forms!.find((form) => form.id === "cancel")!;
    await cancel.submit({ value: { reason: "superseded" }, selected: page.items }, new AbortController().signal);
    expect(stub.cancelArtifactPublication).toHaveBeenCalledWith("d".repeat(64), 2, "superseded");
  });

  it("submits lifecycle transitions with the catalog snapshot revision", async () => {
    const stub = clientStub();
    const page = createArtifactRepositoryPages(stub.value, "artifacts")[0]!;
    const result = await page.load({ mode: "page", page: 1, pageSize: 20, filters: {} }, new AbortController().signal);
    const form = page.forms![0]!;
    await form.submit({ value: { status: "deprecated", reason: "use v2", replacementPluginId: "cn.vastplan.example.demo", replacementConstraint: ">=2.0.0" }, selected: [result.items[0]!] }, new AbortController().signal);
    expect(stub.setArtifactLifecycle).toHaveBeenCalledWith({
      ref: { pluginId: "cn.vastplan.example.demo", version: "1.0.0", channel: "stable" },
      status: "deprecated", reason: "use v2", replacement: { pluginId: "cn.vastplan.example.demo", constraint: ">=2.0.0" }, expectedRevision: 3,
    });
  });

  it("exposes only state-machine migration commands through governed actions", async () => {
    const stub = clientStub();
    const page = createArtifactRepositoryPages(stub.value, "artifacts")[4]!;
    const result = await page.load({ mode: "page", page: 1, pageSize: 10, filters: {} }, new AbortController().signal);
    expect(result.items[0]).toMatchObject({ migrationId: "move-1", phase: "synced", canRollback: true });
    const sync = page.collection.actions!.find((action) => action.id === "sync")!;
    await page.runAction?.({ action: sync, selected: result.items, refresh: () => undefined }, new AbortController().signal);
    expect(stub.syncArtifactMigration).toHaveBeenCalledWith("move-1");
    const cutover = page.forms!.find((form) => form.id === "cutover")!;
    await cutover.submit({ value: { observationSeconds: 300 }, selected: result.items }, new AbortController().signal);
    expect(stub.cutoverArtifactMigration).toHaveBeenCalledWith("move-1", 300);
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
