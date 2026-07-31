import {
  defaultPortalAppearance,
  sanitizeAppearanceColors,
  type PortalAppearanceSettings,
} from "@vastplan/ui-primitives";
import type { PortalPrepareOptions, PortalSpec } from "./portal-contracts";

const idPattern = /^[a-zA-Z0-9][a-zA-Z0-9._:-]{0,127}$/;
const storageVersion = 1;
const maxStorageBytes = 64 << 10;

interface RendererAppearance {
  readonly iconThemeID?: string;
  readonly theme: PortalAppearanceSettings;
}

interface StoredPortalAppearance {
  readonly version: typeof storageVersion;
  readonly rendererID?: string;
  readonly pendingRendererID?: string;
  readonly shellTemplateID?: string;
  readonly renderers: Readonly<Record<string, RendererAppearance>>;
}

/** Owns every user-facing appearance choice. It never performs network I/O. */
export class PortalAppearanceSession {
  private values: StoredPortalAppearance;

  private constructor(private readonly portal: PortalSpec) {
    this.values = readAppearance(portal);
  }

  public static open(portal: PortalSpec): PortalAppearanceSession {
    return new PortalAppearanceSession(portal);
  }

  public resolve(portal: PortalSpec = this.portal): PortalPrepareOptions {
    const rendererID = validRendererChoice(portal, this.values.pendingRendererID) ?? validRendererChoice(portal, this.values.rendererID) ?? portal.renderAdapter.config.defaultRenderer;
    const option = this.values.renderers[rendererID];
    const rendererConfig = portal.renderAdapter.config.rendererOptions?.[rendererID];
    const scheme = resolveSystemScheme(option?.theme.mode ?? defaultPortalAppearance.mode);
    const selectedTheme = option?.theme[scheme].templateID;
    return Object.freeze({
      rendererID,
      shellTemplateID: validShell(portal, this.values.shellTemplateID) ?? portal.shell.config.defaultTemplate,
      themeTemplateID: validAllowedID(selectedTheme, rendererConfig?.allowedThemeTemplates) ?? rendererConfig?.themeTemplate,
      iconThemeID: validAllowedID(option?.iconThemeID, rendererConfig?.allowedIconThemes) ?? rendererConfig?.iconTheme,
    });
  }

  public appearance(rendererID: string): PortalAppearanceSettings {
    return this.values.renderers[rendererID]?.theme ?? defaultPortalAppearance;
  }

  public setRenderer(rendererID: string): void {
    if (validRendererChoice(this.portal, rendererID) === undefined) return;
    this.save({ ...this.values, pendingRendererID: rendererID });
  }

  public hasPendingRenderer(): boolean { return this.values.pendingRendererID !== undefined; }

  public commitPendingRenderer(): void {
    const pending = validRendererChoice(this.portal, this.values.pendingRendererID);
    if (pending === undefined) return;
    const { pendingRendererID: _, ...rest } = this.values;
    this.save({ ...rest, rendererID: pending });
  }

  public discardPendingRenderer(): void {
    if (this.values.pendingRendererID === undefined) return;
    const { pendingRendererID: _, ...rest } = this.values;
    this.save(rest);
  }

  public setShellTemplate(shellTemplateID: string): void {
    if (validShell(this.portal, shellTemplateID) === undefined) return;
    this.save({ ...this.values, shellTemplateID });
  }

  public setIconTheme(rendererID: string, iconThemeID: string): void {
    const allowed = this.portal.renderAdapter.config.rendererOptions?.[rendererID]?.allowedIconThemes;
    if (validAllowedID(iconThemeID, allowed) === undefined) return;
    this.updateRenderer(rendererID, { iconThemeID });
  }

  public setAppearance(rendererID: string, theme: PortalAppearanceSettings): void {
    this.updateRenderer(rendererID, { theme: sanitizeTheme(theme, this.portal.renderAdapter.config.rendererOptions?.[rendererID]?.allowedThemeTemplates) });
  }

  private updateRenderer(rendererID: string, patch: Partial<RendererAppearance>): void {
    if (!knownRenderer(this.portal, rendererID)) return;
    const previous = this.values.renderers[rendererID] ?? { theme: defaultPortalAppearance };
    this.save({
      ...this.values,
      renderers: Object.freeze({
        ...this.values.renderers,
        [rendererID]: Object.freeze({ ...previous, ...patch }),
      }),
    });
  }

