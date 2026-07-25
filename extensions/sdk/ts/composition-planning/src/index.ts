export interface CompositionRef { id: string; revision: number; digest: string; }
export interface ArtifactRef { pluginId: string; version: string; channel: string; }
export interface ArtifactRequirement { pluginId: string; constraint: string; }

export interface ResourceList { cpu_millis?: number; memory_bytes?: number; gpu?: number; }
export interface ResourceRequirements { requests?: ResourceList; }
export interface LabelTerm { match_labels: Record<string, string>; }
export interface WeightedLabelTerm extends LabelTerm { weight: number; }
export interface LabelPolicy { required?: LabelTerm[]; preferred?: WeightedLabelTerm[]; }
export interface Placement {
  nodeSelector?: Record<string, string>;
  affinity?: LabelPolicy;
  antiAffinity?: LabelPolicy;
}
export interface Autoscaling {
  min_replicas: number;
  max_replicas: number;
  metric: string;
  target_value_per_replica: number;
}

export interface RootPluginSelection { ref: ArtifactRef; features?: string[]; }
export interface ServiceOperationsIntent {
  replicas: number;
  autoscaling?: Autoscaling;
  resources?: ResourceRequirements;
  placement?: Placement;
}
export interface BackendServiceIntent {
  id: string;
  serviceClass: string;
  rootPlugins: RootPluginSelection[];
  pluginConfig?: Record<string, Record<string, unknown>>;
  operations: ServiceOperationsIntent;
}
export interface BackendApplicationIntent {
  version: 1;
  revision: number;
  id: string;
  target: { kernel: "backend" };
  metadata: { name: string; tenant?: string };
  services: BackendServiceIntent[];
}

export interface ArtifactLockPackage {
  ref: ArtifactRef;
  sha256: string;
  size: number;
  publisher: string;
  keyId: string;
  repositoryRevision: number;
  dependencies?: Record<string, string>;
  lifecycleStatus?: "deprecated";
  lifecycleReason?: string;
  replacement?: ArtifactRequirement;
}
export interface ArtifactLock {
  schemaVersion: "v1";
  repositoryRevision: number;
  target: "backend" | "frontend" | "runner" | "mobile";
  kernelVersion: string;
  platform?: string;
  roots: ArtifactRequirement[];
  packages: ArtifactLockPackage[];
  digest: string;
}

export type ResolutionStatus = "Resolved" | "NeedsConfiguration" | "Invalid";
export interface ResolvedFeature { unitId: string; pluginId: string; featureId: string; }
export interface CapabilityProviderBinding {
  consumerUnitId: string;
  capability: string;
  providerUnitId: string;
  providerPluginId: string;
  version?: string;
  logicalService?: string;
  routingDomain?: string;
}
export interface ServiceDependencyNode { unitId: string; serviceClass: string; }
export interface ServiceDependencyEdge {
  fromUnitId: string;
  toUnitId: string;
  capability: string;
  kind: "strong" | "soft" | "lazy" | "data";
  failurePolicy: "fail" | "degrade" | "retry";
}
export interface ServiceDependencyGraph { nodes: ServiceDependencyNode[]; edges: ServiceDependencyEdge[]; }
export interface ConfigurationRequirement { kind: "property" | "managed-credential"; field: string; }
export interface ConfigurationPlanItem {
  unitId: string;
  pluginId: string;
  source: "root" | "package-dependency" | "platform-provider" | "foundation";
  editable: boolean;
  schemaDigest: string;
  configurationDigest: string;
  dependencyPath: string[];
  missing?: ConfigurationRequirement[];
}
export interface ConfigurationPlan { items: ConfigurationPlanItem[]; digest: string; }
export interface ResolutionDiagnostic {
  severity: "warning" | "error";
  code: string;
  path?: string[];
  message: string;
}
export interface BackendResolutionReport {
  version: 1;
  intent: CompositionRef;
  platformProfile: CompositionRef;
  planner: { ref: ArtifactRef; capability: "platform.composition.plan" };
  status: ResolutionStatus;
  applicationComposition?: unknown;
  applicationCompositionDigest?: string;
  artifactLock?: ArtifactLock;
  features: ResolvedFeature[];
  providerBindings: CapabilityProviderBinding[];
  serviceGraph: ServiceDependencyGraph;
  configurationPlan: ConfigurationPlan;
  diagnostics: ResolutionDiagnostic[];
  planDigest: string;
}

export function normalizeBackendApplicationIntent(input: BackendApplicationIntent): BackendApplicationIntent {
  if (input.version !== 1 || input.target.kernel !== "backend" || input.revision < 1) {
    throw new Error("invalid Backend Application Intent identity");
  }
  const services = input.services.map(normalizeServiceIntent).sort((left, right) => left.id.localeCompare(right.id));
  rejectDuplicates(services.map((service) => service.id), "service");
  return {
    version: 1,
    revision: input.revision,
    id: input.id,
    target: { kernel: "backend" },
    metadata: { name: input.metadata.name, ...(input.metadata.tenant === undefined ? {} : { tenant: input.metadata.tenant }) },
    services,
  };
}

