import assert from "node:assert/strict";
import { mkdtemp, mkdir, rm, writeFile } from "node:fs/promises";
import { pathToFileURL } from "node:url";
import { resolve } from "node:path";
import test from "node:test";
import { buildFrontendServerGraph } from "./frontend-server-build.mjs";

test("server build can load React SSR Node built-ins from ESM", async () => {
  const root = await mkdtemp(resolve("extensions/plugins/cn.vastplan.foundation.frontend.runtime.engine.react/frontend/.server-build-test-"));
  try {
    const source = resolve(root, "frontend/src/server.tsx");
    await mkdir(resolve(root, "frontend/src"), { recursive: true });
    await writeFile(resolve(root, "package.json"), '{"type":"module"}\n');
    await writeFile(source, `
      import { createElement } from "react";
      import { renderToStaticMarkup } from "react-dom/server";
      export default { id: "ui.runtime.engine.server", render() { return { html: renderToStaticMarkup(createElement("main", null, "ready")) }; } };
    `);
    const { graph } = await buildFrontendServerGraph({ buildRoot: root, serverEntry: "frontend/dist/server.js", serverSource: source });
    assert.deepEqual(graph.externals, ["stream", "util"]);
    const runtime = (await import(`${pathToFileURL(resolve(root, graph.entry)).href}?test=${Date.now()}`)).default;
    assert.deepEqual(await runtime.render(), { html: "<main>ready</main>" });
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});
