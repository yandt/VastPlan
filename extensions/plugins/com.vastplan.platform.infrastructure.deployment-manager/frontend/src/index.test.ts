import { describe, expect, it } from "vitest";
import { buildBackendComposition, serviceCompositionSchema } from "./index";

describe("deployment-manager frontend contract", () => {
  it("exposes only application composition fields and preserves replicas", () => {
    const schema = serviceCompositionSchema([{ deploymentName: "agent-services", platformProfile: { id: "baseline", revision: 1, digest: "a".repeat(64) } }]);
    expect(schema.schema.properties).not.toHaveProperty("platformProfile");
    const composition = buildBackendComposition({ deployment: "agent-services", units: [{ serviceClass: "application.backend", id: "api", plugins: [{ id: "com.example.agent", version: "1.0.0" }], replicas: 3, dependsOn: ["database"] }] });
    expect(composition.metadata).toEqual({ name: "agent-services" });
    expect(composition.units[0]?.spec.replicas).toBe(3);
    expect(composition.units[0]?.spec.depends_on).toEqual(["database"]);
  });
});
