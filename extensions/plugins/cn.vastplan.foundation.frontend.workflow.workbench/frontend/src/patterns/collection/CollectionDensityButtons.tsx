import type { CollectionDensity } from "@vastplan/ui-contract";
import { usePortalUI } from "@vastplan/ui-primitives";

export function CollectionDensityButtons({ label, value, options, labels, onChange }: {
  label: string;
  value: CollectionDensity;
  options: readonly CollectionDensity[];
  labels: Readonly<Record<CollectionDensity, string>>;
  onChange(value: CollectionDensity): void;
}) {
  const ui = usePortalUI();
  if (options.length <= 1) return null;
  return <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: 8 }}>
    <span style={{ color: ui.theme.tokens.color.mutedText, fontSize: 12, whiteSpace: "nowrap" }}>{label}</span>
    <div role="radiogroup" aria-label={label} style={{ display: "inline-flex", alignItems: "center", gap: 2 }}>
      {options.map((option) => {
        const selected = option === value;
        return <button key={option} type="button" role="radio" aria-checked={selected} title={labels[option]} onClick={() => onChange(option)} style={{
          boxSizing: "border-box", minWidth: 38, height: 24, padding: "0 7px", border: `1px solid ${selected ? ui.theme.tokens.color.primary : ui.theme.tokens.color.border}`,
          borderRadius: 5, background: selected ? ui.theme.tokens.color.selected : "transparent", color: selected ? ui.theme.tokens.color.primary : ui.theme.tokens.color.mutedText,
          font: "inherit", fontSize: 12, lineHeight: "22px", cursor: "pointer",
        }}>{labels[option]}</button>;
      })}
    </div>
  </div>;
}
