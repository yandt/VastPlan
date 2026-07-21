import { build } from "esbuild";
import { mkdir, writeFile } from "node:fs/promises";
import { basename, dirname, extname, resolve } from "node:path";
import { createFrontendModuleGraph } from "./frontend-module-graph.mjs";

export const serverAllowedExternals = new Set(["stream", "util", "node:stream", "node:util"]);

export async function buildFrontendServerGraph({ buildRoot, serverEntry, serverSource }) {
  const serverOutfile = resolve(buildRoot, serverEntry);
  const serverOutdir = dirname(serverOutfile);
  await mkdir(serverOutdir, { recursive: true });
  const serverEntryName = basename(serverEntry, extname(serverEntry));
  const result = await build({
    bundle: true,
    format: "esm",
    platform: "node",
    target: "node22",
    legalComments: "none",
    minify: true,
    banner: { js: 'import { createRequire as __vastplanCreateRequire } from "node:module"; const require = __vastplanCreateRequire(import.meta.url);' },
    entryPoints: { [serverEntryName]: serverSource },
    outdir: serverOutdir,
    entryNames: serverEntryName,
    chunkNames: "server-chunks/[name]-[hash]",
    splitting: true,
    metafile: true,
    outExtension: { ".js": extname(serverEntry) },
  });
  const graph = await createFrontendModuleGraph({ target: "server", pluginRoot: buildRoot, entry: serverEntry, metafile: result.metafile, allowedExternals: serverAllowedExternals });
  const graphFile = resolve(serverOutdir, "vastplan.server-graph.json");
  await writeFile(graphFile, `${JSON.stringify(graph, null, 2)}\n`);
  return { graph, graphFile };
}
