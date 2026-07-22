export interface PreferenceCatalogScope { readonly id: string; readonly contractMajor: number }

export interface PortalPreferenceScope {
  readonly portalId: string;
  readonly renderer: PreferenceCatalogScope;
  readonly shell: PreferenceCatalogScope;
  readonly workbench: PreferenceCatalogScope;
}

export interface RendererPreference {
  readonly themeTemplateId?: string;
  readonly iconThemeId?: string;
}

export interface CollectionPreference {
  readonly columns?: readonly string[];
  readonly hiddenColumns?: readonly string[];
  readonly density?: "compact" | "standard" | "comfortable";
  readonly pageSize?: number;
}

export interface PortalPreferenceValues {
  readonly rendererId?: string;
  readonly rendererOptions?: Readonly<Record<string, RendererPreference>>;
  readonly shellTemplateId?: string;
  readonly collections?: Readonly<Record<string, CollectionPreference>>;
}

export interface PortalPreference {
  readonly revision: number;
  readonly scope: PortalPreferenceScope;
  readonly values: PortalPreferenceValues;
  readonly updatedAt?: string;
}
