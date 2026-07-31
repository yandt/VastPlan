export interface PortalPreferenceScope {
  readonly portalId: string;
  readonly workbench: PreferenceCatalogScope;
}

export interface PreferenceCatalogScope { readonly id: string; readonly contractMajor: number }

export interface CollectionPreference {
  readonly columns?: readonly string[];
  readonly hiddenColumns?: readonly string[];
  readonly density?: "compact" | "standard" | "comfortable";
  readonly pageSize?: number;
}

export interface PortalPreferenceValues {
  readonly collections?: Readonly<Record<string, CollectionPreference>>;
}

export interface PortalPreference {
  readonly revision: number;
  readonly scope: PortalPreferenceScope;
  readonly values: PortalPreferenceValues;
  readonly updatedAt?: string;
}
