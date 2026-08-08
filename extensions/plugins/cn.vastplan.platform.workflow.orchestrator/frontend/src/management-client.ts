import { createBrowserManagementAPIClient, type ManagementAPIClient } from "@vastplan/platform-admin";

export interface ArtifactOwner { ref: { pluginId: string; version: string; channel: string }; sha256: string; publisher: string; }
export interface WorkflowFeature { id: string; contract: string; resourceKind: string; unboundPolicy?: "deny" | "direct"; }
export interface WorkflowNodeDescriptor { id: string; contract: string; title: string; outcomes: readonly string[]; }
export interface WorkflowCatalog {
  features: readonly { descriptor: WorkflowFeature; owner: ArtifactOwner }[];
  templates: readonly { descriptor: WorkflowNodeDescriptor; owner: ArtifactOwner }[];
  providers: readonly { descriptor: WorkflowNodeDescriptor; owner: ArtifactOwner }[];
}

export interface WorkflowNode {
  id: string;
  type: { id: string; contract: string };
  title?: string;
  roles?: readonly string[];
  outcomes?: Readonly<Record<string, string>>;
  actionId?: string;
  next?: string;
  result?: "succeeded" | "rejected" | "cancelled";
}
export interface WorkflowDefinition { id: string; revision: number; featureId: string; entryNodeId: string; nodes: readonly WorkflowNode[]; }
export interface WorkflowDefinitionRef { id: string; revision: number; digest: string; }
export interface PublishedWorkflowDefinition { definition: WorkflowDefinition; ref: WorkflowDefinitionRef; publishedBy: string; publishedAt: string; }
export interface WorkflowBinding { serviceId: string; featureId: string; definition: WorkflowDefinitionRef; revision: number; updatedAt: string; updatedBy: string; }
export interface WorkflowInstance {
  id: string; serviceId: string; featureId: string; resource: { kind: string; id: string }; mode: "workflow" | "direct";
  status: "running" | "succeeded" | "rejected" | "cancelled" | "suspended"; currentNodeId?: string;
  revision: number; startedBy: string; startedAt: string; updatedAt: string;
}
export interface WorkflowTask {
  id: string; instanceId: string; nodeId: string; title: string; roles?: readonly string[]; allowedOutcomes: readonly string[];
  status: "pending" | "completed"; revision: number; createdAt: string;
}

export class WorkflowManagementClient {
  public constructor(private readonly api: ManagementAPIClient) {}
  public catalog(): Promise<WorkflowCatalog> { return this.api.get("/catalog"); }
  public listDefinitions(): Promise<PublishedWorkflowDefinition[]> { return this.api.get("/definitions"); }
  public publishDefinition(definition: WorkflowDefinition): Promise<WorkflowDefinitionRef> { return this.api.mutate("/definitions", "POST", definition); }
  public listBindings(): Promise<WorkflowBinding[]> { return this.api.get("/bindings"); }
  public bindDefinition(featureId: string, definition: WorkflowDefinitionRef, expectedRevision: number): Promise<WorkflowBinding> { return this.api.mutate("/bindings", "PUT", { featureId, definition, expectedRevision }); }
  public listInstances(): Promise<WorkflowInstance[]> { return this.api.get("/instances"); }
  public listTasks(): Promise<WorkflowTask[]> { return this.api.get("/tasks"); }
  public completeTask(taskId: string, expectedRevision: number, outcome: string, comment?: string): Promise<WorkflowInstance> { return this.api.mutate("/tasks/complete", "POST", { taskId, expectedRevision, outcome, ...(comment === undefined || comment === "" ? {} : { comment }) }); }
  public cancelInstance(instanceId: string, expectedRevision: number, reason: string): Promise<WorkflowInstance> { return this.api.mutate("/instances/cancel", "POST", { instanceId, expectedRevision, reason }); }
}

export function createWorkflowManagementClient(portalID: string, serviceID: string): WorkflowManagementClient {
  return new WorkflowManagementClient(createBrowserManagementAPIClient(portalID, serviceID, "management-api"));
}
