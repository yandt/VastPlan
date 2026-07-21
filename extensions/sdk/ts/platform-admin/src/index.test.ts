import { describe, expect, it } from "vitest";
import { PlatformAdminClient, PlatformAdminError, type PlatformFetch } from "./index";

describe("PlatformAdminClient", () => {
  it("obtains CSRF before a write and never places credential plaintext in the URL", async () => {
    const calls: Array<{ path: string; body?: string }> = [];
    const fetcher: PlatformFetch = async (path, init) => {
      calls.push({ path, body: init?.body });
      return { ok: true, status: 200, json: async () => path === "/v1/csrf" ? { token: "safe" } : { name: "vault.db", version: 1 } };
    };
	await new PlatformAdminClient(fetcher, "operations", "credentials").putCredential("vault.db", "top-secret");
    expect(calls).toHaveLength(2);
	expect(calls[1].path).toBe("/v1/portals/operations/platform/services/credentials/credentials/vault.db");
    expect(calls[1].path).not.toContain("top-secret");
    expect(calls[1].body).toContain("top-secret");
  });

  it("rejects ambiguous path names locally", async () => {
	const client = new PlatformAdminClient(async () => ({ ok: true, status: 200, json: async () => ({}) }), "operations", "settings");
    expect(() => client.deleteSetting("bad/name")).toThrowError(PlatformAdminError);
  });

  it("uses fixed deployment routes and obtains CSRF before bootstrap approval", async () => {
    const calls: Array<{ path: string; method?: string }> = [];
    const fetcher: PlatformFetch = async (path, init) => {
      calls.push({ path, method: init?.method });
      return { ok: true, status: 200, json: async () => path === "/v1/csrf" ? { token: "safe" } : { id: "bootstrap-1", state: "SystemdActive" } };
    };
	const client = new PlatformAdminClient(fetcher, "operations", "deployment");
    await client.approveBootstrapJob("bootstrap-1");
    expect(calls).toEqual([
      { path: "/v1/csrf", method: "GET" },
		{ path: "/v1/portals/operations/platform/services/deployment/deployment/bootstrap-jobs/bootstrap-1/approve", method: "POST" },
    ]);
  });

  it("keeps online service composition on portal-scoped fixed routes", async () => {
    const calls: Array<{ path: string; method?: string }> = [];
    const fetcher: PlatformFetch = async (path, init) => {
      calls.push({ path, method: init?.method });
      return { ok: true, status: 200, json: async () => path === "/v1/csrf" ? { token: "safe" } : { id: 4, status: "Published" } };
    };
    const client = new PlatformAdminClient(fetcher, "operations", "deployment");
    await client.publishServiceRevision(4);
    expect(calls).toEqual([
      { path: "/v1/csrf", method: "GET" },
      { path: "/v1/portals/operations/platform/services/deployment/deployment/service-revisions/4/publish", method: "POST" },
    ]);
    expect(() => client.rollbackServiceRevision(0)).toThrowError(PlatformAdminError);
  });

  it("publishes an exact test artifact through the deployment BFF", async () => {
    const calls: Array<{ path: string; method?: string; body?: string }> = [];
    const fetcher: PlatformFetch = async (path, init) => {
      calls.push({ path, method: init?.method, body: init?.body });
      return { ok: true, status: 200, json: async () => path === "/v1/csrf" ? { token: "safe" } : { id: 1, status: "Ready" } };
    };
    const client = new PlatformAdminClient(fetcher, "operations", "deployment");
    await client.createTestRelease({
      bindingId: "demo-api",
      artifact: { pluginId: "cn.example.demo", version: "1.1.0-dev.1", channel: "testing" },
      sha256: "a".repeat(64), repositoryRevision: 17,
    });
    expect(calls[1]).toEqual({
      path: "/v1/portals/operations/platform/services/deployment/deployment/test-releases",
      method: "POST",
      body: expect.stringContaining('"repositoryRevision":17'),
    });
  });

  it("uses fixed CSRF-protected artifact migration actions", async () => {
    const calls: Array<{ path: string; method?: string; body?: string }> = [];
    const fetcher: PlatformFetch = async (path, init) => {
      calls.push({ path, method: init?.method, body: init?.body });
      return { ok: true, status: 200, json: async () => path === "/v1/csrf" ? { token: "safe" } : { migrationId: "repository.move-001", phase: "observing" } };
    };
    const client = new PlatformAdminClient(fetcher, "operations", "artifacts");
    await client.cutoverArtifactMigration("repository.move-001", 300);
    expect(calls).toEqual([
      { path: "/v1/csrf", method: "GET", body: undefined },
      { path: "/v1/portals/operations/platform/services/artifacts/artifacts/migrations/repository.move-001/cutover", method: "POST", body: '{"observationSeconds":300}' },
    ]);
    expect(() => client.cutoverArtifactMigration("repository.move-001", 0)).toThrowError(PlatformAdminError);
    expect(() => client.releaseArtifactMigration("bad/id")).toThrowError(PlatformAdminError);
    await client.setArtifactLifecycle({ ref: { pluginId: "cn.example.demo", version: "1.0.0", channel: "stable" }, status: "deprecated", reason: "use v2", expectedRevision: 17 });
    expect(calls[3]).toEqual({
      path: "/v1/portals/operations/platform/services/artifacts/artifacts/lifecycle",
      method: "POST",
      body: expect.stringContaining('"expectedRevision":17'),
    });
  });

  it("reads artifact reference protection through a fixed BFF route", async () => {
    const calls: string[] = [];
    const client = new PlatformAdminClient(async (path) => {
      calls.push(path);
      return { ok: true, status: 200, json: async () => ({ revision: 1, items: [] }) };
    }, "operations", "artifacts");
    await client.listArtifactReferences();
    expect(calls).toEqual(["/v1/portals/operations/platform/services/artifacts/artifacts/references"]);
  });

  it("reads verified artifact capacity through a fixed BFF route", async () => {
    const calls: string[] = [];
    const client = new PlatformAdminClient(async (path) => {
      calls.push(path);
      return { ok: true, status: 200, json: async () => ({ activeArtifacts: 2, activeBytes: 100, buckets: [], quotas: [] }) };
    }, "operations", "artifacts");
    await client.artifactRepositoryCapacity();
    expect(calls).toEqual(["/v1/portals/operations/platform/services/artifacts/artifacts/capacity"]);
  });

  it("keeps artifact GC on fixed read and CSRF-protected mutation routes", async () => {
    const calls: Array<{ path: string; method?: string; body?: string }> = [];
    const fetcher: PlatformFetch = async (path, init) => {
      calls.push({ path, method: init?.method, body: init?.body });
      return { ok: true, status: 200, json: async () => path === "/v1/csrf" ? { token: "safe" } : { revision: 1, items: [] } };
    };
    const client = new PlatformAdminClient(fetcher, "operations", "artifacts");
    await client.planArtifactGarbageCollection();
    await client.quarantineArtifacts("a".repeat(64), 72);
    expect(calls).toEqual([
      { path: "/v1/portals/operations/platform/services/artifacts/artifacts/gc/plan", method: "GET", body: undefined },
      { path: "/v1/csrf", method: "GET", body: undefined },
      { path: "/v1/portals/operations/platform/services/artifacts/artifacts/gc/quarantine", method: "POST", body: JSON.stringify({ planId: "a".repeat(64), graceHours: 72 }) },
    ]);
    expect(() => client.quarantineArtifacts("bad", 72)).toThrowError(PlatformAdminError);
  });

  it("uses a fixed catalog route with encoded filters and bounded pagination", async () => {
		const calls: Array<{ path: string; method?: string }> = [];
		const fetcher: PlatformFetch = async (path, init) => {
			calls.push({ path, method: init?.method });
			return { ok: true, status: 200, json: async () => ({ revision: 1, total: 0, page: 2, pageSize: 10, items: [] }) };
		};
		const client = new PlatformAdminClient(fetcher, "operations", "artifacts");
		await client.listArtifactCatalog({ pluginPrefix: "cn.vastplan", target: "backend", lifecycle: "active", page: 2, pageSize: 10 });
		expect(calls[0]).toEqual({ path: "/v1/portals/operations/platform/services/artifacts/artifacts/catalog?pluginPrefix=cn.vastplan&target=backend&lifecycle=active&page=2&pageSize=10", method: "GET" });
		expect(() => client.listArtifactCatalog({ pageSize: 101 })).toThrowError(PlatformAdminError);
	});
});
