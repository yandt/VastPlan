import { arrayMove } from "@dnd-kit/helpers";

/** Immutable reorder helper shared by pointer and keyboard interactions. */
export function moveSortableItem<Item>(items: readonly Item[], from: number, to: number): Item[] {
  if (from < 0 || to < 0 || from >= items.length || to >= items.length || from === to) return [...items];
  return arrayMove([...items], from, to);
}
