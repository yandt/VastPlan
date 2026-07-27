import type { ReactNode } from "react";
import { DragDropProvider } from "@dnd-kit/react";
import { useSortable } from "@dnd-kit/react/sortable";
import type { SortableListProps, SortableRenderState } from "./SortableList.js";
import { moveSortableItem } from "./sortable-model.js";

export function DndSortableList<Item>({ items, getID, group, onReorder, children }: SortableListProps<Item>) {
  return <DragDropProvider onDragEnd={(event) => {
    if (event.canceled) return;
    const sourceID = event.operation.source?.id;
    const targetID = event.operation.target?.id;
    if (sourceID === undefined || targetID === undefined || sourceID === targetID) return;
    const from = items.findIndex((item) => getID(item) === String(sourceID));
    const to = items.findIndex((item) => getID(item) === String(targetID));
    if (from >= 0 && to >= 0) onReorder(moveSortableItem(items, from, to));
  }}>
    {items.map((item, index) => {
      const id = getID(item);
      return <SortableItem key={id} id={id} index={index} group={group}>
        {(state) => children(item, index, state)}
      </SortableItem>;
    })}
  </DragDropProvider>;
}

function SortableItem({ id, index, group, children }: {
  id: string;
  index: number;
  group: string;
  children(state: SortableRenderState): ReactNode;
}) {
  const { ref, handleRef, isDragging, isDropTarget } = useSortable({ id, index, group });
  return children({
    itemRef: (element) => ref(element),
    handleRef: (element) => handleRef(element),
    isDragging,
    isDropTarget,
  });
}
