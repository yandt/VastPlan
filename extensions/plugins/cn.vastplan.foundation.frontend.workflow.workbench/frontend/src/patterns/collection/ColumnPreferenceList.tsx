import type { KeyboardEvent } from "react";
import { usePortalUI } from "@vastplan/ui-primitives";
import type { CollectionColumnPreference } from "./model.js";
import { toggleColumnVisibility } from "./column-preference-actions.js";
import { SortableList } from "../interaction/SortableList.js";
import { moveSortableItem } from "../interaction/sortable-model.js";

export function ColumnPreferenceList({ columns, labels, dragLabel, showLabel, hideLabel, onChange }: {
  columns: readonly CollectionColumnPreference[];
  labels: Readonly<Record<string, string>>;
  dragLabel: string;
  showLabel: string;
  hideLabel: string;
  onChange(columns: readonly CollectionColumnPreference[]): void;
}) {
  const ui = usePortalUI();
  const keyboardMove = (event: KeyboardEvent<HTMLButtonElement>, index: number, dragging: boolean) => {
    if (dragging) return;
    if (event.key !== "ArrowUp" && event.key !== "ArrowDown") return;
    event.preventDefault();
    onChange(moveSortableItem(columns, index, index + (event.key === "ArrowUp" ? -1 : 1)));
  };
  return <div role="list" style={{ minWidth: 0, width: "100%", maxHeight: 256, display: "grid", gap: 2, overflowX: "hidden", overflowY: "auto", overscrollBehavior: "contain", scrollbarGutter: "stable" }}>
    <SortableList items={columns} getID={(column) => column.key} group="collection-columns" onReorder={onChange}>
      {(column, index, { itemRef, handleRef, isDragging, isDropTarget }) => {
        const label = labels[column.key] ?? column.key;
        const hidden = !column.visible;
        const visibilityLabel = hidden ? showLabel : hideLabel;
        return <div ref={itemRef} role="listitem" style={{
          boxSizing: "border-box", minWidth: 0, minHeight: 32, display: "grid", gridTemplateColumns: "28px minmax(0,1fr) 28px", alignItems: "center", gap: 4, padding: "2px 3px",
          borderRadius: 5, outline: isDropTarget && !isDragging ? `1px solid ${ui.theme.tokens.color.primary}` : "none",
          background: hidden ? ui.theme.tokens.color.hover : "transparent", color: hidden ? ui.theme.tokens.color.mutedText : ui.theme.tokens.color.text,
          opacity: isDragging ? 0.55 : 1,
        }}>
          <button ref={handleRef} type="button" aria-label={`${dragLabel}: ${label}`} title={`${dragLabel}: ${label}`} onKeyDown={(event) => keyboardMove(event, index, isDragging)} style={iconButtonStyle("grab")}><ui.Icon name="drag" size="sm" /></button>
          <span title={label} style={{ minWidth: 0, maxWidth: 196, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap", fontSize: 13 }}>{label}</span>
          <button type="button" aria-label={`${visibilityLabel}: ${label}`} title={`${visibilityLabel}: ${label}`} aria-pressed={column.visible} onClick={() => onChange(toggleColumnVisibility(columns, column.key))} style={iconButtonStyle("pointer")}><ui.Icon name={column.visible ? "visibility" : "visibilityOff"} size="sm" /></button>
        </div>;
      }}
    </SortableList>
  </div>;
}

function iconButtonStyle(cursor: "grab" | "pointer") {
  return { width: 28, height: 28, padding: 0, display: "inline-grid", placeItems: "center", border: 0, borderRadius: 4, background: "transparent", color: "inherit", cursor } as const;
}
