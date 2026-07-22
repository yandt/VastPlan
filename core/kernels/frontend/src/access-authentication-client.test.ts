import { describe, expect, it } from "vitest";
import { AccessAuthenticationClient, accessBrandAssetURL, accessLocaleDirection, accessReturnTo, localizeAccessText, providerTestSelection } from "./access-authentication-client";

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
