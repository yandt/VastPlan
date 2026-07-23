import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { mkdtemp, readFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import test from "node:test";

test("builds every node-worker backend as a self-contained ESM module", async () => {
  const output = await mkdtemp(join(tmpdir(), "vastplan-node-backend-"));
  try {
    execFileSync(process.execPath, ["engineering/tools/build-node-backend-plugins.mjs", "--out-dir", output], { cwd: resolve("."), stdio: "pipe" });
    for (const id of [
      "cn.vastplan.foundation.backend.runtime.node-worker-hello",
      "cn.vastplan.foundation.security.authentication.provider.oidc",
      "cn.vastplan.platform.security.authentication.delivery.webhook",
    ]) {
      const bundle = await readFile(join(output, id, "backend/main.mjs"), "utf8");
      assert.match(bundle, /__vastplanCreateRequire/);
      assert.doesNotMatch(bundle, /from\s*["']@vastplan\//);
    }
  } finally {
    await rm(output, { recursive: true, force: true });
  }
});
