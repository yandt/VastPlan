import { describe, expect, it } from "vitest";
import { parseHostArguments } from "./host-config";

describe("parseHostArguments", () => {
  it("requires TLS unless local insecure mode is explicit", () => {
    expect(() => parseHostArguments(["--portal-assets", "bin/portal", "--session-file", "sessions.json"], "/srv/vastplan")).toThrow(/TLS/);
    const config = parseHostArguments(["--portal-assets", "bin/portal", "--session-file", "sessions.json", "--allow-insecure-http"], "/srv/vastplan");
    expect(config.portalAssets).toBe("/srv/vastplan/bin/portal");
    expect(config.listenPort).toBe(8443);
  });

  it("rejects unknown and duplicate parameters", () => {
    expect(() => parseHostArguments(["--portal-assets", "a", "--portal-assets", "b", "--session-file", "s", "--allow-insecure-http"])).toThrow(/重复/);
    expect(() => parseHostArguments(["--portal-assets", "a", "--session-file", "s", "--proxy", "http://evil", "--allow-insecure-http"])).toThrow(/未知/);
  });

  it("requires a complete, mTLS-protected Addressing configuration", () => {
    const base = ["--portal-assets", "bin/portal", "--session-file", "sessions.json", "--allow-insecure-http"];
    expect(() => parseHostArguments([...base, "--nats-servers", "nats://127.0.0.1:4222"])).toThrow(/Addressing/);
    expect(() => parseHostArguments([...base, "--nats-servers", "nats://127.0.0.1:4222", "--addressing-contracts", "contracts/proto", "--transport-seed", "portal.seed", "--transport-trust", "trust.json"])).toThrow(/mTLS/);
    const config = parseHostArguments([
      ...base, "--nats-servers", "nats://127.0.0.1:4222,nats://127.0.0.1:4223", "--addressing-contracts", "contracts/proto",
      "--transport-seed", "portal.seed", "--transport-trust", "trust.json", "--allow-insecure-nats", "--composer-logical-service", "platform.portal-composer",
      "--interaction-logical-service", "platform.interaction-broker",
    ], "/srv/vastplan");
    expect(config.addressing).toMatchObject({
      servers: ["nats://127.0.0.1:4222", "nats://127.0.0.1:4223"], contractsDirectory: "/srv/vastplan/contracts/proto",
      seedFile: "/srv/vastplan/portal.seed", allowInsecure: true, composerLogicalService: "platform.portal-composer", interactionLogicalService: "platform.interaction-broker",
    });
  });
});
