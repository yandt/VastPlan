import { useState, type DragEvent, type KeyboardEvent } from "react";
import { usePortalUI } from "@vastplan/ui-primitives";
import type { CollectionColumnPreference } from "./model.js";
import { moveItem } from "./preferences.js";
import { reorderColumns, toggleColumnVisibility } from "./column-preference-actions.js";

export function ColumnPreferenceList({ columns, labels, dragLabel, showLabel, hideLabel, onChange }: {
  columns: readonly CollectionColumnPreference[];
  labels: Readonly<Record<string, string>>;
  dragLabel: string;
  showLabel: string;
  hideLabel: string;
  onChange(columns: readonly CollectionColumnPreference[]): void;
}) {
  const ui = usePortalUI();
  const [draggingKey, setDraggingKey] = useState<string>();
  const [overKey, setOverKey] = useState<string>();
  const finishDrag = () => { setDraggingKey(undefined); setOverKey(undefined); };
  const keyboardMove = (event: KeyboardEvent<HTMLButtonElement>, index: number) => {
    if (event.key !== "ArrowUp" && event.key !== "ArrowDown") return;
    event.preventDefault();
    onChange(moveItem(columns, index, event.key === "ArrowUp" ? -1 : 1));
  };
  const drop = (event: DragEvent<HTMLDivElement>, targetKey: string) => {
    event.preventDefault();
    const sourceKey = draggingKey ?? event.dataTransfer.getData("text/plain");
    if (sourceKey !== "") onChange(reorderColumns(columns, sourceKey, targetKey));
    finishDrag();
  };
  return <div role="list" style={{ minWidth: 0, width: "100%", maxHeight: 256, display: "grid", gap: 2, overflowX: "hidden", overflowY: "auto", overscrollBehavior: "contain", scrollbarGutter: "stable" }}>
    {columns.map((column, index) => {
      const label = labels[column.key] ?? column.key;
      const hidden = !column.visible;
      const visibilityLabel = hidden ? showLabel : hideLabel;
      return <div key={column.key} role="listitem" onDragOver={(event) => { event.preventDefault(); event.dataTransfer.dropEffect = "move"; setOverKey(column.key); }} onDrop={(event) => drop(event, column.key)} style={{
        boxSizing: "border-box", minWidth: 0, minHeight: 32, display: "grid", gridTemplateColumns: "28px max-content 28px", alignItems: "center", gap: 4, padding: "2px 3px",
        borderRadius: 5, outline: overKey === column.key && draggingKey !== column.key ? `1px solid ${ui.theme.tokens.color.primary}` : "none",
        background: hidden ? ui.theme.tokens.color.hover : "transparent", color: hidden ? ui.theme.tokens.color.mutedText : ui.theme.tokens.color.text,
        opacity: draggingKey === column.key ? 0.55 : 1,
      }}>
        <button type="button" draggable aria-label={`${dragLabel}: ${label}`} title={`${dragLabel}: ${label}`} onDragStart={(event) => {
          event.dataTransfer.effectAllowed = "move";
          event.dataTransfer.setData("text/plain", column.key);
          setDraggingKey(column.key);
        }} onDragEnd={finishDrag} onKeyDown={(event) => keyboardMove(event, index)} style={iconButtonStyle("grab")}><ui.Icon name="drag" size="sm" /></button>
        <span title={label} style={{ minWidth: 0, maxWidth: 196, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap", fontSize: 13 }}>{label}</span>
        <button type="button" aria-label={`${visibilityLabel}: ${label}`} title={`${visibilityLabel}: ${label}`} aria-pressed={column.visible} onClick={() => onChange(toggleColumnVisibility(columns, column.key))} style={iconButtonStyle("pointer")}><ui.Icon name={column.visible ? "visibility" : "visibilityOff"} size="sm" /></button>
      </div>;
    })}
  </div>;
}

function iconButtonStyle(cursor: "grab" | "pointer") {
  return { width: 28, height: 28, padding: 0, display: "inline-grid", placeItems: "center", border: 0, borderRadius: 4, background: "transparent", color: "inherit", cursor } as const;
}
