import { describe, expect, it, vi } from "vitest";
import { ManagementAPIClient } from "@vastplan/platform-admin";
import { GovernedPublicationSubmitter } from "./publication-submitter";

describe("GovernedPublicationSubmitter", () => {
  it("submits only feature input while the gateway owns service identity", async () => {
    const fetch = vi.fn()
      .mockResolvedValueOnce(reply({ token: "csrf-token" }))
      .mockResolvedValueOnce(reply({ resource: {}, instance: {} }));
    const submitter = new GovernedPublicationSubmitter(new ManagementAPIClient(fetch, "operations", "workflow-service", "management-api"));

    await submitter.submit("customer-portal", 7);

    expect(fetch).toHaveBeenNthCalledWith(2, "/v1/portals/operations/platform/services/workflow-service/api/management-api/govern", {
      method: "POST",
      credentials: "include",
      headers: { "Content-Type": "application/json", "X-VastPlan-CSRF": "csrf-token" },
      body: JSON.stringify({
        featureId: "platform.portal.publication",
        preparePayload: { portalId: "customer-portal", publication: { expectedWorkingRevision: 7 } },
        idempotencyKey: "portal-publication:customer-portal:7",
      }),
    });
  });
});

function reply(body: unknown) {
  return { ok: true, status: 200, async json() { return body; } };
}
