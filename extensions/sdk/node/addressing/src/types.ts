export interface TransportIdentity {
  name: string;
  role: string;
  publicKey: string;
  tenantId?: string;
  nodeId?: string;
  serviceRoles: readonly string[];
  logicalServices: readonly string[];
  allowedCapabilities: readonly string[];
  allowGlobal: boolean;
  allowDelegation: boolean;
}

export interface TransportTrustDocument {
  version: 1;
  identities: readonly TransportIdentity[];
}

export interface CapabilityAnnouncement {
  schema_version: number;
  capability: string;
  extension_point: string;
  service_role: string;
  logical_service?: string;
  routing_domain?: string;
  partition_key?: string;
  instance_policy?: string;
  state_model?: string;
  visibility?: string;
  routing?: string;
  instance_id: string;
  node_id: string;
  unit_id: string;
  subject: string;
  stream_endpoint?: string;
  version?: string;
  health: string;
  readiness?: string;
  readiness_reason?: string;
  generation?: number;
  fencing_token?: string;
  lease_expires_at?: string;
  updated_at: string;
  transport_public_key?: string;
  transport_timestamp?: string;
  transport_nonce?: string;
  transport_signature?: string;
}

export interface CallTarget {
  extension_point: string;
  capability: string;
  version?: string;
  operation?: string;
  payload_schema?: string;
  logical_service?: string;
  routing_domain?: string;
  partition_key?: string;
  instance_id?: string;
}

export interface Principal {
  user_id: string;
  username?: string;
  is_admin?: boolean;
  tenant_id: string;
  system_roles?: readonly string[];
  project_roles?: Readonly<Record<string, { roles: readonly string[] }>>;
  session_id?: string;
}

export interface CallContext {
  principal?: Principal;
  caller: { kind: number; id: string };
  scene: string;
  tenant_id: string;
  project_id?: string;
  trace?: { trace_id: string; span_id: string; parent_span_id?: string };
  deadline_unix_ms?: number;
  credentials?: readonly { name: string; scope?: string }[];
  idempotency_key?: string;
  metadata?: Readonly<Record<string, string>>;
  call_path?: readonly string[];
}

export interface CallResult {
  status: number;
  error?: { code: string; message: string; retryable: boolean; details?: Readonly<Record<string, string>> };
  warnings?: readonly string[];
  metadata?: Readonly<Record<string, string>>;
}

export interface AddressingResponse {
  result: CallResult;
  payload: Uint8Array;
}

export interface DirectoryQuery {
  capability: string;
  logicalService?: string;
  routingDomain?: string;
  partitionKey?: string;
  instanceId?: string;
}

export interface CapabilityDirectoryPort {
  instances(query: DirectoryQuery): readonly CapabilityAnnouncement[];
}
