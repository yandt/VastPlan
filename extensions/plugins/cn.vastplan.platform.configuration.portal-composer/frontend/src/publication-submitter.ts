import type { PortalControlClient } from "@vastplan/ui-primitives";
import { createBrowserManagementAPIClient, type ManagementAPIClient } from "@vastplan/platform-admin";

export interface PortalPublicationSubmitter {
  submit(portalId: string, expectedWorkingRevision: number): Promise<unknown>;
}

export function directPublicationSubmitter(client: PortalControlClient): PortalPublicationSubmitter {
  return { submit: (portalId, expectedWorkingRevision) => client.submitPortalPublication(portalId, expectedWorkingRevision) };
}

export class GovernedPublicationSubmitter implements PortalPublicationSubmitter {
  public constructor(private readonly api: ManagementAPIClient) {}

  public submit(portalId: string, expectedWorkingRevision: number): Promise<unknown> {
    return this.api.mutate("/govern", "POST", {
      featureId: "platform.portal.publication",
      preparePayload: { portalId, publication: { expectedWorkingRevision } },
      idempotencyKey: `portal-publication:${portalId}:${expectedWorkingRevision}`,
    });
  }
}

export function governedPublicationSubmitter(portalId: string, serviceId: string): PortalPublicationSubmitter {
  return new GovernedPublicationSubmitter(createBrowserManagementAPIClient(portalId, serviceId, "management-api"));
}
