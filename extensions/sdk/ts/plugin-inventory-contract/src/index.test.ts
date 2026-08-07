import { describe, expect, it } from "vitest";
import { contributionsByKind, parseContributionIndex, parsePluginReconciliationPlan } from "./index";

const digest = "a".repeat(64);

describe("plugin inventory contract", () => {
  it("keeps unknown contribution kinds discoverable", () => {
    const index = parseContributionIndex({ schemaVersion: 1, generation: 3, inventoryDigest: digest, digest, contributions: [{ kind: "frontend.futureWidgets", surface: "frontend", id: "future", contract: "^1.0.0", owner: { ref: { pluginId: "cn.vastplan.future", version: "1.0.0", channel: "stable" }, sha256: digest, publisher: "vastplan" }, descriptor: { id: "future", mode: "safe" } }] });
    expect(contributionsByKind(index, "frontend.futureWidgets")).toHaveLength(1);
    expect(Object.isFrozen(index.contributions[0]?.descriptor)).toBe(true);
  });

  it("rejects duplicate kind identities", () => {
    const contribution = { kind: "frontend.futureWidgets", surface: "frontend", id: "future", owner: { ref: { pluginId: "cn.vastplan.future", version: "1.0.0", channel: "stable" }, sha256: digest, publisher: "vastplan" }, descriptor: { id: "future" } };
    expect(() => parseContributionIndex({ schemaVersion: 1, generation: 1, inventoryDigest: digest, digest, contributions: [contribution, contribution] })).toThrow(/重复/);
  });

  it("parses the same target-neutral reconciliation plan for every kernel", () => {
    for (const target of ["backend", "frontend", "desktop", "mobile"] as const) {
      expect(parsePluginReconciliationPlan({ schemaVersion: 1, target, generation: 1, selectionDigest: digest, contributionDigest: digest, digest, actions: [{ pluginId: "cn.vastplan.test", operation: "activate", strategy: `${target}.test` }] }).target).toBe(target);
    }
  });
});
