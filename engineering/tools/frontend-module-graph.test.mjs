import assert from "node:assert/strict";
import { mkdir, mkdtemp, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";
import { computeFrontendModuleGraphDigest, createFrontendModuleGraph } from "./frontend-module-graph.mjs";

const crossLanguageDigest = "609691d052868274387ac73579768bf2377aacff883a9fc61f1aaa60021a027e";

test("creates a closed deterministic graph from esbuild metadata", async () => {
  const root = await mkdtemp(join(tmpdir(), "vastplan-graph-"));
  await mkdir(join(root, "frontend", "dist", "chunks"), { recursive: true });
  const entry = join(root, "frontend", "dist", "main.js");
  const chunk = join(root, "frontend", "dist", "chunks", "lazy.js");
  const entryContent = "import('./chunks/lazy.js'); import React from 'react';\n";
  const chunkContent = "export const lazy = true;\n";
  await writeFile(entry, entryContent);
  await writeFile(chunk, chunkContent);
  const metafile = { outputs: {
    [entry]: { bytes: Buffer.byteLength(entryContent), imports: [{ path: "chunks/lazy.js", kind: "dynamic-import" }, { path: "react", kind: "import-statement", external: true }] },
    [chunk]: { bytes: Buffer.byteLength(chunkContent), imports: [] },
  } };
  const graph = await createFrontendModuleGraph({ target: "browser", pluginRoot: root, entry: "frontend/dist/main.js", metafile, allowedExternals: new Set(["react"]) });
  assert.equal(graph.nodes.length, 2);
  assert.deepEqual(graph.externals, ["react"]);
  assert.equal(graph.nodes.find((node) => node.purpose === "entry").dependencies[0].kind, "dynamic");
  assert.match(graph.digest, /^[a-f0-9]{64}$/);
});

test("rejects undeclared externals and output escape", async () => {
  const root = await mkdtemp(join(tmpdir(), "vastplan-graph-"));
  const entry = join(root, "main.js");
  await writeFile(entry, "import 'evil';\n");
  const metafile = { outputs: { [entry]: { bytes: 15, imports: [{ path: "evil", kind: "import-statement", external: true }] } } };
  await assert.rejects(createFrontendModuleGraph({ target: "browser", pluginRoot: root, entry: "main.js", metafile, allowedExternals: new Set() }), /未允许/);
});

test("uses the same canonical digest as the Go manifest validator", () => {
  const graph = {
    schemaVersion: "v1", target: "browser", entry: "frontend/dist/main.js", externals: ["react"],
    nodes: [
      { path: "frontend/dist/main.js", sha256: "923fe53966c6cd9343e11af776cd4b05be315ea4b200b02e4d5dfb0f929b73bf", size: 5, mediaType: "text/javascript", purpose: "entry", dependencies: [{ specifier: "chunks/lazy.js", path: "frontend/dist/chunks/lazy.js", kind: "dynamic" }] },
      { path: "frontend/dist/chunks/lazy.js", sha256: "6c87f68371b28954707ebb92afee7ccffb74c6f71ec8fea8a98cf6104289585b", size: 5, mediaType: "text/javascript", purpose: "chunk", dependencies: [] },
    ],
  };
  assert.equal(computeFrontendModuleGraphDigest(graph), crossLanguageDigest);
});
