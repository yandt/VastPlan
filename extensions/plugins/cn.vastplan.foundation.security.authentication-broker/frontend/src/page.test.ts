import { afterEach, describe, expect, it, vi } from "vitest";
import type {
  AuthenticationProviderManagementState,
  PlatformAdminClient,
} from "@vastplan/platform-admin";
import { message } from "@vastplan/workbench-sdk";
import { createAuthenticationProviderPage } from "./page.js";

const state: AuthenticationProviderManagementState = {
  version: 1,
  generation: 4,
  updatedAt: "2026-07-23T00:00:00Z",
  providers: [
    {
      profile: {
        version: 1,
        revision: 1,
        id: "corporate-oidc",
        contributionId: "enterprise-oidc",
        configuration: {
          id: "oidc-config",
          revision: 1,
          digest: "a".repeat(64),
        },
        purposes: ["portal-login"],
        methods: ["oidc"],
        subjectNamespace: "enterprise.identity.oidc",
        requiredCapabilities: [],
      },
      lifecycle: {
        schemaVersion: "v1",
        profile: { id: "corporate-oidc", revision: 1, digest: "b".repeat(64) },
        state: "validated",
        readiness: "unknown",
        unmetCapabilities: [],
        updatedAt: "2026-07-23T00:00:00Z",
      },
    },
  ],
};

afterEach(() => vi.unstubAllGlobals());

describe("Authentication Provider Workbench", () => {
  it("uses the collection framework and preserves CAS generation", async () => {
    const validate = vi.fn(async () => state);
    const client = {
      authenticationProviderState: vi.fn(async () => state),
      validateAuthenticationProvider: validate,
    } as unknown as PlatformAdminClient;
    const page = createAuthenticationProviderPage(
      client,
      "security",
      "/settings/authentication-providers",
      message("test", "title", "Providers"),
    );
    const loaded = await page.load(
      { mode: "page", page: 1, pageSize: 20, filters: {} },
      new AbortController().signal,
    );
    expect(loaded).toMatchObject({
      total: 1,
      items: [{ id: "corporate-oidc", generation: 4 }],
    });
    await page.runAction?.(
      {
        action: page.collection.actions!.find(
          (item) => item.id === "validate",
        )!,
        selected: loaded.items,
        refresh() {},
      },
      new AbortController().signal,
    );
    expect(validate).toHaveBeenCalledWith("corporate-oidc", 4);
    expect(
      page.collection.actions?.map((item) => item.form).filter(Boolean),
    ).toEqual(["create", "publish"]);
  });

  it("never accepts a browser-pasted Assertion as an authentication test", () => {
    const testProvider = vi.fn();
    const client = {
      testAuthenticationProvider: testProvider,
    } as unknown as PlatformAdminClient;
    const page = createAuthenticationProviderPage(
      client,
      "security",
      "/settings/authentication-providers",
      message("test", "title", "Providers"),
    );
    expect(page.forms?.some((item) => item.id === "test")).toBe(false);
    expect(page.collection.actions?.find((item) => item.id === "test")?.form).toBeUndefined();
    expect(testProvider).not.toHaveBeenCalled();
  });

  it("records a server-sealed Provider test receipt on return", async () => {
    const testProvider = vi.fn(async () => state);
    vi.stubGlobal("location", { href: "https://portal.example/settings/authentication-providers?providerTestReceipt=corporate-oidc" });
    vi.stubGlobal("history", { replaceState: vi.fn() });
    const client = { authenticationProviderState: vi.fn(async () => state), testAuthenticationProvider: testProvider } as unknown as PlatformAdminClient;
    const page = createAuthenticationProviderPage(client, "security", "/settings/authentication-providers", message("test", "title", "Providers"));
    await page.load({ mode: "page", page: 1, pageSize: 20, filters: {} }, new AbortController().signal);
    expect(testProvider).toHaveBeenCalledWith("corporate-oidc", 4);
    expect(globalThis.history.replaceState).toHaveBeenCalled();
  });
});
