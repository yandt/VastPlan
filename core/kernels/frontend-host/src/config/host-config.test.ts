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
});
