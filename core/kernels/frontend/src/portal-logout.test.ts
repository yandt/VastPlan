import { describe, expect, it } from "vitest";
import { logoutPortalSession, portalLogoutRedirect } from "./portal-logout";

describe("Portal logout", () => {
  it("obtains CSRF before clearing the trusted session", async () => {
    const calls: Array<{ input: string; init?: RequestInit }> = [];
    const fetcher = async (input: string, init?: RequestInit) => {
      calls.push({ input, init });
      return input === "/auth/v1/csrf"
        ? new Response(JSON.stringify({ token: "a".repeat(32) }), { status: 200 })
        : new Response(null, { status: 204 });
    };
    await logoutPortalSession(fetcher);
    expect(calls).toEqual([
      { input: "/auth/v1/csrf", init: { credentials: "same-origin", cache: "no-store" } },
      { input: "/auth/logout", init: { method: "POST", credentials: "same-origin", cache: "no-store", headers: { "X-VastPlan-CSRF": "a".repeat(32) } } },
    ]);
  });

  it("never emits an external post-logout return target", () => {
    expect(portalLogoutRedirect("/operations/settings")).toBe("/auth/login?returnTo=%2Foperations%2Fsettings");
    expect(portalLogoutRedirect("//external.example")).toBe("/auth/login?returnTo=%2F");
  });
});
