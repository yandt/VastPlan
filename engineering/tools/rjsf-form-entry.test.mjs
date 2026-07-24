import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const rendererEntries = [
  "extensions/plugins/cn.vastplan.foundation.frontend.render.adapter.arco/frontend/src/json-schema-form.tsx",
  "extensions/plugins/cn.vastplan.foundation.frontend.render.adapter.mui/frontend/src/index.tsx",
];

test("production renderers bypass the RJSF test registry entry", async () => {
  for (const filename of rendererEntries) {
    const source = await readFile(filename, "utf8");
    assert.match(source, /from "@rjsf\/core\/lib\/components\/Form\.js"/u, `${filename} 必须直接使用公开 Form 子路径`);
    assert.doesNotMatch(source, /from "@rjsf\/core"/u, `${filename} 不得通过根入口拉入未声明的 AJV8 测试依赖`);
  }
});
