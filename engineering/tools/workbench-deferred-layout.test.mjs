import assert from "node:assert/strict";
import test from "node:test";
import { assertWorkbenchDeferredLayout } from "./workbench-deferred-layout.mjs";

const entry = "extensions/plugins/cn.vastplan.foundation.frontend.workflow.workbench/frontend/dist/index.js";
const chunk = "extensions/plugins/cn.vastplan.foundation.frontend.workflow.workbench/frontend/dist/chunks/DashboardGrid.js";

test("accepts react-grid-layout only in a bounded deferred chunk", () => {
  const result = assertWorkbenchDeferredLayout({ outputs: {
    [entry]: { entryPoint: "extensions/plugins/cn.vastplan.foundation.frontend.workflow.workbench/frontend/src/index.tsx", bytes: 100, inputs: { "src/index.tsx": {} } },
    [chunk]: { bytes: 75_000, inputs: { ".vastplan/cache/node/virtual-store/react-grid-layout@2.2.3/node_modules/react-grid-layout/dist/index.js": {} } },
    "dist/chunks/DndSortableList.js": { bytes: 90_000, inputs: { ".vastplan/cache/node/virtual-store/@dnd-kit+react@0.5.0/node_modules/@dnd-kit/react/index.js": {} } },
  } });
  assert.deepEqual(result, { layout: { chunks: 1, bytes: 75_000, budgetBytes: 250_000 }, dragDrop: { chunks: 1, bytes: 90_000, budgetBytes: 200_000 } });
});

test("rejects dashboard code in the entry or above its deferred budget", () => {
  assert.throws(() => assertWorkbenchDeferredLayout({ outputs: {
    [entry]: { entryPoint: "extensions/plugins/cn.vastplan.foundation.frontend.workflow.workbench/frontend/src/index.tsx", bytes: 100, inputs: { "node_modules/react-grid-layout/index.js": {} } },
  } }), /首屏入口/);
  assert.throws(() => assertWorkbenchDeferredLayout({ outputs: {
    [entry]: { entryPoint: "extensions/plugins/cn.vastplan.foundation.frontend.workflow.workbench/frontend/src/index.tsx", bytes: 100, inputs: {} },
    [chunk]: { bytes: 250_001, inputs: { "node_modules/react-grid-layout/index.js": {} } },
    "dist/chunks/DndSortableList.js": { bytes: 90_000, inputs: { "node_modules/@dnd-kit/react/index.js": {} } },
  } }), /超过预算/);
});

test("rejects dnd-kit in the always-loaded Workbench entry", () => {
  assert.throws(() => assertWorkbenchDeferredLayout({ outputs: {
    [entry]: { entryPoint: "extensions/plugins/cn.vastplan.foundation.frontend.workflow.workbench/frontend/src/index.tsx", bytes: 100, inputs: { "node_modules/@dnd-kit/react/index.js": {} } },
    [chunk]: { bytes: 75_000, inputs: { "node_modules/react-grid-layout/index.js": {} } },
  } }), /dnd-kit.*首屏入口/);
});
