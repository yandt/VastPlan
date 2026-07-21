import { describe, expect, it } from "vitest";
import { portalSpecDigest, type PortalSpec } from "./portal-runtime-contract";

describe("Portal Runtime contract", () => {
  it("matches the Go PortalSpec JSON digest vector", () => {
    const spec = {
      revision: 7,
      id: "operations",
      tenantId: "tenant-a",
      route: "/operations",
      localization: { defaultLocale: "", supportedLocales: null },
      updates: { mode: "" },
      runtimeEngine: { id: "", version: "", engineContract: "", family: "" },
      renderAdapter: {
        id: "", version: "", uiContract: "",
        config: { defaultRenderer: "", allowedRenderers: null, userSelectable: false },
      },
      shell: {
        id: "", version: "", uiContract: "",
        config: { defaultTemplate: "", allowedTemplates: null, userSelectable: false },
      },
      workbench: { id: "", version: "", uiContract: "" },
      plugins: null,
      management: {
        tenantId: "", portalId: "",
        platformProfile: { id: "", revision: 0, digest: "" }, services: null,
      },
      resolution: {
        platformCatalog: { id: "", revision: 0, digest: "" },
        platformProfile: { id: "", revision: 0, digest: "" },
        applicationComposition: { id: "", revision: 0, digest: "" },
        managementBindingDigest: "", pluginOrigins: null,
      },
    } as unknown as PortalSpec;
    expect(portalSpecDigest(spec)).toBe("802548f3a1bc52573631c762d2780ffcd54e43588caa40e9321f2be45480807c");
  });
});