export async function backendApplicationIntentDigest(input: BackendApplicationIntent): Promise<string> {
  const payload = new TextEncoder().encode(JSON.stringify(normalizeBackendApplicationIntent(input)));
  const digest = await globalThis.crypto.subtle.digest("SHA-256", payload);
  return Array.from(new Uint8Array(digest), (value) => value.toString(16).padStart(2, "0")).join("");
}

function normalizeServiceIntent(input: BackendServiceIntent): BackendServiceIntent {
  const roots = input.rootPlugins.map((selection) => ({
    ref: { pluginId: selection.ref.pluginId, version: selection.ref.version, channel: selection.ref.channel },
    ...(selection.features === undefined ? {} : { features: [...selection.features].sort() }),
  })).sort((left, right) => left.ref.pluginId.localeCompare(right.ref.pluginId));
  rejectDuplicates(roots.map((selection) => selection.ref.pluginId), `root plugin in ${input.id}`);
  for (const selection of roots) rejectDuplicates(selection.features ?? [], `feature in ${selection.ref.pluginId}`);
  return {
    id: input.id,
    serviceClass: input.serviceClass,
    rootPlugins: roots,
    ...(input.pluginConfig === undefined ? {} : { pluginConfig: stableRecord(input.pluginConfig) as Record<string, Record<string, unknown>> }),
    operations: normalizeOperations(input.operations),
  };
}

function normalizeOperations(input: ServiceOperationsIntent): ServiceOperationsIntent {
  if (!Number.isInteger(input.replicas) || input.replicas < 1 || input.replicas > 1024) throw new Error("invalid replicas");
  if (input.autoscaling !== undefined && (input.autoscaling.min_replicas > input.autoscaling.max_replicas || input.replicas < input.autoscaling.min_replicas || input.replicas > input.autoscaling.max_replicas)) {
    throw new Error("replicas must be within autoscaling bounds");
  }
  return {
    replicas: input.replicas,
    ...(input.autoscaling === undefined ? {} : { autoscaling: {
      min_replicas: input.autoscaling.min_replicas,
      max_replicas: input.autoscaling.max_replicas,
      metric: input.autoscaling.metric,
      target_value_per_replica: input.autoscaling.target_value_per_replica,
    } }),
    ...(input.resources === undefined ? {} : { resources: normalizeResources(input.resources) }),
    ...(input.placement === undefined ? {} : { placement: normalizePlacement(input.placement) }),
  };
}

function normalizeResources(input: ResourceRequirements): ResourceRequirements {
  if (input.requests === undefined) return {};
  return { requests: {
    ...(input.requests.cpu_millis === undefined ? {} : { cpu_millis: input.requests.cpu_millis }),
    ...(input.requests.memory_bytes === undefined ? {} : { memory_bytes: input.requests.memory_bytes }),
    ...(input.requests.gpu === undefined ? {} : { gpu: input.requests.gpu }),
  } };
}

function normalizePlacement(input: Placement): Placement {
  return {
    ...(input.nodeSelector === undefined ? {} : { nodeSelector: stableStringRecord(input.nodeSelector) }),
    ...(input.affinity === undefined ? {} : { affinity: normalizeLabelPolicy(input.affinity) }),
    ...(input.antiAffinity === undefined ? {} : { antiAffinity: normalizeLabelPolicy(input.antiAffinity) }),
  };
}

function normalizeLabelPolicy(input: LabelPolicy): LabelPolicy {
  return {
    ...(input.required === undefined ? {} : { required: input.required.map((term) => ({ match_labels: stableStringRecord(term.match_labels) })) }),
    ...(input.preferred === undefined ? {} : { preferred: input.preferred.map((term) => ({ match_labels: stableStringRecord(term.match_labels), weight: term.weight })) }),
  };
}

function stableStringRecord(input: Record<string, string>): Record<string, string> {
  return Object.fromEntries(Object.keys(input).sort().map((key) => [key, input[key]!])) as Record<string, string>;
}

function stableRecord(input: Record<string, unknown>): Record<string, unknown> {
  return Object.fromEntries(Object.keys(input).sort().map((key) => [key, stableValue(input[key])])) as Record<string, unknown>;
}

function stableValue(input: unknown): unknown {
  if (Array.isArray(input)) return input.map(stableValue);
  if (input !== null && typeof input === "object") return stableRecord(input as Record<string, unknown>);
  return input;
}

function rejectDuplicates(values: string[], noun: string): void {
  if (new Set(values).size !== values.length) throw new Error(`duplicate ${noun}`);
}
