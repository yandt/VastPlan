import { describe, expect, it } from "vitest";
import { translate } from "@vastplan/ui-primitives";
import type { ContributionIndexSnapshot } from "@vastplan/plugin-inventory-contract";
import { navigationCatalogsFromIndex } from "./navigation-contributions";

const digest = "a".repeat(64);
const owner = {
  ref: { pluginId: "cn.vastplan.test.navigation", version: "1.0.0", channel: "stable" },
  sha256: digest,
  publisher: "vastplan",
};

function index(descriptor: Record<string, unknown>): ContributionIndexSnapshot {
  return {
    schemaVersion: 1,
    generation: 1,
    inventoryDigest: digest,
    digest,
    contributions: [{ kind: "frontend.navigations", surface: "frontend", id: "main", contract: "1.0.0", owner, descriptor }],
  };
}

function descriptor(overrides: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    id: "main",
    contract: "1.0.0",
    nodes: [{
      id: "resources",
      zone: "primary",
      label: { key: "navigation.resources", fallback: "Resources" },
      icon: { kind: "semantic", name: "settings" },
      order: 10,
    }],
    icons: [],
    ...overrides,
  };
}

describe("signed navigation contributions", () => {
  it("binds localization namespace to the signed owner", () => {
    const catalog = navigationCatalogsFromIndex(index(descriptor()))[0];
    expect(catalog.nodes[0]).toMatchObject({ id: "cn.vastplan.test.navigation/resources", ref: { pluginID: owner.ref.pluginId, nodeID: "resources" }, zone: "primary" });
    expect(translate(catalog.nodes[0].label, "en-US", { [owner.ref.pluginId]: { defaultLocale: "zh-CN", messages: { "zh-CN": { "navigation.resources": "资源与配置" }, "en-US": { "navigation.resources": "Resources and configuration" } } } }, "zh-CN")).toBe("Resources and configuration");
  });

  it("rejects executable or external SVG content", () => {
    const malicious = descriptor({
      icons: [{ id: "unsafe", motion: "none", states: { normal: { viewBox: "0 0 24 24", nodes: [{ tag: "script", d: "alert(1)", tone: "primary" }] } } }],
      nodes: [{ id: "resources", zone: "primary", label: { key: "navigation.resources", fallback: "Resources" }, icon: { kind: "custom", name: "unsafe" } }],
    });
    expect(() => navigationCatalogsFromIndex(index(malicious))).toThrow(/只允许安全 path\/group/);
  });

  it("uses an owned fallback for a missing optional cross-plugin parent", () => {
    const catalogs = navigationCatalogsFromIndex(index(descriptor({ nodes: [
      { id: "fallback", zone: "primary", label: { key: "navigation.fallback", fallback: "Fallback" }, icon: { kind: "semantic", name: "menu" } },
      { id: "resources", zone: "primary", label: { key: "navigation.resources", fallback: "Resources" }, icon: { kind: "semantic", name: "settings" }, parent: { pluginId: "cn.vastplan.optional", nodeId: "root", mode: "optional", fallbackNodeId: "fallback" } },
    ] })));
    expect(catalogs[0].nodes).toHaveLength(2);
  });

  it("rejects required unknown parents, cross-zone links and overly deep trees", () => {
    expect(() => navigationCatalogsFromIndex(index(descriptor({ nodes: [{ id: "child", zone: "primary", label: { key: "navigation.child", fallback: "Child" }, icon: { kind: "semantic", name: "menu" }, parent: { pluginId: "cn.vastplan.missing", nodeId: "root", mode: "required" } }] })))).toThrow(/未知 required 父级/);
    expect(() => navigationCatalogsFromIndex(index(descriptor({ nodes: [
      { id: "root", zone: "primary", label: { key: "navigation.root", fallback: "Root" }, icon: { kind: "semantic", name: "menu" } },
      { id: "child", zone: "settings", label: { key: "navigation.child", fallback: "Child" }, icon: { kind: "semantic", name: "menu" }, parent: { nodeId: "root", mode: "required" } },
    ] })))).toThrow(/不能跨 zone/);
    expect(() => navigationCatalogsFromIndex(index(descriptor({ nodes: [
      { id: "root", zone: "primary", label: { key: "navigation.root", fallback: "Root" }, icon: { kind: "semantic", name: "menu" } },
      { id: "child", zone: "primary", label: { key: "navigation.child", fallback: "Child" }, icon: { kind: "semantic", name: "menu" }, parent: { nodeId: "root", mode: "required" } },
      { id: "deep", zone: "primary", label: { key: "navigation.deep", fallback: "Deep" }, icon: { kind: "semantic", name: "menu" }, parent: { nodeId: "child", mode: "required" } },
    ] })))).toThrow(/深度超过/);
  });

  it("allows an enabled signed plugin to contribute a menu below the account avatar anchor", () => {
    const accountNode = descriptor({ nodes: [{ id: "profile", zone: "secondary", label: { key: "navigation.profile", fallback: "Profile" }, icon: { kind: "semantic", name: "info" }, parent: { pluginId: "vastplan.host", nodeId: "account", mode: "required" } }] });
    expect(navigationCatalogsFromIndex(index(accountNode))[0]?.nodes[0]?.parent).toMatchObject({ pluginID: "vastplan.host", nodeID: "account", mode: "required" });
  });

  it("parses and translates a 500-node Portal catalog within the bounded budget", () => {
    const contributions: Array<ContributionIndexSnapshot["contributions"][number]> = [];
    for (let catalogIndex = 0; catalogIndex < 8; catalogIndex += 1) {
      const pluginID = `cn.vastplan.test.navigation-${catalogIndex}`;
      const nodeCount = catalogIndex < 4 ? 63 : 62;
      contributions.push({
        kind: "frontend.navigations",
        surface: "frontend",
        id: "main",
        contract: "1.0.0",
        owner: { ...owner, ref: { ...owner.ref, pluginId: pluginID } },
        descriptor: descriptor({
          nodes: Array.from({ length: nodeCount }, (_, nodeIndex) => ({
            id: `node-${nodeIndex}`,
            zone: "primary",
            label: { key: `navigation.node-${nodeIndex}`, fallback: `Node ${nodeIndex}` },
            icon: { kind: "semantic", name: "menu" },
            order: nodeIndex,
          })),
        }),
      });
    }
    const snapshot: ContributionIndexSnapshot = { schemaVersion: 1, generation: 1, inventoryDigest: digest, digest, contributions };
    const startedAt = performance.now();
    const catalogs = navigationCatalogsFromIndex(snapshot);
    const labels = catalogs.flatMap((catalog) => catalog.nodes.map((node) => translate(node.label, "en-US", {}, "zh-CN")));
    const elapsed = performance.now() - startedAt;

    expect(labels).toHaveLength(500);
    expect(labels[0]).toBe("Node 0");
    expect(elapsed).toBeLessThan(1_000);
  });
});
