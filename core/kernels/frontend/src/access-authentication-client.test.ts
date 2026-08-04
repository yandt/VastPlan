import { describe, expect, it } from "vitest";
import { AccessAuthenticationClient, AccessAuthenticationError, accessBrandAssetURL, accessLocaleDirection, accessReturnTo, localizeAccessText, providerTestSelection } from "./access-authentication-client";

describe("AccessAuthenticationClient", () => {
  it("loads only host-governed access and method descriptions", async () => {
    const calls: string[] = [];
    const fetcher = async (input: string) => {
      calls.push(input);
      if (input.startsWith("/auth/v1/bootstrap")) return new Response(JSON.stringify({ schemaVersion: "v1", localization: { defaultLocale: "zh-CN", supportedLocales: ["zh-CN"] }, authentication: { allowedMethods: ["password"], defaultMethod: "password", reuseIdentifier: true }, branding: { productName: { "zh-CN": "VastPlan" } } }));
      return new Response(JSON.stringify({ methods: [{ methodId: "password", interaction: "form", displayName: { "zh-CN": "密码" } }], defaultMethod: "password" }));
    };
    const value = await new AccessAuthenticationClient(fetcher, "/operations").bootstrap();
    expect(value.defaultMethod).toBe("password");
    expect(calls).toEqual(["/auth/v1/bootstrap?returnTo=%2Foperations", "/auth/v1/methods?returnTo=%2Foperations"]);
  });

  it("obtains pre-auth CSRF before starting a transaction", async () => {
    const calls: Array<{ input: string; init?: RequestInit }> = [];
    const fetcher = async (input: string, init?: RequestInit) => {
      calls.push({ input, init });
      if (input === "/auth/v1/csrf") return new Response(JSON.stringify({ token: "safe" }));
      return new Response(JSON.stringify({ transactionId: "t".repeat(32), result: { state: "challenge" } }), { status: 201 });
    };
    await new AccessAuthenticationClient(fetcher, "/").begin("password", "zh-CN");
    expect(calls[1].init?.headers).toMatchObject({ "X-VastPlan-CSRF": "safe" });
  });

  it("renews a rejected CSRF token and retries the untouched mutation once", async () => {
    const calls: Array<{ input: string; token?: string }> = [];
    let csrf = 0, mutation = 0;
    const fetcher = async (input: string, init?: RequestInit) => {
      calls.push({ input, token: new Headers(init?.headers).get("X-VastPlan-CSRF") ?? undefined });
      if (input === "/auth/v1/csrf") return new Response(JSON.stringify({ token: csrf++ === 0 ? "stale" : "renewed" }));
      if (mutation++ === 0) return new Response(JSON.stringify({ error: "csrf_rejected" }), { status: 403 });
      return new Response(JSON.stringify({ transactionId: "t".repeat(32), result: { state: "challenge" } }), { status: 201 });
    };

    await new AccessAuthenticationClient(fetcher, "/").begin("password", "zh-CN");

    expect(calls).toEqual([
      { input: "/auth/v1/csrf", token: undefined },
      { input: "/auth/v1/transactions", token: "stale" },
      { input: "/auth/v1/csrf", token: undefined },
      { input: "/auth/v1/transactions", token: "renewed" },
    ]);
  });

  it("does not loop when a renewed CSRF token is also rejected", async () => {
    let calls = 0;
    const fetcher = async (input: string) => {
      calls++;
      return input === "/auth/v1/csrf"
        ? new Response(JSON.stringify({ token: `token-${calls}` }))
        : new Response(JSON.stringify({ error: "csrf_rejected" }), { status: 403 });
    };

    await expect(new AccessAuthenticationClient(fetcher, "/").begin("password", "zh-CN"))
      .rejects.toEqual(expect.objectContaining<Partial<AccessAuthenticationError>>({ code: "csrf_rejected", status: 403 }));
    expect(calls).toBe(4);
  });

  it("preserves an expired transaction code for the login lifecycle", async () => {
    const fetcher = async (input: string) => input === "/auth/v1/csrf"
      ? new Response(JSON.stringify({ token: "safe" }))
      : new Response(JSON.stringify({ error: "authentication_transaction_rejected" }), { status: 401 });

    await expect(new AccessAuthenticationClient(fetcher, "/").continue("t".repeat(32), "s".repeat(32), []))
      .rejects.toEqual(expect.objectContaining<Partial<AccessAuthenticationError>>({ code: "authentication_transaction_rejected", status: 401 }));
  });

  it("rejects cross-origin and malformed returnTo values", () => {
    expect(accessReturnTo({ search: "?returnTo=%2Foperations" } as Location)).toBe("/operations");
    expect(accessReturnTo({ search: "?returnTo=https%3A%2F%2Fevil.example" } as Location)).toBe("/");
    expect(accessReturnTo({ search: "?returnTo=%2F%2Fevil.example" } as Location)).toBe("/");
    expect(providerTestSelection({ search: "?providerTest=corporate-oidc&method=sso" } as Location)).toEqual({ providerProfileId: "corporate-oidc", methodId: "sso" });
		expect(accessBrandAssetURL({ schemaVersion:"v1", generationId:"a".repeat(64), accessTemplate:"access", localization:{defaultLocale:"zh-CN",supportedLocales:["zh-CN"]}, authentication:{allowedMethods:["password"],defaultMethod:"password",reuseIdentifier:true}, branding:{productName:{"zh-CN":"VastPlan"},logoAssetId:"vastplan.svg"} }, "/operations")).toBe(`/auth/v1/assets/${"a".repeat(64)}/vastplan.svg?returnTo=%2Foperations`);
		expect(accessLocaleDirection("ar-SA")).toBe("rtl");
		expect(accessLocaleDirection("en-US")).toBe("ltr");
		expect(localizeAccessText({"zh-CN":"中文","en-US":"English"},"ar-SA","fallback")).toBe("English");
  });
});
