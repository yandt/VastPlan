import type { RecordTreeNode } from "@vastplan/workbench-sdk";

const maxTreeNodes = 5_000;
const maxTreeDepth = 16;

export function validateRecordTree(nodes: readonly RecordTreeNode[]): readonly RecordTreeNode[] {
  const ids = new Set<string>();
  let count = 0;
  const visit = (items: readonly RecordTreeNode[], depth: number): readonly RecordTreeNode[] => {
    if (depth > maxTreeDepth) throw new Error(`记录树深度不能超过 ${maxTreeDepth}`);
    return items.map((item) => {
      count += 1;
      if (count > maxTreeNodes) throw new Error(`记录树节点不能超过 ${maxTreeNodes}`);
      if (!/^[A-Za-z0-9][A-Za-z0-9._:-]{0,239}$/.test(item.id) || ids.has(item.id) || item.title.trim() === "") throw new Error(`记录树节点无效或重复: ${item.id}`);
      ids.add(item.id);
      return Object.freeze({ ...item, children: item.children === undefined ? undefined : visit(item.children, depth + 1) });
    });
  };
  return Object.freeze(visit(nodes, 1));
}

export function firstSelectableTreeNode(nodes: readonly RecordTreeNode[]): string | undefined {
  for (const node of nodes) {
    if (!node.disabled) return node.id;
    const child = firstSelectableTreeNode(node.children ?? []);
    if (child !== undefined) return child;
  }
  return undefined;
}

export function hasSelectableTreeNode(nodes: readonly RecordTreeNode[], id: string): boolean {
  for (const node of nodes) {
    if (node.id === id) return !node.disabled;
    if (hasSelectableTreeNode(node.children ?? [], id)) return true;
  }
  return false;
}

export function initialExpandedTreeNodes(nodes: readonly RecordTreeNode[], depth: number): readonly string[] {
  const expanded: string[] = [];
  const visit = (items: readonly RecordTreeNode[], current: number) => {
    if (current >= depth) return;
    for (const item of items) {
      if ((item.children?.length ?? 0) === 0) continue;
      expanded.push(item.id);
      visit(item.children ?? [], current + 1);
    }
  };
  visit(nodes, 0);
  return expanded;
}
