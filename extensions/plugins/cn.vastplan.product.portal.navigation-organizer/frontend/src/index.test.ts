import { describe, expect, it, vi } from "vitest";
import { message } from "@vastplan/workbench-sdk";
import plugin from "./index.js";
import { createNavigationFolderPage } from "./page.js";
import type { NavigationOrganizerClient } from "./management-client.js";

describe("Portal navigation organizer UI", () => {
  it("registers the owner default only when no replacement provider exists", () => {
    const pages: unknown[] = [];
    const context = {
      portal: {
        id: "operations", tenantId: "tenant-a", revision: 7, route: "/",
        experience: { permissions: ["portal.navigation.read", "portal.navigation.publish"] },
        management: { services: [{ id: "service-a", logicalService: "service-a", routingDomain: "service", capabilities: [{ capability: "portal.navigation", read: ["apiRead"], write: ["apiPublish"] }] }] },
      },
      lifecycle: { pluginID: "cn.vastplan.product.portal.navigation-organizer", generation: "7", signal: new AbortController().signal, reason: "bootstrap" },
      i18n: { message: (_key: string, fallback: string) => fallback },
      extensions: { owns: () => true, contributes: () => false, list: () => [] },
      addWorkspacePage: (page: unknown) => pages.push(page),
    };
    plugin.register(context as never);
    expect(pages).toHaveLength(1);

    pages.length = 0;
    plugin.register({ ...context, extensions: { ...context.extensions, list: () => [{ point: "cn.vastplan.product.portal.navigation-organizer.ui-provider", id: "cn.example.replacement.page", pluginId: "cn.example.replacement", contract: "^1.0.0", descriptor: { pageId: "replacement", groupId: "navigation" } }] } } as never);
    expect(pages).toHaveLength(0);
  });

  it("publishes a full service-scoped folder snapshot through Activation CAS", async () => {
    const read = vi.fn().mockResolvedValue({ portalId: "operations", serviceId: "service-a", activationId: 7, folders: [] });
    const publish = vi.fn().mockResolvedValue({ status: "Committed", activationId: 8 });
    const page = createNavigationFolderPage({ read, publish } as unknown as NavigationOrganizerClient, "service-a", message("test", "service", "Service A"));
    const create = page.forms?.find((form) => form.id === "create");
    await create?.submit({
      value: { id: "operations", label: "Operations", iconName: "", members: ["cn.example.a/root", "cn.example.b/root"] },
      selected: [],
    }, new AbortController().signal);
    expect(publish).toHaveBeenCalledWith(7, [{
      id: "operations", serviceId: "service-a", label: "Operations",
      members: ["cn.example.a/root", "cn.example.b/root"],
    }]);
  });
});
