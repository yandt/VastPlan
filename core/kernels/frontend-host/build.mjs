import { build } from "esbuild";

await build({
  entryPoints: {
    "portal-host": "src/main.ts",
    "server-generation-worker": "src/workers/server-generation-worker.ts",
  },
  bundle: true,
  platform: "node",
  format: "cjs",
  target: "node22",
  legalComments: "none",
  outdir: "dist",
  outExtension: { ".js": ".cjs" },
});
