import { useCallback, useState } from "react";
import type { RecordDetailPageDefinition } from "@vastplan/workbench-sdk";
import { RecordWorkspace } from "./RecordWorkspace.js";
import { useRecordLoader } from "./useRecordLoader.js";

export function RecordDetailPage({ page }: { page: RecordDetailPageDefinition }) {
  const [dirty, setDirty] = useState(false);
  const load = useCallback((_: string, signal: AbortSignal) => page.load(signal), [page]);
  const data = useRecordLoader(page.id, load);
  return <RecordWorkspace page={page} data={data} refresh={data.refresh} onDirtyChange={setDirty} />;
}
