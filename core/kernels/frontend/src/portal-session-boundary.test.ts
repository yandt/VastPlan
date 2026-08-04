import { describe, expect, it, vi } from "vitest";
import { createPortalSessionFetch } from "./portal-session-boundary";

describe("Portal session fetch boundary", () => {
  it("redirects an expired session to login without consuming the original response", async () => {
    const assign = vi.fn();
    const fetcher = createPortalSessionFetch(async () => new Response(JSON.stringify({ error: "session_required" }), {
      status: 401, headers: { "Content-Type": "application/json" },
    }), { pathname: "/operations/settings/databases", search: "?tab=pool", hash: "#primary", assign } as unknown as Location);

    const response = await fetcher("/v1/csrf");

    expect(assign).toHaveBeenCalledWith("/auth/access?returnTo=%2Foperations%2Fsettings%2Fdatabases%3Ftab%3Dpool%23primary");
    expect(await response.json()).toEqual({ error: "session_required" });
  });

  it("does not redirect unrelated 401 responses or recurse from the login page", async () => {
    const assign = vi.fn();
    const unauthorized = async () => new Response(JSON.stringify({ error: "invalid_credentials" }), { status: 401 });
    await createPortalSessionFetch(unauthorized, { pathname: "/operations", search: "", hash: "", assign } as unknown as Location)("/v1/test");
    const expired = async () => new Response(JSON.stringify({ error: "session_required" }), { status: 401 });
    await createPortalSessionFetch(expired, { pathname: "/auth/access", search: "?returnTo=%2Foperations", hash: "", assign } as unknown as Location)("/auth/v1/test");
    expect(assign).not.toHaveBeenCalled();
  });
});
