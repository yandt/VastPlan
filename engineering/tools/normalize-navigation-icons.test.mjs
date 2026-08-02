import test from "node:test";
import assert from "node:assert/strict";
import { mkdtemp, mkdir, readFile, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { resolve } from "node:path";
import { spawnSync } from "node:child_process";

const script = resolve("engineering/tools/normalize-navigation-icons.mjs");

test("normalizes safe SVG sources into a code-free glyph AST", async () => {
  const root = await fixture(`<svg viewBox="0 0 24 24" fill="currentColor"><g transform="translate(1 1)"><path d="M1 1L10 10Z" data-tone="secondary"/></g></svg>`);
  const result = run(root);
  assert.equal(result.status, 0, result.stderr);
  const manifest = JSON.parse(await readFile(resolve(root, "vastplan.plugin.json"), "utf8"));
  const icon = manifest.contributes.frontend.navigations[0].icons[0];
  assert.equal(icon.sources, undefined);
  assert.equal(icon.states.normal.nodes[0].children[0].tone, "secondary");
});

test("rejects executable or externally-referenced SVG", async () => {
  const root = await fixture(`<svg viewBox="0 0 24 24"><script>alert(1)</script><path d="M1 1Z"/></svg>`);
  const result = run(root);
  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /脚本、外链、样式或不受支持图元/);
});

function run(root) {
  return spawnSync(process.execPath, [script, "--plugin-root", root, "--manifest", resolve(root, "vastplan.plugin.json")], { encoding: "utf8" });
}

async function fixture(svg) {
  const root = await mkdtemp(resolve(tmpdir(), "vastplan-navigation-icon-"));
  await mkdir(resolve(root, "frontend/icons/navigation"), { recursive: true });
  await writeFile(resolve(root, "frontend/icons/navigation/menu.svg"), svg);
  await writeFile(resolve(root, "vastplan.plugin.json"), JSON.stringify({
    contributes: { frontend: { navigations: [{ icons: [{ id: "menu", sources: { normal: "frontend/icons/navigation/menu.svg" }, motion: "none" }] }] } },
  }));
  return root;
}
