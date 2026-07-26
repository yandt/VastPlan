import { build } from "esbuild";
import { join, resolve } from "node:path";
import { describe, expect, it } from "vitest";
import { PortalDeliveryStore } from "../runtime/portal-delivery-store";
import { createPortalDeliveryFixture, writePortalDeliveryRevision } from "../testing/portal-delivery-fixture";
import { ServerGenerationManager } from "./server-generation-manager";

describe("ServerGenerationManager", () => {
  it("keeps a healthy sealed candidate invisible until an explicit commit", async () => {
    const fixture = await createPortalDeliveryFixture();
    const spec = {
      revision: 12, id: "operations", tenantId: "tenant-a", route: "/operations",
      runtimeEngine: { id: "cn.vastplan.foundation.frontend.runtime.engine.react", version: "1.1.1" },
    };
    await writePortalDeliveryRevision(fixture, spec, undefined,
      "export default { id: 'ui.runtime.engine.server', render(input) { return { html: `<main>${input.portalId}:${input.path}</main>` }; } };\n");
    const workerScript = join(fixture.cache, "server-generation-worker.cjs");
    await build({
      entryPoints: [resolve("src/workers/server-generation-worker.ts")], bundle: true, platform: "node", format: "cjs", target: "node22", outfile: workerScript,
    });
    const delivery = await PortalDeliveryStore.open(fixture.cache, fixture.origin);
    const manager = new ServerGenerationManager(delivery, join(fixture.cache, "generations"), workerScript);
    try {
      const input = {
        generation: 12, tenantId: "tenant-a", portalId: "operations", path: "/operations/settings", locale: "zh-CN", branding: {},
      };
      expect(await manager.renderActive("tenant-a", spec, input)).toBeUndefined();
      const prepared = await manager.prepare("tenant-a", spec, input);
      expect(prepared).toBeDefined();
      expect(await manager.renderActive("tenant-a", spec, input)).toBeUndefined();
      manager.commit(prepared!);
      const rendered = await manager.renderActive("tenant-a", spec, input);
      expect(rendered?.html).toBe("<main>operations:/operations/settings</main>");
    } finally {
      await manager.shutdown();
    }
  });
});
