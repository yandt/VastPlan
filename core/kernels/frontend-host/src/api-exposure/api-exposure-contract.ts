export interface APIContractContribution {
  readonly id: string;
  readonly service_role: "backend" | "workspace" | "rs";
  readonly title?: string;
  readonly contractId: string;
  readonly contractVersion: string;
  readonly protocol: "http-json";
  readonly routes: readonly APIRouteContract[];
}

export interface APIRouteContract {
  readonly id: string;
  readonly method: "GET" | "POST" | "PUT" | "PATCH" | "DELETE";
  readonly path: string;
  readonly target: { readonly capability: string; readonly operation: string };
  readonly requestSchema: Readonly<Record<string, unknown>>;
  readonly responseSchema: Readonly<Record<string, unknown>>;
  readonly successStatus: 200 | 201 | 202 | 204;
  readonly errors?: readonly { readonly code: string; readonly status: number }[];
}

export interface APIExposure {
  readonly schemaVersion: "v1";
  readonly id: string;
  readonly revision: number;
  readonly routeKey: string;
  readonly displayName: string;
  readonly tenantId: string;
  readonly portalId?: string;
  readonly hosts: readonly string[];
  readonly contract: {
    readonly pluginId: string;
    readonly artifactSha256: string;
    readonly contributionId: string;
    readonly contractId: string;
    readonly contractVersion: string;
    readonly contractDigest: string;
  };
  readonly authentication: { readonly profileId: string; readonly allowAnonymous: boolean };
  readonly requiredPermissions: readonly string[];
  readonly limits: { readonly maxBodyBytes: number; readonly maxResponseBytes: number; readonly requestsPerMinute: number; readonly timeoutMs: number };
  readonly target: { readonly logicalService: string; readonly routingDomain: string };
}

export interface ResolvedAPIExposure {
  readonly exposure: APIExposure;
  readonly contract: APIContractContribution;
}

export interface APIExposureCatalog {
  readonly schemaVersion: "v1";
  readonly generation: number;
  readonly exposures: readonly ResolvedAPIExposure[];
}

export interface APIExposureCatalogPort {
  resolve(host: string, routeKey: string, majorVersion: number): Promise<ResolvedAPIExposure | undefined>;
}

export interface GatewayInvocation {
  readonly schemaVersion: "v1";
  readonly routeId: string;
  readonly method: APIRouteContract["method"];
  readonly pathParams: Readonly<Record<string, string>>;
  readonly query: Readonly<Record<string, readonly string[]>>;
  readonly body: unknown;
}
