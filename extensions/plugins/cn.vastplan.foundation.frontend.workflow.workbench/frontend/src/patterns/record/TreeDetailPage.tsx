import { useCallback, useEffect, useMemo, useState } from "react";
import { message, usePortalI18n, usePortalUI, type RecordTreeItem } from "@vastplan/ui-primitives";
import type { RecordTreeNode, TreeDetailPageDefinition } from "@vastplan/workbench-sdk";
import { firstSelectableTreeNode, hasSelectableTreeNode, initialExpandedTreeNodes, validateRecordTree } from "./tree-model.js";
import { initialRecordSelection, persistRecordSelection } from "./selection.js";
import { RecordWorkspace } from "./RecordWorkspace.js";
import { useNarrowSplitView } from "./useNarrowSplitView.js";
import { useRecordLoader } from "./useRecordLoader.js";

const namespace = "cn.vastplan.foundation.frontend.workflow.workbench";

export function TreeDetailPage({ page }: { page: TreeDetailPageDefinition }) {
  const ui = usePortalUI();
  const i18n = usePortalI18n();
  const parameter = page.tree.selectionParam ?? page.tree.id;
  const [selected, setSelected] = useState(() => initialRecordSelection(parameter));
  const [nodes, setNodes] = useState<readonly RecordTreeNode[]>([]);
  const [expanded, setExpanded] = useState<readonly string[]>([]);
  const [loading, setLoading] = useState(true);
  const [failure, setFailure] = useState<string>();
  const [revision, setRevision] = useState(0);
  const [dirty, setDirty] = useState(false);
  const [showPrimary, setShowPrimary] = useState(selected === undefined);
  const narrow = useNarrowSplitView();
  useEffect(() => {
    const controller = new AbortController();
    setLoading(true); setFailure(undefined);
    void page.loadTree(controller.signal).then((result) => {
      if (controller.signal.aborted) return;
      const next = validateRecordTree(result);
      setNodes(next); setExpanded(initialExpandedTreeNodes(next, page.tree.defaultExpandedDepth ?? 1));
      const first = firstSelectableTreeNode(next);
      setSelected((current) => {
        if (current !== undefined && hasSelectableTreeNode(next, current)) return current;
        if (first === undefined) return undefined;
        persistRecordSelection(parameter, first);
        return first;
      });
    }).catch((error: unknown) => { if (!controller.signal.aborted) setFailure(error instanceof Error ? error.message : String(error)); })
      .finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => controller.abort();
  }, [page, parameter, revision]);
  const loadRecord = useCallback((key: string, signal: AbortSignal) => page.loadRecord(key, signal), [page]);
  const data = useRecordLoader(selected, loadRecord);
  const refresh = useCallback(() => { setRevision((value) => value + 1); data.refresh(); }, [data.refresh]);
  const choose = async (id: string) => {
    if (id === selected) { if (narrow) setShowPrimary(false); return; }
    if (dirty && !await ui.confirm({ title: i18n.text(message(namespace, "form.discardTitle", "放弃未保存的修改？")), content: i18n.text(message(namespace, "record.selectionDiscard", "切换记录后，当前未保存修改不会保留。")) })) return;
    setSelected(id); persistRecordSelection(parameter, id); setShowPrimary(false);
  };
  const primary = <ui.Stack gap="sm">
    <ui.Stack direction="row" align="center" justify="between"><strong>{i18n.text(page.tree.title)}</strong><ui.IconButton icon="refresh" label={i18n.text(message(namespace, "action.refresh", "刷新"))} loading={loading} onClick={refresh} /></ui.Stack>
    {failure === undefined ? null : <ui.ErrorState title={failure} retry={refresh} />}
    {loading ? <ui.Skeleton rows={7} /> : nodes.length === 0 ? <ui.EmptyState title={i18n.text(page.tree.emptyTitle ?? message(namespace, "record.treeEmpty", "暂无节点"))} /> : <ui.RecordTree items={treeItems(nodes, ui)} selectedID={selected} expandedIDs={expanded} ariaLabel={i18n.text(page.tree.title)} onSelect={(id) => void choose(id)} onExpandedChange={setExpanded} />}
  </ui.Stack>;
  return <RecordWorkspace page={page} data={data} refresh={refresh} primary={primary} primaryLabel={i18n.text(page.tree.title)} splitMode={narrow ? showPrimary ? "primary" : "secondary" : "both"} onBack={narrow ? () => setShowPrimary(true) : undefined} onDirtyChange={setDirty} />;
}

function treeItems(nodes: readonly RecordTreeNode[], ui: ReturnType<typeof usePortalUI>): readonly RecordTreeItem[] {
  return nodes.map((node) => ({ id: node.id, title: node.title, description: node.description, disabled: node.disabled,
    status: node.status === undefined ? undefined : <ui.Status tone={node.status.tone}>{node.status.label}</ui.Status>,
    children: node.children === undefined ? undefined : treeItems(node.children, ui) }));
}
