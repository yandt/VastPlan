import { describe, expect, it, vi } from "vitest";
import { PortalGenerationCommitClient } from "./portal-generation-client";
import type { PortalRuntimeSpec } from "./module-loader";

const transactionID = "a".repeat(64);
const spec = {
  portal: { revision: 7 }, modules: [], moduleGraphs: [],
  coordination: { state: "prepared", activationId: 7, transactionId: transactionID },
} as unknown as PortalRuntimeSpec;

describe("PortalGenerationCommitClient", () => {
  it("uses CSRF and commits each prepared transaction once", async () => {
    const fetcher = vi.fn(async (path: string, _init?: RequestInit) => new Response(JSON.stringify(path === "/v1/csrf"
      ? { token: "safe" } : { state: "committed", activationId: 7 }), { status: 200, headers: { "Content-Type": "application/json" } }));
    const client = new PortalGenerationCommitClient(fetcher);
    await client.commit(spec);
    await client.commit(spec);
    expect(fetcher).toHaveBeenCalledTimes(2);
    expect(fetcher.mock.calls[1]?.[0]).toBe("/v1/portal-generation-commit");
    expect(fetcher.mock.calls[1]?.[1]).toMatchObject({ method: "POST", body: JSON.stringify({ transactionId: transactionID }) });
  });

  it("fails before Browser commit when the server rejects the transaction", async () => {
    const fetcher = vi.fn(async (path: string, _init?: RequestInit) => new Response(JSON.stringify(path === "/v1/csrf" ? { token: "safe" } : { error: "activation_changed" }), {
      status: path === "/v1/csrf" ? 200 : 409, headers: { "Content-Type": "application/json" },
    }));
    await expect(new PortalGenerationCommitClient(fetcher).commit(spec)).rejects.toMatchObject({ code: "activation_changed" });
  });
});