  private save(next: StoredPortalAppearance): void {
    this.values = Object.freeze(next);
    try { globalThis.localStorage?.setItem(storageKey(this.portal), JSON.stringify(this.values)); } catch { /* privacy mode */ }
  }
}

export function resolveSystemScheme(mode: PortalAppearanceSettings["mode"]): "light" | "dark" {
  if (mode !== "system") return mode;
  return globalThis.matchMedia?.("(prefers-color-scheme: dark)").matches === true ? "dark" : "light";
}

function readAppearance(portal: PortalSpec): StoredPortalAppearance {
  const fallback: StoredPortalAppearance = Object.freeze({ version: storageVersion, renderers: Object.freeze({}) });
  try {
    const raw = globalThis.localStorage?.getItem(storageKey(portal));
    if (raw === null || raw === undefined || new TextEncoder().encode(raw).byteLength > maxStorageBytes) return fallback;
    const parsed = JSON.parse(raw) as unknown;
    if (!isRecord(parsed) || parsed.version !== storageVersion) return fallback;
    const renderers: Record<string, RendererAppearance> = {};
    if (isRecord(parsed.renderers)) {
      for (const [rendererID, rawOption] of Object.entries(parsed.renderers).slice(0, 16)) {
        if (!knownRenderer(portal, rendererID) || !isRecord(rawOption)) continue;
        renderers[rendererID] = Object.freeze({
          theme: sanitizeTheme(rawOption.theme, portal.renderAdapter.config.rendererOptions?.[rendererID]?.allowedThemeTemplates),
          ...(validAllowedID(rawOption.iconThemeID, portal.renderAdapter.config.rendererOptions?.[rendererID]?.allowedIconThemes) === undefined ? {} : { iconThemeID: rawOption.iconThemeID as string }),
        });
      }
    }
    return Object.freeze({
      version: storageVersion,
      ...(validRendererChoice(portal, parsed.rendererID) === undefined ? {} : { rendererID: parsed.rendererID as string }),
      ...(validRendererChoice(portal, parsed.pendingRendererID) === undefined ? {} : { pendingRendererID: parsed.pendingRendererID as string }),
      ...(validShell(portal, parsed.shellTemplateID) === undefined ? {} : { shellTemplateID: parsed.shellTemplateID as string }),
      renderers: Object.freeze(renderers),
    });
  } catch { return fallback; }
}

function sanitizeTheme(value: unknown, allowed?: readonly string[]): PortalAppearanceSettings {
  if (!isRecord(value)) return defaultPortalAppearance;
  const mode = value.mode === "light" || value.mode === "dark" || value.mode === "system" ? value.mode : defaultPortalAppearance.mode;
  return Object.freeze({
    mode,
    light: sanitizeScheme(value.light, defaultPortalAppearance.light, allowed),
    dark: sanitizeScheme(value.dark, defaultPortalAppearance.dark, allowed),
  });
}

function sanitizeScheme(value: unknown, fallback: PortalAppearanceSettings["light"], allowed?: readonly string[]): PortalAppearanceSettings["light"] {
  if (!isRecord(value)) return fallback;
  const templateID = typeof value.templateID === "string" && idPattern.test(value.templateID) && (allowed === undefined || allowed.includes(value.templateID)) ? value.templateID : fallback.templateID;
  return Object.freeze({ templateID, colors: sanitizeAppearanceColors(value.colors) });
}

function storageKey(portal: PortalSpec): string {
  const account = portal.account;
  return `vastplan.appearance.${encodeURIComponent(account?.tenantID ?? portal.tenantId)}.${encodeURIComponent(account?.subjectID ?? "anonymous")}.${encodeURIComponent(portal.id)}`;
}

function validRendererChoice(portal: PortalSpec, value: unknown): string | undefined {
  return typeof value === "string" && portal.renderAdapter.config.userSelectable && portal.renderAdapter.config.allowedRenderers.includes(value) ? value : undefined;
}

function knownRenderer(portal: PortalSpec, value: string): boolean {
  return value === portal.renderAdapter.config.defaultRenderer || portal.renderAdapter.config.allowedRenderers.includes(value);
}

function validShell(portal: PortalSpec, value: unknown): string | undefined {
  return typeof value === "string" && portal.shell.config.userSelectable && portal.shell.config.allowedTemplates.includes(value) ? value : undefined;
}

function validAllowedID(value: unknown, allowed: readonly string[] | undefined): string | undefined {
  return typeof value === "string" && allowed?.includes(value) === true ? value : undefined;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}
