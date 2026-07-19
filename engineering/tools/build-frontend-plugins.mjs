import { build } from "esbuild";
import { spawnSync } from "node:child_process";
import { createHash } from "node:crypto";
import { mkdir, readFile, writeFile } from "node:fs/promises";
import { dirname, resolve } from "node:path";

const outputRoot = option("--out-dir");
const manifestPath = option("--manifest");

const common = {
  bundle: true,
  format: "esm",
  platform: "browser",
  target: "es2022",
  legalComments: "none",
  minify: true,
  define: { "process.env.NODE_ENV": '"production"' },
  external: ["react", "react-dom", "react/jsx-runtime", "@vastplan/ui-primitives", "@vastplan/ui-contract"],
};

const plugins = [
  ["cn.vastplan.foundation.frontend.render.adapter.arco", { loader: { ".css": "text" } }],
  ["cn.vastplan.foundation.frontend.render.adapter.mui", {}],
  ["cn.vastplan.foundation.frontend.structure.composition.standard", {}],
  ["cn.vastplan.foundation.frontend.structure.layout.standard", {}],
  ["cn.vastplan.foundation.frontend.structure.layout.top-navigation", {}],
  ["cn.vastplan.platform.configuration.portal-composer", {}],
  ["cn.vastplan.platform.configuration.global-settings", {}],
  ["cn.vastplan.platform.security.credentials", {}],
  ["cn.vastplan.platform.data.relational.connection-manager", {}],
  ["cn.vastplan.platform.artifacts.repository", {}],
  ["cn.vastplan.platform.infrastructure.deployment-manager", {}],
];

const modules = [];
for (const [id, options] of plugins) {
  const outfile = outputRoot === undefined
    ? `extensions/plugins/${id}/frontend/dist/index.js`
    : resolve(outputRoot, `${id}.js`);
  await mkdir(dirname(outfile), { recursive: true });
  await build({
    ...common,
    ...options,
    entryPoints: [`extensions/plugins/${id}/frontend/src/index.${id === "cn.vastplan.foundation.frontend.structure.composition.standard" ? "ts" : "tsx"}`],
    outfile,
  });
  if (id === "cn.vastplan.foundation.frontend.render.adapter.arco") {
    const result = spawnSync(process.execPath, ["engineering/tools/check-arco-on-demand.mjs"], { stdio: "inherit", env: { ...process.env, ARCO_BUNDLE_FILE: outfile } });
    if (result.status !== 0) process.exit(result.status ?? 1);
  }
  if (outputRoot !== undefined) {
    const bytes = await readFile(outfile);
    modules.push({ id, entry: "frontend/dist/index.js", file: outfile, sha256: createHash("sha256").update(bytes).digest("hex") });
  }
}

if (manifestPath !== undefined) {
  if (outputRoot === undefined) throw new Error("--manifest 必须与 --out-dir 一起使用");
  await mkdir(dirname(resolve(manifestPath)), { recursive: true });
  await writeFile(manifestPath, `${JSON.stringify({ version: 1, modules }, null, 2)}\n`, { mode: 0o600 });
}

function option(name) {
  const index = process.argv.indexOf(name);
  if (index === -1) return undefined;
  const value = process.argv[index + 1];
  if (value === undefined || value.startsWith("--")) throw new Error(`${name} 缺少值`);
  return value;
}
