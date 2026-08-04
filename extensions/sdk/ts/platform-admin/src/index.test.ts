import { describe, expect, it, vi } from "vitest";
import { ManagementAPIClient, PlatformAdminClient, PlatformAdminError, type PlatformFetch } from "./index";

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

  it("reuses a CSRF token until fifteen idle minutes have elapsed", async () => {
    vi.useFakeTimers();
    try {
      vi.setSystemTime(new Date("2026-08-04T12:00:00Z"));
      const calls: string[] = [];
      const client = new PlatformAdminClient(async (path) => {
        calls.push(path);
        return { ok: true, status: 200, json: async () => path === "/v1/csrf" ? { token: "safe" } : { key: "theme", value: "dark" } };
      }, "operations", "settings");

      await client.putSetting("theme", "dark");
      vi.setSystemTime(new Date("2026-08-04T12:14:00Z"));
      await client.putSetting("theme", "light");
      vi.setSystemTime(new Date("2026-08-04T12:28:00Z"));
      await client.putSetting("theme", "system");
      expect(calls.filter((path) => path === "/v1/csrf")).toHaveLength(1);

      vi.setSystemTime(new Date("2026-08-04T12:43:01Z"));
      await client.putSetting("theme", "dark");
      expect(calls.filter((path) => path === "/v1/csrf")).toHaveLength(2);
    } finally {
      vi.useRealTimers();
    }
  });

  it("refreshes once and safely retries when the host rejected CSRF before routing", async () => {
    const calls: string[] = [];
    let writes = 0;
    const client = new PlatformAdminClient(async (path) => {
      calls.push(path);
      if (path === "/v1/csrf") return { ok: true, status: 200, json: async () => ({ token: `safe-${calls.length}` }) };
      writes += 1;
      return writes === 1
        ? { ok: false, status: 403, json: async () => ({ error: "csrf_rejected" }) }
        : { ok: true, status: 200, json: async () => ({ key: "theme", value: "dark" }) };
    }, "operations", "settings");

    await client.putSetting("theme", "dark");
    expect(calls).toEqual([
      "/v1/csrf",
      "/v1/portals/operations/platform/services/settings/settings/theme",
      "/v1/csrf",
      "/v1/portals/operations/platform/services/settings/settings/theme",
    ]);
  });

  it("rejects ambiguous path names locally", async () => {
	const client = new PlatformAdminClient(async () => ({ ok: true, status: 200, json: async () => ({}) }), "operations", "settings");
    expect(() => client.deleteSetting("bad/name")).toThrowError(PlatformAdminError);
  });

  it("tests current database form values through a fixed CSRF-protected route", async () => {
    const calls: Array<{ path: string; method?: string; body?: string }> = [];
    const client = new PlatformAdminClient(async (path, init) => {
      calls.push({ path, method: init?.method, body: init?.body });
      return { ok: true, status: 200, json: async () => path === "/v1/csrf" ? { token: "safe" } : { ready: true, providerId: "postgresql", latencyMs: 8 } };
    }, "operations", "database");
    await client.testDatabaseConnection("main", { providerId: "postgresql", endpoint: "db:5432", options: { user: "app" }, credentialValue: "one-time" });
    expect(calls).toEqual([
      { path: "/v1/csrf", method: "GET", body: undefined },
      { path: "/v1/portals/operations/platform/services/database/database-connections/main/test", method: "POST", body: JSON.stringify({ providerId: "postgresql", endpoint: "db:5432", options: { user: "app" }, credentialValue: "one-time" }) },
    ]);
    expect(calls[1]!.path).not.toContain("one-time");
  });

  it("reads redacted managed credential audit through a bounded fixed route", async () => {
    const calls: string[] = [];
    const client = new PlatformAdminClient(async (path) => {
      calls.push(path);
      return { ok: true, status: 200, json: async () => ({ items: [], maintenance: { autoAborted: 0, collected: 0, counts: {} } }) };
    }, "operations", "credentials");
    await client.listManagedCredentialAudit(42, 50);
    expect(calls).toEqual(["/v1/portals/operations/platform/services/credentials/credentials/managed-audit?beforeId=42&limit=50"]);
    expect(() => client.listManagedCredentialAudit(0, 50)).toThrowError(PlatformAdminError);
    expect(() => client.listManagedCredentialAudit(undefined, 201)).toThrowError(PlatformAdminError);
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

  it("keeps API Exposure lifecycle on fixed CSRF-protected routes",async()=>{
    const calls:Array<{path:string;method?:string}>=[];
    const client=new PlatformAdminClient(async(path,init)=>{calls.push({path,method:init?.method});return {ok:true,status:200,json:async()=>path==="/v1/csrf"?{token:"safe"}:{id:7,status:"Approved"}};},"operations","core");
    await client.approveAPIExposure(7);
    expect(calls).toEqual([{path:"/v1/csrf",method:"GET"},{path:"/v1/portals/operations/platform/services/core/api-exposures/7/approve",method:"POST"}]);
  });

  it("keeps Data Plane Exposure retirement on a fixed CSRF-protected route", async () => {
    const calls: Array<{ path: string; method?: string }> = [];
    const client = new PlatformAdminClient(async (path, init) => {
      calls.push({ path, method: init?.method });
      return { ok: true, status: 200, json: async () => path === "/v1/csrf" ? { token: "safe" } : { retired: true } };
    }, "operations", "api-exposure");
    await client.retireDataPlaneExposure("dpx_aaaaaaaaaaaaaaaaaaaa");
    expect(calls).toEqual([
      { path: "/v1/csrf", method: "GET" },
      { path: "/v1/portals/operations/platform/services/api-exposure/data-plane-exposures/exposure/dpx_aaaaaaaaaaaaaaaaaaaa/retire", method: "POST" },
    ]);
  });

  it("keeps online service revision publication on portal-scoped fixed routes", async () => {
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

  it("uses the same portal route for Intent drafts and explicit plan refresh", async () => {
    const calls: Array<{ path: string; method?: string; body?: string }> = [];
    const client = new PlatformAdminClient(async (path, init) => {
      calls.push({ path, method: init?.method, body: init?.body });
      return { ok: true, status: 200, json: async () => path === "/v1/csrf" ? { token: "safe" } : { id: 4, status: "Draft" } };
    }, "operations", "deployment");
    const intent = { version: 1 as const, revision: 1, id: "agents", target: { kernel: "backend" as const }, metadata: { name: "agents" }, services: [] };
    await client.createIntentDraft(intent);
    await client.refreshIntentDraft(4);
    expect(calls).toEqual([
      { path: "/v1/csrf", method: "GET", body: undefined },
      { path: "/v1/portals/operations/platform/services/deployment/deployment/service-revisions", method: "POST", body: JSON.stringify({ intent }) },
      { path: "/v1/portals/operations/platform/services/deployment/deployment/service-revisions/4/refresh-plan", method: "POST", body: "{}" },
    ]);
  });

  it("keeps plugin installation preview and lifecycle on fixed controller BFF routes", async () => {
    const calls: Array<{ path: string; method?: string; body?: string }> = [];
    const client = new PlatformAdminClient(async (path, init) => {
      calls.push({ path, method: init?.method, body: init?.body });
      return { ok: true, status: 200, json: async () => path === "/v1/csrf" ? { token: "safe" } : {} };
    }, "operations", "deployment");
    const request = { version: 1 as const, target: { kernel: "backend" as const, deployment: "agents", unitId: "api" }, portalTargets: ["operations"], change: { action: "upgrade" as const, pluginId: "cn.example.agent", requirement: { pluginId: "cn.example.agent", constraint: "^2.0.0", channel: "stable" } }, expectedActiveRevision: 4 };
    await client.listPluginInstallationTargets();
    await client.previewPluginInstallation(request);
    await client.createPluginInstallationCandidate(request);
    const id = "installation-0123456789abcdef0123456789abcdef";
    await client.submitPluginInstallationCandidate(id);
    expect(calls.filter((call) => call.path !== "/v1/csrf")).toEqual([
      { path: "/v1/portals/operations/platform/services/deployment/deployment/plugin-installations/targets", method: "GET", body: undefined },
      { path: "/v1/portals/operations/platform/services/deployment/deployment/plugin-installations/preview", method: "POST", body: JSON.stringify(request) },
      { path: "/v1/portals/operations/platform/services/deployment/deployment/plugin-installations", method: "POST", body: JSON.stringify(request) },
      { path: `/v1/portals/operations/platform/services/deployment/deployment/plugin-installations/${id}/submit`, method: "POST", body: "{}" },
    ]);
    expect(() => client.getPluginInstallationCandidate("bad/id")).toThrowError(PlatformAdminError);
  });

  it("submits and activates plugin configuration on fixed candidate routes", async () => {
    const calls: Array<{ path: string; method?: string; body?: string }> = [];
    const client = new PlatformAdminClient(async (path, init) => {
      calls.push({ path, method: init?.method, body: init?.body });
      return { ok: true, status: 200, json: async () => path === "/v1/csrf" ? { token: "safe" } : { id: "pcfg_" + "a".repeat(32), status: "Publishing" } };
    }, "operations", "configuration");
    const id = "pcfg_" + "a".repeat(32);
    await client.submitPluginConfigurationDraft(id, 4);
    await client.activatePluginConfigurationCandidate(id, 5);
    await client.submitPlatformProfileConfigurationDraft(id, 6);
    await client.approvePlatformProfileConfigurationCandidate(id, 7);
    await client.activatePlatformProfileConfigurationCandidate(id, 8);
    await client.abortPlatformProfileConfigurationCandidate(id, 9);
    await client.submitHotServiceConfigurationDraft(id, 10);
    await client.approveHotServiceConfigurationCandidate(id, 11);
    await client.activateHotServiceConfigurationCandidate(id, 12);
    await client.abortHotServiceConfigurationCandidate(id, 13);
	await client.submitScopedConfigurationDraft(id, 14);
	await client.approveScopedConfigurationCandidate(id, 15);
	await client.activateScopedConfigurationCandidate(id, 16);
	await client.abortScopedConfigurationCandidate(id, 17);
    expect(calls.filter((call) => call.path !== "/v1/csrf")).toEqual([
      { path: `/v1/portals/operations/platform/services/configuration/plugin-configurations/candidates/${id}/submit`, method: "POST", body: '{"expectedRevision":4}' },
      { path: `/v1/portals/operations/platform/services/configuration/plugin-configurations/candidates/${id}/activate`, method: "POST", body: '{"expectedRevision":5}' },
      { path: `/v1/portals/operations/platform/services/configuration/plugin-configurations/candidates/${id}/submit-profile`, method: "POST", body: '{"expectedRevision":6}' },
      { path: `/v1/portals/operations/platform/services/configuration/plugin-configurations/candidates/${id}/approve-profile`, method: "POST", body: '{"expectedRevision":7}' },
      { path: `/v1/portals/operations/platform/services/configuration/plugin-configurations/candidates/${id}/activate-profile`, method: "POST", body: '{"expectedRevision":8}' },
      { path: `/v1/portals/operations/platform/services/configuration/plugin-configurations/candidates/${id}/abort-profile`, method: "POST", body: '{"expectedRevision":9}' },
      { path: `/v1/portals/operations/platform/services/configuration/plugin-configurations/candidates/${id}/submit-hot`, method: "POST", body: '{"expectedRevision":10}' },
      { path: `/v1/portals/operations/platform/services/configuration/plugin-configurations/candidates/${id}/approve-hot`, method: "POST", body: '{"expectedRevision":11}' },
      { path: `/v1/portals/operations/platform/services/configuration/plugin-configurations/candidates/${id}/activate-hot`, method: "POST", body: '{"expectedRevision":12}' },
      { path: `/v1/portals/operations/platform/services/configuration/plugin-configurations/candidates/${id}/abort-hot`, method: "POST", body: '{"expectedRevision":13}' },
	  { path: `/v1/portals/operations/platform/services/configuration/plugin-configurations/candidates/${id}/submit-scoped`, method: "POST", body: '{"expectedRevision":14}' },
	  { path: `/v1/portals/operations/platform/services/configuration/plugin-configurations/candidates/${id}/approve-scoped`, method: "POST", body: '{"expectedRevision":15}' },
	  { path: `/v1/portals/operations/platform/services/configuration/plugin-configurations/candidates/${id}/activate-scoped`, method: "POST", body: '{"expectedRevision":16}' },
	  { path: `/v1/portals/operations/platform/services/configuration/plugin-configurations/candidates/${id}/abort-scoped`, method: "POST", body: '{"expectedRevision":17}' },
    ]);
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
			receipt: { schemaVersion: 1, repositoryId: "local-testing", protocol: "artifact.repository.local-test.v1", profileDigest: "d".repeat(64), ref: { pluginId: "cn.example.demo", version: "1.1.0-dev.1", channel: "testing" }, sha256: "a".repeat(64), revision: 17 },
    });
    expect(calls[1]).toEqual({
      path: "/v1/portals/operations/platform/services/deployment/deployment/test-releases",
      method: "POST",
			body: expect.stringContaining('"profileDigest":"' + "d".repeat(64) + '"'),
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
    expect(calls[2]).toEqual({
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

  it("preauthorizes assessment reports and requests only the fixed data-plane resource", async () => {
    const calls: Array<{ path: string; method?: string; body?: string }> = [];
    const digest = "a".repeat(64), routeKey = "a234567a234567a23456";
    const client = new PlatformAdminClient(async (path, init) => {
      calls.push({ path, method: init?.method, body: init?.body });
      return { ok: true, status: 200, json: async () => path === "/v1/csrf" ? { token: "safe" } : path.includes("/assessment/reports/")
        ? { sha256: digest, resource: `/v1/assessment-reports/${digest}` }
        : { endpoint: "https://repo.example/", leaseId: "lease", ticket: "b234567890123456789012345678901234567890123", expiresAt: new Date(Date.now() + 30_000).toISOString() } };
    }, "operations", "artifacts");
    await client.prepareArtifactAssessmentReport(digest);
    await client.issueArtifactAssessmentReportTicket(routeKey, digest);
    expect(calls).toEqual([
      { path: `/v1/portals/operations/platform/services/artifacts/artifacts/assessment/reports/${digest}`, method: "GET", body: undefined },
      { path: "/v1/csrf", method: "GET", body: undefined },
      { path: `/api/d/${routeKey}/ticket`, method: "POST", body: JSON.stringify({ method: "GET", resource: `/v1/assessment-reports/${digest}` }) },
    ]);
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

  it("keeps publication approval and evidence on fixed protected routes", async () => {
    const calls: Array<{ path: string; method?: string; body?: string }> = [];
    const client = new PlatformAdminClient(async (path, init) => {
      calls.push({ path, method: init?.method, body: init?.body });
      return { ok: true, status: 200, json: async () => path === "/v1/csrf" ? { token: "safe" } : { revision: 2, items: [] } };
    }, "operations", "artifacts");
    await client.listArtifactPublications();
    await client.submitArtifactPublication({ source: { pluginId: "cn.vastplan.demo", version: "1.0.0", channel: "testing" }, targetChannel: "stable", reason: "ready", expectedRevision: 2 });
    await client.approveArtifactPublication("a".repeat(64), 3);
    await client.rejectArtifactPublication("b".repeat(64), 4, "risk found");
    await client.cancelArtifactPublication("c".repeat(64), 5, "superseded");
    await client.artifactSupplyChainEvidence({ pluginId: "cn.vastplan.demo", version: "1.0.0", channel: "stable" });
    expect(calls.map((call) => call.path)).toEqual([
      "/v1/portals/operations/platform/services/artifacts/artifacts/publications",
      "/v1/csrf", "/v1/portals/operations/platform/services/artifacts/artifacts/publications",
      `/v1/portals/operations/platform/services/artifacts/artifacts/publications/${"a".repeat(64)}/approve`,
      `/v1/portals/operations/platform/services/artifacts/artifacts/publications/${"b".repeat(64)}/reject`,
      `/v1/portals/operations/platform/services/artifacts/artifacts/publications/${"c".repeat(64)}/cancel`,
      "/v1/portals/operations/platform/services/artifacts/artifacts/evidence?pluginId=cn.vastplan.demo&version=1.0.0&channel=stable",
    ]);
  });

  it("uses opaque fixed routes for plugin configuration drafts", async () => {
    const calls: Array<{ path: string; method?: string; body?: string }> = [];
    const client = new PlatformAdminClient(async (path, init) => {
      calls.push({ path, method: init?.method, body: init?.body });
      return { ok: true, status: 200, json: async () => path === "/v1/csrf" ? { token: "safe" } : [] };
    }, "operations", "configuration");
    await client.listPluginConfigurationDefinitions();
	await client.createPluginConfigurationDraft("cfg_aaaaaaaaaaaaaaaaaaaaaaaa", "b".repeat(64), { region: "cn-east" }, { token: "one-use-secret" });
    await client.discardPluginConfigurationDraft("pcfg_cccccccccccccccccccccccccccccccc", 1);
    expect(calls).toEqual([
      { path: "/v1/portals/operations/platform/services/configuration/plugin-configurations", method: "GET", body: undefined },
      { path: "/v1/csrf", method: "GET", body: undefined },
	  { path: "/v1/portals/operations/platform/services/configuration/plugin-configurations/candidates", method: "POST", body: JSON.stringify({ configurationId: "cfg_aaaaaaaaaaaaaaaaaaaaaaaa", catalogDigest: "b".repeat(64), values: { region: "cn-east" }, secrets: { token: "one-use-secret" } }) },
      { path: "/v1/portals/operations/platform/services/configuration/plugin-configurations/candidates/pcfg_cccccccccccccccccccccccccccccccc", method: "DELETE", body: "{\"expectedRevision\":1}" },
    ]);
    expect(calls.some((call) => call.path.includes("com.example"))).toBe(false);
  });

  it("uses fixed resource-profile routes without exposing plugin ids", async () => {
    const calls: Array<{ path: string; method?: string; body?: string }> = [];
    const client = new PlatformAdminClient(async (path, init) => {
      calls.push({ path, method: init?.method, body: init?.body });
      return { ok: true, status: 200, json: async () => path === "/v1/csrf" ? { token: "safe" } : { items: [] } };
    }, "operations", "configuration");
    const configurationId = "cfg_aaaaaaaaaaaaaaaaaaaaaaaa";
    const collectionId = "cfgc_bbbbbbbbbbbbbbbbbbbbbbbb";
    const resourceId = "cfgp_cccccccccccccccccccccccc";
    const digest = "d".repeat(64);
    await client.listPluginConfigurationResources(configurationId, collectionId, digest, undefined, 20);
    await client.createPluginConfigurationResourceDraft(configurationId, collectionId, digest, { endpoint: "https://notify.example.test" }, { authorization: "secret" });
    await client.activatePluginConfigurationResourceCandidate("pcfg_" + "e".repeat(32), 7);
    expect(calls).toEqual([
      { path: `/v1/portals/operations/platform/services/configuration/plugin-configurations/resources?configurationId=${configurationId}&resourceCollectionId=${collectionId}&catalogDigest=${digest}&limit=20`, method: "GET", body: undefined },
      { path: "/v1/csrf", method: "GET", body: undefined },
      { path: "/v1/portals/operations/platform/services/configuration/plugin-configurations/resources/candidates/create", method: "POST", body: JSON.stringify({ configurationId, resourceCollectionId: collectionId, catalogDigest: digest, values: { endpoint: "https://notify.example.test" }, secrets: { authorization: "secret" } }) },
      { path: `/v1/portals/operations/platform/services/configuration/plugin-configurations/candidates/pcfg_${"e".repeat(32)}/activate-resource`, method: "POST", body: '{"expectedRevision":7}' },
    ]);
    expect(calls.some((call) => call.path.includes("authentication.delivery.webhook"))).toBe(false);
    expect(resourceId).toMatch(/^cfgp_/);
  });
});

describe("ManagementAPIClient", () => {
  it("uses only portal-local selectors and a contract-relative path", async () => {
    const calls: Array<{ path: string; method?: string; body?: string }> = [];
    const client = new ManagementAPIClient(async (path, init) => {
      calls.push({ path, method: init?.method, body: init?.body });
      return { ok: true, status: 200, json: async () => path === "/v1/csrf" ? { token: "safe" } : { id: 7 } };
    }, "operations", "api-exposure", "primary");
    await client.get("/api-exposures?status=Draft");
    await client.mutate("/api-exposures", "POST", { name: "customer-api" });
    expect(calls).toEqual([
      { path: "/v1/portals/operations/platform/services/api-exposure/api/primary/api-exposures?status=Draft", method: "GET", body: undefined },
      { path: "/v1/csrf", method: "GET", body: undefined },
      { path: "/v1/portals/operations/platform/services/api-exposure/api/primary/api-exposures", method: "POST", body: JSON.stringify({ name: "customer-api" }) },
    ]);
    expect(calls.some(({ path }) => path.includes("capability=") || path.includes("operation="))).toBe(false);
  });

  it("shares the same idle CSRF policy with the generic Management API client", async () => {
    const calls: string[] = [];
    const client = new ManagementAPIClient(async (path) => {
      calls.push(path);
      return { ok: true, status: 200, json: async () => path === "/v1/csrf" ? { token: "safe" } : { id: 7 } };
    }, "operations", "api-exposure", "primary");

    await client.mutate("/api-exposures", "POST", { name: "one" });
    await client.mutate("/api-exposures", "POST", { name: "two" });
    expect(calls.filter((path) => path === "/v1/csrf")).toHaveLength(1);
  });

  it("rejects ambiguous selectors and relative-path traversal", async () => {
    const fetcher: PlatformFetch = async () => ({ ok: true, status: 200, json: async () => ({}) });
    expect(() => new ManagementAPIClient(fetcher, "operations", "bad/service", "primary")).toThrowError(PlatformAdminError);
    const client = new ManagementAPIClient(fetcher, "operations", "api-exposure", "primary");
    expect(() => client.get("/../credentials")).toThrowError(PlatformAdminError);
  });
});
