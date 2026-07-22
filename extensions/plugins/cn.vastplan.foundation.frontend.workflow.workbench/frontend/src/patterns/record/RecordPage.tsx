import type { RecordPageDefinition } from "@vastplan/workbench-sdk";
import { MasterDetailPage } from "./MasterDetailPage.js";
import { RecordDetailPage } from "./RecordDetailPage.js";
import { TreeDetailPage } from "./TreeDetailPage.js";

export function RecordPage({ page }: { page: RecordPageDefinition }) {
  if (page.pattern === "master-detail") return <MasterDetailPage page={page} />;
  if (page.pattern === "tree-detail") return <TreeDetailPage page={page} />;
  return <RecordDetailPage page={page} />;
}
