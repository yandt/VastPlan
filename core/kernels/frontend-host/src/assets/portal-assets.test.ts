import { symlink } from "node:fs/promises";
import { join } from "node:path";
import { describe, expect, it } from "vitest";
import { PortalAssets, portalNoncePlaceholder } from "./portal-assets";
import { createPortalFixture } from "../testing/portal-fixture";

describe("PortalAssets", () => {
  it("loads a bounded immutable asset snapshot and rotates CSP nonces", async () => {
    const root = await createPortalFixture();
    const assets = await PortalAssets.load(root);
    expect(assets.get("app.js")?.contentType).toBe("text/javascript; charset=utf-8");
    expect(assets.get("app.js")?.etag).toMatch(/^"sha256-[a-f0-9]{64}"$/);
    const first = assets.renderIndex();
    const second = assets.renderIndex();
    expect(first.nonce).not.toBe(second.nonce);
    expect(first.body.toString()).toContain(first.nonce);
    expect(first.body.toString()).not.toContain(portalNoncePlaceholder);
		const rendered = assets.renderIndex('<main aria-busy="true">VastPlan</main>').body.toString();
		expect(rendered).toContain('<template shadowrootmode="open"><div id="vastplan-portal-root"><main aria-busy="true">VastPlan</main>');
  });

  it("rejects symbolic links at every static trust boundary", async () => {
    const root = await createPortalFixture();
    await symlink(join(root, "assets", "app.js"), join(root, "assets", "linked.js"));
    await expect(PortalAssets.load(root)).rejects.toThrow(/符号链接/);
  });
});
