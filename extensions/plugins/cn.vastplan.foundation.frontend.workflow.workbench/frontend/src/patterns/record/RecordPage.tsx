import { useSyncExternalStore } from "react";
import type { PageRefreshSignal } from "@vastplan/ui-primitives";
import type { RecordPageDefinition } from "@vastplan/workbench-sdk";
import { MasterDetailPage } from "./MasterDetailPage.js";
import { RecordDetailPage } from "./RecordDetailPage.js";
import { TreeDetailPage } from "./TreeDetailPage.js";

const emptyRefreshSignal: PageRefreshSignal = Object.freeze({ subscribe: () => () => undefined, getSnapshot: () => 0 });

export function RecordPage({ page, refreshSignal = emptyRefreshSignal }: { page: RecordPageDefinition; refreshSignal?: PageRefreshSignal }) {
  const refreshRevision = useSyncExternalStore(refreshSignal.subscribe, refreshSignal.getSnapshot, refreshSignal.getSnapshot);
  if (page.pattern === "master-detail") return <MasterDetailPage key={refreshRevision} page={page} />;
  if (page.pattern === "tree-detail") return <TreeDetailPage key={refreshRevision} page={page} />;
  return <RecordDetailPage key={refreshRevision} page={page} />;
}
