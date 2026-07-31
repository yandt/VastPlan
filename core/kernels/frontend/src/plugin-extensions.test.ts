import { describe, expect, it } from "vitest";
import type { PortalRegisteredPage } from "@vastplan/ui-primitives";
import { createPluginExtensionAccess, parsePortalExtensionGraph, validateFrontendPageExtensions } from "./plugin-extensions";

const owner = "cn.vastplan.foundation.frontend.identity.account-center";
const contributor = "cn.vastplan.example.account-security";
const pointID = `${owner}.page`;

const graph = parsePortalExtensionGraph({
  points: [{ id: pointID, ownerPluginId: owner, surface: "frontend", contract: "1.0.0", kind: "frontend.page", dispatch: "mount", targets: ["account", "account.settings"] }],
  contributions: [{ point: pointID, id: `${contributor}.page`, pluginId: contributor, contract: "^1.0.0", order: 20, descriptor: { pageId: "account.security", groupId: "account.settings" } }],
});

const extensionPage: PortalRegisteredPage = {
  id: "account.security", pluginID: contributor, path: "/account/security", title: "Security",
  navigation: { id: "account.security", label: "Security", zone: "secondary", groupID: "account.settings" },
  slots: [{ id: "body", slot: "page.body.main", component: () => null }],
};

describe("Portal plugin extension graph", () => {
  it("gives owners the complete point while contributors only see their binding", () => {
    expect(createPluginExtensionAccess(graph, owner).list(pointID)).toHaveLength(1);
    expect(createPluginExtensionAccess(graph, contributor).contributes(pointID)).toBe(true);
    expect(createPluginExtensionAccess(graph, "cn.vastplan.example.other").list(pointID)).toHaveLength(0);
  });

  it("accepts a page that matches the signed extension descriptor", () => {
    expect(() => validateFrontendPageExtensions([extensionPage], graph)).not.toThrow();
  });

  it("rejects undeclared or mismatched pages in an owned navigation target", () => {
    const noContributions = parsePortalExtensionGraph({ points: graph.points, contributions: [] });
    expect(() => validateFrontendPageExtensions([{ ...extensionPage, pluginID: "cn.vastplan.example.other" }], noContributions)).toThrow(/未声明扩展关系/);
    expect(() => validateFrontendPageExtensions([{ ...extensionPage, navigation: { ...extensionPage.navigation!, groupID: "account" } }], graph)).toThrow(/未按签名 descriptor/);
  });

  it("rejects duplicate extension identities before assembly", () => {
    expect(() => parsePortalExtensionGraph({ points: [...graph.points, ...graph.points], contributions: [] })).toThrow(/扩展点重复/);
  });
});
