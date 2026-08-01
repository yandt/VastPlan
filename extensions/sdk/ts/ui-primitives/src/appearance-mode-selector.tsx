import type { ReactNode } from "react";
import { resolveAppearanceColors, type AppearanceMode, type PortalAppearanceSettings } from "./appearance.js";
import { usePortalUI } from "./portal-ui-context.js";

export interface AppearanceModeSelectorProps {
  readonly ariaLabel: string;
  readonly value: AppearanceMode;
  readonly appearance: PortalAppearanceSettings;
  readonly labels: Readonly<Record<AppearanceMode, ReactNode>>;
  onChange(mode: AppearanceMode): void;
}

/** A visual, accessible chooser for system, light, and dark appearance modes. */
export function AppearanceModeSelector({ ariaLabel, value, appearance, labels, onChange }: AppearanceModeSelectorProps) {
  const ui = usePortalUI();
  return <div role="group" aria-label={ariaLabel} style={{ display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(120px, 1fr))", gap: 8, width: "100%" }}>
    {(["system", "light", "dark"] as const).map((mode) => <button key={mode} type="button" aria-pressed={value === mode} onClick={() => onChange(mode)} style={{ display: "grid", gap: 6, minWidth: 0, padding: 4, border: value === mode ? `${ui.theme.tokens.focus.width}px solid ${ui.theme.tokens.color.focusRing}` : `1px solid ${ui.theme.tokens.color.border}`, borderRadius: ui.theme.tokens.radius.sm, background: ui.theme.tokens.color.surface, color: ui.theme.tokens.color.text, cursor: "pointer", textAlign: "left" }}>
      <AppearanceModePreview mode={mode} appearance={appearance} />
      <span style={{ padding: "0 4px 2px", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>{labels[mode]}</span>
    </button>)}
  </div>;
}

function AppearanceModePreview({ mode, appearance }: { mode: AppearanceMode; appearance: PortalAppearanceSettings }) {
  const light = resolveAppearanceColors(appearance.light.templateID, appearance.light.colors);
  const dark = resolveAppearanceColors(appearance.dark.templateID, appearance.dark.colors);
  return <span aria-hidden style={{ display: "grid", gridTemplateColumns: mode === "system" ? "1fr 1fr" : "1fr", height: 62, overflow: "hidden", borderRadius: 2 }}>
    {mode === "dark" ? <PreviewSurface colors={dark} /> : mode === "light" ? <PreviewSurface colors={light} /> : <><PreviewSurface colors={light} /><PreviewSurface colors={dark} /></>}
  </span>;
}

function PreviewSurface({ colors }: { colors: ReturnType<typeof resolveAppearanceColors> }) {
  return <span style={{ display: "grid", gridTemplateRows: "8px minmax(0,1fr)", minWidth: 0, background: colors.canvas }}>
    <span style={{ background: colors.primary }} />
    <span style={{ display: "grid", gridTemplateColumns: "20% minmax(0,1fr)", gap: 4, padding: 5 }}>
      <span style={{ borderRadius: 1, background: colors.surface }} />
      <span style={{ display: "grid", alignContent: "center", gap: 3, minWidth: 0 }}>
        <span style={{ width: "78%", height: 4, borderRadius: 1, background: colors.text }} />
        <span style={{ width: "54%", height: 4, borderRadius: 1, background: colors.mutedText }} />
      </span>
    </span>
  </span>;
}
