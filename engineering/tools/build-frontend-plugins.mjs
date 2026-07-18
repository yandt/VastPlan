import { build } from "esbuild";
import { spawnSync } from "node:child_process";

const common = {
  bundle: true,
  format: "esm",
  platform: "browser",
  target: "es2022",
  legalComments: "none",
  minify: true,
  define: { "process.env.NODE_ENV": '"production"' },
  external: ["react", "react-dom", "react/jsx-runtime", "@vastplan/portal-ui", "@vastplan/ui-contract"],
};

const plugins = [
  ["com.vastplan.foundation.frontend.design-system.arco", { loader: { ".css": "text" } }],
  ["com.vastplan.foundation.frontend.design-system.mui", {}],
  ["com.vastplan.platform.configuration.portal-composer", {}],
  ["com.vastplan.platform.configuration.global-settings", {}],
  ["com.vastplan.platform.security.credentials", {}],
  ["com.vastplan.platform.data.relational.connection-manager", {}],
  ["com.vastplan.platform.artifacts.repository", {}],
];

for (const [id, options] of plugins) {
  await build({
    ...common,
    ...options,
    entryPoints: [`extensions/plugins/${id}/frontend/src/index.tsx`],
    outfile: `extensions/plugins/${id}/frontend/dist/index.js`,
  });
  if (id === "com.vastplan.foundation.frontend.design-system.arco") {
    const result = spawnSync(process.execPath, ["engineering/tools/check-arco-on-demand.mjs"], { stdio: "inherit" });
    if (result.status !== 0) process.exit(result.status ?? 1);
  }
}
