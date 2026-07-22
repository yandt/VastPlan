import { describe, expect, it, vi } from "vitest";
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
    ).toEqual(["create", "publish", "test"]);
  });

  it("does not turn a checkbox into a successful authentication test", async () => {
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
    const form = page.forms?.find((item) => item.id === "test")!;
    const result = await form.submit(
      {
        value: { assertion: "not-json" },
        selected: [
          {
            ...state.providers[0],
            id: "corporate-oidc",
            generation: 4,
          } as never,
        ],
      },
      new AbortController().signal,
    );
    expect(result).toHaveProperty("fieldErrors.assertion");
    expect(testProvider).not.toHaveBeenCalled();
  });
});
