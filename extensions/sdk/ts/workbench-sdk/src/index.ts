import type { ActionSpec, CollectionDensity, CollectionSpec, LocalizedText } from "@vastplan/ui-contract";

export type { ActionSpec, CollectionSpec, ColumnSpec, FilterSpec, CollectionFilterKind, CollectionQueryMode, CollectionSelectionMode, CollectionView } from "@vastplan/ui-contract";

export interface CollectionQuery {
  page: number;
  pageSize: number;
  filters: Readonly<Record<string, unknown>>;
  sort?: { key: string; direction: "asc" | "desc" };
}

export interface CollectionResult<Row extends Record<string, unknown> = Record<string, unknown>> {
  items: readonly Row[];
  total: number;
}

export interface CollectionActionContext<Row extends Record<string, unknown> = Record<string, unknown>> {
  action: ActionSpec;
  selected: readonly Row[];
  refresh(): void;
}

export interface CollectionSummaryMetric {
  id: string;
  label: LocalizedText;
  value: string | number;
  tone?: "neutral" | "info" | "success" | "warning" | "error";
}

export interface CollectionSummary {
  title?: LocalizedText;
  metrics: readonly CollectionSummaryMetric[];
}

/** Platform Profile policy for the collection presentation family. */
export interface WorkbenchPresentationConfig {
  collection?: { defaultDensity?: CollectionDensity; allowedDensities?: readonly CollectionDensity[] };
}

export interface CollectionPageDefinition<Row extends Record<string, unknown> = Record<string, unknown>> {
  id: string;
  path: string;
  title: LocalizedText;
  description?: LocalizedText;
  navigation?: { id: string; label: LocalizedText; zone: "primary" | "settings" | "secondary"; groupID?: string; order?: number };
  collection: CollectionSpec;
  load(query: CollectionQuery, signal: AbortSignal): Promise<CollectionResult<Row>>;
  loadSummary?(signal: AbortSignal): Promise<CollectionSummary>;
  runAction?(context: CollectionActionContext<Row>, signal: AbortSignal): Promise<void>;
}

/** The only registration surface a functional Collection page receives. */
export interface WorkbenchPluginContext {
  addCollectionPage<Row extends Record<string, unknown>>(page: CollectionPageDefinition<Row>): void;
}

/** Makes page definitions discoverable and prevents a future arbitrary component escape hatch. */
export function defineCollectionPage<Row extends Record<string, unknown>>(definition: CollectionPageDefinition<Row>): CollectionPageDefinition<Row> {
  return Object.freeze({ ...definition, collection: Object.freeze({ ...definition.collection }) });
}
