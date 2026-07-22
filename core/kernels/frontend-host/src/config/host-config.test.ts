import { describe, expect, it } from "vitest";
import { parseHostArguments } from "./host-config";

describe("parseHostArguments", () => {
  it("requires TLS unless local insecure mode is explicit", () => {
    expect(() => parseHostArguments(["--portal-assets", "bin/portal", "--session-file", "sessions.json"], "/srv/vastplan")).toThrow(/TLS/);
    const config = parseHostArguments(["--portal-assets", "bin/portal", "--session-file", "sessions.json", "--allow-insecure-http"], "/srv/vastplan");
    expect(config.portalAssets).toBe("/srv/vastplan/bin/portal");
    expect(config.listenPort).toBe(8443);
    expect(config.identity).toEqual({ kind: "file", sessionFile: "/srv/vastplan/sessions.json" });
  });

  it("accepts an explicit pre-session Access Profile Catalog", () => {
    const config = parseHostArguments([
      "--portal-assets", "bin/portal", "--session-file", "sessions.json", "--allow-insecure-http",
      "--access-profile-catalog", "config/access-profiles.json",
    ], "/srv/vastplan");
    expect(config.accessProfileCatalog).toBe("/srv/vastplan/config/access-profiles.json");
  });

  it("requires Broker trust, session protection and pluggable service routing", () => {
    const base = ["--portal-assets", "bin/portal", "--tls-cert", "portal.crt", "--tls-key", "portal.key", "--identity-provider", "broker"];
    expect(() => parseHostArguments(base)).toThrow(/assertion-trust/i);
    const config = parseHostArguments([
      ...base, "--authentication-assertion-trust-file", "assertion-trust.json", "--portal-session-key-file", "session.key",
      "--portal-session-max-age", "600", "--authentication-broker-logical-service", "identity-broker",
      "--authorization-session-logical-service", "authorization-session",
    ], "/srv/vastplan");
    expect(config.identity).toEqual({ kind: "broker", assertionTrustFile: "/srv/vastplan/assertion-trust.json", sessionKeyFile: "/srv/vastplan/session.key", sessionMaxAgeSeconds: 600, brokerLogicalService: "identity-broker", authorizationLogicalService: "authorization-session" });
    expect(() => parseHostArguments([...base, "--authentication-assertion-trust-file", "trust", "--portal-session-key-file", "key", "--oidc-issuer", "https://id.example.com"])).toThrow(/未知/);
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

  it("enables immutable frontend delivery only with an explicit local cache", () => {
    const base = ["--portal-assets", "bin/portal", "--session-file", "sessions.json", "--allow-insecure-http"];
    expect(() => parseHostArguments([...base, "--frontend-delivery-origin", "origin"], "/srv/vastplan")).toThrow(/cache/);
    const config = parseHostArguments([...base, "--frontend-delivery-cache", "cache", "--frontend-delivery-origin", "origin"], "/srv/vastplan");
    expect(config.delivery).toEqual({ cacheRoot: "/srv/vastplan/cache", originRoot: "/srv/vastplan/origin" });
  });
});
