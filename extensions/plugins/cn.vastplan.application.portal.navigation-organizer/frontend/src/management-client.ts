import { createBrowserManagementAPIClient, type ManagementAPIClient } from "@vastplan/platform-admin";

export interface NavigationFolder {
  id: string;
  serviceId: string;
  label: string;
  labels?: Readonly<Record<string, string>>;
  icon?: { kind: "semantic"; name: string };
  members: readonly string[];
  order?: number;
}

export interface NavigationConfigurationSnapshot {
  portalId: string;
  serviceId: string;
  activationId: number;
  folders: readonly NavigationFolder[];
}

export interface NavigationConfigurationPreparation {
  candidateId: string;
  portalId: string;
  serviceId: string;
  status: "Preparing" | "Prepared" | "Committed" | "Aborted" | "RolledBack";
  versionId: number;
  previousActivationId: number;
  activationId?: number;
  updatedAt: string;
}

export class NavigationOrganizerClient {
  public constructor(private readonly api: ManagementAPIClient) {}

  public read(): Promise<NavigationConfigurationSnapshot> {
    return this.api.get<NavigationConfigurationSnapshot>("/folders");
  }

  public publish(expectedActivationId: number, folders: readonly NavigationFolder[]): Promise<NavigationConfigurationPreparation> {
    return this.api.mutate<NavigationConfigurationPreparation>("/folders", "PUT", {
      candidateId: globalThis.crypto.randomUUID(), expectedActivationId,
      folders: folders.map(({ serviceId: _serviceId, ...folder }) => folder),
    });
  }
}

export function createNavigationOrganizerClient(portalID: string, serviceID: string): NavigationOrganizerClient {
  return new NavigationOrganizerClient(createBrowserManagementAPIClient(portalID, serviceID, "management-api"));
}
