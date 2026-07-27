const layoutInputPattern = /(?:^|\/)(?:react-grid-layout|react-draggable|react-resizable)(?:@[^/]*)?\//;
const dragDropInputPattern = /(?:^|\/)@dnd-kit(?:\+|\/)(?:react|dom)(?:@[^/]*)?\//;

/** Keeps optional interaction engines out of the always-loaded Workbench entry. */
export function assertWorkbenchDeferredLayout(metafile, layoutBudgetBytes = 250_000, dragDropBudgetBytes = 200_000) {
  const outputs = Object.entries(metafile.outputs ?? {});
  const entry = outputs.find(([, metadata]) => typeof metadata.entryPoint === "string" && /frontend\/src\/index\.tsx?$/.test(metadata.entryPoint));
  if (entry === undefined) throw new Error("Workbench 构建缺少入口输出");
  const [entryPath, entryMetadata] = entry;
  if (layoutInputs(entryMetadata).length > 0) throw new Error("react-grid-layout 禁止进入 Workbench 首屏入口");
  if (dragDropInputs(entryMetadata).length > 0) throw new Error("dnd-kit 禁止进入 Workbench 首屏入口");
  return {
    layout: deferredStats(outputs, entryPath, layoutInputs, "react-grid-layout", layoutBudgetBytes),
    dragDrop: deferredStats(outputs, entryPath, dragDropInputs, "dnd-kit", dragDropBudgetBytes),
  };
}

function layoutInputs(metadata) {
  return Object.keys(metadata.inputs ?? {}).filter((path) => layoutInputPattern.test(path));
}

function dragDropInputs(metadata) {
  return Object.keys(metadata.inputs ?? {}).filter((path) => dragDropInputPattern.test(path));
}

function deferredStats(outputs, entryPath, inputSelector, label, budgetBytes) {
  const deferred = outputs.filter(([path, metadata]) => path !== entryPath && inputSelector(metadata).length > 0);
  if (deferred.length === 0) throw new Error(`Workbench 构建缺少 ${label} 延迟 Chunk`);
  const bytes = deferred.reduce((total, [, metadata]) => total + (metadata.bytes ?? 0), 0);
  if (bytes > budgetBytes) throw new Error(`Workbench ${label} 延迟 Chunk 超过预算: ${bytes}/${budgetBytes} bytes`);
  return { chunks: deferred.length, bytes, budgetBytes };
}
