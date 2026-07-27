import { Fragment, lazy, Suspense, type ReactElement, type ReactNode } from "react";

export interface SortableRenderState {
  itemRef(element: HTMLElement | null): void;
  handleRef(element: HTMLElement | null): void;
  isDragging: boolean;
  isDropTarget: boolean;
}

/**
 * Workbench-owned sortable boundary. Pattern components consume stable IDs and
 * never depend on dnd-kit event objects, sensors, or package versions.
 */
export interface SortableListProps<Item> {
  items: readonly Item[];
  getID(item: Item): string;
  group: string;
  onReorder(items: readonly Item[]): void;
  children(item: Item, index: number, state: SortableRenderState): ReactNode;
}

const DeferredSortableList = lazy(() => import("./DndSortableList.js").then((module) => ({ default: module.DndSortableList }))) as unknown as <Item>(props: SortableListProps<Item>) => ReactElement;
const inactiveState: SortableRenderState = { itemRef() {}, handleRef() {}, isDragging: false, isDropTarget: false };

/** Keeps the pointer/touch runtime out of the Workbench entry until a sortable surface mounts. */
export function SortableList<Item>(props: SortableListProps<Item>) {
  const fallback = <>{props.items.map((item, index) => <Fragment key={props.getID(item)}>{props.children(item, index, inactiveState)}</Fragment>)}</>;
  return <Suspense fallback={fallback}><DeferredSortableList {...props} /></Suspense>;
}
