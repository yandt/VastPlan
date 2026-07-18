import { describe, expect, it } from "vitest";
import { PlatformAdminClient, PlatformAdminError, type PlatformFetch } from "./index";

describe("PlatformAdminClient", () => {
  it("obtains CSRF before a write and never places credential plaintext in the URL", async () => {
    const calls: Array<{ path: string; body?: string }> = [];
    const fetcher: PlatformFetch = async (path, init) => {
      calls.push({ path, body: init?.body });
      return { ok: true, status: 200, json: async () => path === "/v1/csrf" ? { token: "safe" } : { name: "vault.db", version: 1 } };
    };
    await new PlatformAdminClient(fetcher).putCredential("vault.db", "top-secret");
    expect(calls).toHaveLength(2);
    expect(calls[1].path).toBe("/v1/platform/credentials/vault.db");
    expect(calls[1].path).not.toContain("top-secret");
    expect(calls[1].body).toContain("top-secret");
  });

  it("rejects ambiguous path names locally", async () => {
    const client = new PlatformAdminClient(async () => ({ ok: true, status: 200, json: async () => ({}) }));
    expect(() => client.deleteSetting("bad/name")).toThrowError(PlatformAdminError);
  });

  it("uses fixed deployment routes and obtains CSRF before bootstrap approval", async () => {
    const calls: Array<{ path: string; method?: string }> = [];
    const fetcher: PlatformFetch = async (path, init) => {
      calls.push({ path, method: init?.method });
      return { ok: true, status: 200, json: async () => path === "/v1/csrf" ? { token: "safe" } : { id: "bootstrap-1", state: "SystemdActive" } };
    };
    const client = new PlatformAdminClient(fetcher);
    await client.approveBootstrapJob("bootstrap-1");
    expect(calls).toEqual([
      { path: "/v1/csrf", method: "GET" },
      { path: "/v1/platform/deployment/bootstrap-jobs/bootstrap-1/approve", method: "POST" },
    ]);
  });
});
