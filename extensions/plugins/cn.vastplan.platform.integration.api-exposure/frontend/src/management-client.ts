import {
  ManagementAPIClient,
  type APIExposureDraftRequest,
  type APIExposureRevision,
} from "@vastplan/platform-admin";

export interface APIExposureManagementPort {
  listAPIExposures(): Promise<APIExposureRevision[]>;
  createAPIExposureDraft(request: APIExposureDraftRequest): Promise<APIExposureRevision>;
  updateAPIExposureDraft(id: number, expectedRevision: number, request: APIExposureDraftRequest): Promise<APIExposureRevision>;
  submitAPIExposure(id: number): Promise<APIExposureRevision>;
  approveAPIExposure(id: number): Promise<APIExposureRevision>;
  publishAPIExposure(id: number): Promise<APIExposureRevision>;
  retireAPIExposure(exposureID: string): Promise<void>;
}

export class APIExposureManagementClient implements APIExposureManagementPort {
  public constructor(private readonly api: ManagementAPIClient) {}

  public listAPIExposures(): Promise<APIExposureRevision[]> { return this.api.get<{ items: APIExposureRevision[] }>("/exposures").then((value) => value.items); }
  public createAPIExposureDraft(request: APIExposureDraftRequest): Promise<APIExposureRevision> { return this.api.mutate("/exposures", "POST", request); }
  public updateAPIExposureDraft(id: number, expectedRevision: number, request: APIExposureDraftRequest): Promise<APIExposureRevision> { return this.api.mutate(`/exposures/${revision(id)}`, "PUT", { expectedRevision, contract: request.contract, input: request.input }); }
  public submitAPIExposure(id: number): Promise<APIExposureRevision> { return this.transition(id, "submit"); }
  public approveAPIExposure(id: number): Promise<APIExposureRevision> { return this.transition(id, "approve"); }
  public publishAPIExposure(id: number): Promise<APIExposureRevision> { return this.transition(id, "publish"); }
  public retireAPIExposure(exposureID: string): Promise<void> { return this.api.mutate(`/exposures/${segment(exposureID)}/retire`, "POST").then(() => undefined); }

  private transition(id: number, action: "submit" | "approve" | "publish"): Promise<APIExposureRevision> {
    return this.api.mutate(`/exposures/${revision(id)}/${action}`, "POST");
  }
}

function revision(value: number): string {
  if (!Number.isSafeInteger(value) || value < 1) throw new Error("revision id 无效");
  return String(value);
}

function segment(value: string): string {
  if (value.trim() === "" || value.includes("/") || value.includes("\\")) throw new Error("resource id 无效");
  return encodeURIComponent(value);
}
