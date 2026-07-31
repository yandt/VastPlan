import { message, type LocalizedText } from "@vastplan/ui-contract";
import type { SemanticThemeTokens } from "./primitives.js";

export type AppearanceMode = "system" | "light" | "dark";
export type AppearanceScheme = "light" | "dark";

export interface AppearanceColorOverrides {
  readonly canvas?: string;
  readonly surface?: string;
  readonly text?: string;
  readonly mutedText?: string;
  readonly border?: string;
  readonly primary?: string;
  readonly danger?: string;
  readonly warning?: string;
  readonly success?: string;
}

export interface AppearanceSchemeSettings {
  readonly templateID: string;
  readonly colors?: AppearanceColorOverrides;
}

export interface PortalAppearanceSettings {
  readonly mode: AppearanceMode;
  readonly light: AppearanceSchemeSettings;
  readonly dark: AppearanceSchemeSettings;
}

export interface PortalAccountSummary {
  readonly subjectID: string;
  readonly tenantID: string;
  readonly displayName: string;
}

export interface AppearanceThemeTemplate {
  readonly id: string;
  readonly label: LocalizedText;
  readonly scheme: AppearanceScheme;
  readonly preview: { readonly background: string; readonly accent: string };
}

type CompletePalette = Required<AppearanceColorOverrides>;
const namespace = "cn.vastplan.foundation.frontend.render.adapter";
const hexPattern = /^#[0-9a-fA-F]{6}$/;

const palettes: Readonly<Record<string, CompletePalette>> = Object.freeze({
  light: palette("#f5f7fa", "#ffffff", "#1d2129", "#6b7785", "#d9d9d9", "#1677ff", "#d92d20", "#d97706", "#039855"),
  "light-soft": palette("#f4f6f8", "#fbfcfd", "#24303f", "#667085", "#d8dee8", "#6366f1", "#d92d20", "#b54708", "#027a48"),
  "light-warm": palette("#faf7f2", "#fffdf9", "#332a22", "#74685c", "#e6ddd2", "#b45309", "#c2410c", "#a16207", "#15803d"),
  dark: palette("#141414", "#1f1f1f", "#f5f5f5", "#a3a3a3", "#3a3a3a", "#4096ff", "#ff7875", "#ffc53d", "#52c41a"),
  "dark-midnight": palette("#0b1020", "#121a2d", "#eef2ff", "#9aa7bd", "#293450", "#6d8cff", "#ff6b6b", "#fbbf24", "#34d399"),
  "dark-slate": palette("#111827", "#1f2937", "#f3f4f6", "#9ca3af", "#374151", "#22d3ee", "#fb7185", "#fbbf24", "#4ade80"),
});

export const builtinAppearanceTemplates: readonly AppearanceThemeTemplate[] = Object.freeze([
  template("light", "theme.lightClassic", "浅色经典", "light"),
  template("light-soft", "theme.lightSoft", "浅色柔和", "light"),
  template("light-warm", "theme.lightWarm", "浅色暖调", "light"),
  template("dark", "theme.darkGraphite", "深色石墨", "dark"),
  template("dark-midnight", "theme.darkMidnight", "深色午夜", "dark"),
  template("dark-slate", "theme.darkSlate", "深色蓝灰", "dark"),
]);

export const defaultPortalAppearance: PortalAppearanceSettings = Object.freeze({
  mode: "system",
  light: Object.freeze({ templateID: "light" }),
  dark: Object.freeze({ templateID: "dark" }),
});

export function appearanceTemplate(id: string, scheme?: AppearanceScheme): AppearanceThemeTemplate {
  return builtinAppearanceTemplates.find((candidate) => candidate.id === id && (scheme === undefined || candidate.scheme === scheme))
    ?? builtinAppearanceTemplates.find((candidate) => candidate.id === (scheme === "dark" ? "dark" : "light"))!;
}

export function resolveAppearanceColors(templateID: string, overrides?: AppearanceColorOverrides): SemanticThemeTokens["color"] {
  const base = palettes[templateID] ?? palettes.light!;
  const merged = { ...base, ...sanitizeAppearanceColors(overrides) };
  return Object.freeze({
    ...merged,
    overlaySurface: merged.surface,
    hover: hexWithAlpha(merged.primary, "0f"),
    selected: hexWithAlpha(merged.primary, "1f"),
    focusRing: merged.primary,
  });
}

export function sanitizeAppearanceColors(value: unknown): AppearanceColorOverrides | undefined {
  if (typeof value !== "object" || value === null || Array.isArray(value)) return undefined;
  const allowed = ["canvas", "surface", "text", "mutedText", "border", "primary", "danger", "warning", "success"] as const;
  const record = value as Readonly<Record<string, unknown>>;
  const out: Record<string, string> = {};
  for (const key of allowed) if (typeof record[key] === "string" && hexPattern.test(record[key])) out[key] = record[key].toLowerCase();
  return Object.keys(out).length === 0 ? undefined : Object.freeze(out);
}

export function appearanceContrastIssue(templateID: string, overrides?: AppearanceColorOverrides): string | undefined {
  const colors = resolveAppearanceColors(templateID, overrides);
  if (contrast(colors.text, colors.canvas) < 4.5 || contrast(colors.text, colors.surface) < 4.5) return "正文与背景对比度不足 4.5:1";
  if (contrast(colors.mutedText, colors.surface) < 3) return "次要文字与表面对比度不足 3:1";
  return undefined;
}

export function hexToRGBTriplet(value: string): string {
  const clean = value.slice(1);
  return `${Number.parseInt(clean.slice(0, 2), 16)}, ${Number.parseInt(clean.slice(2, 4), 16)}, ${Number.parseInt(clean.slice(4, 6), 16)}`;
}

function palette(canvas: string, surface: string, text: string, mutedText: string, border: string, primary: string, danger: string, warning: string, success: string): CompletePalette {
  return Object.freeze({ canvas, surface, text, mutedText, border, primary, danger, warning, success });
}

function template(id: string, key: string, fallback: string, scheme: AppearanceScheme): AppearanceThemeTemplate {
  const colors = palettes[id]!;
  return Object.freeze({ id, label: message(namespace, key, fallback), scheme, preview: Object.freeze({ background: colors.canvas, accent: colors.primary }) });
}

function hexWithAlpha(value: string, alpha: string): string { return `${value}${alpha}`; }
function contrast(left: string, right: string): number {
  const high = Math.max(luminance(left), luminance(right));
  const low = Math.min(luminance(left), luminance(right));
  return (high + 0.05) / (low + 0.05);
}
function luminance(value: string): number {
  const channels = [1, 3, 5].map((offset) => Number.parseInt(value.slice(offset, offset + 2), 16) / 255).map((channel) => channel <= 0.03928 ? channel / 12.92 : ((channel + 0.055) / 1.055) ** 2.4);
  return channels[0]! * 0.2126 + channels[1]! * 0.7152 + channels[2]! * 0.0722;
}
