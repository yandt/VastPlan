import { describe, expect, it } from "vitest";
import { connectionEndpoint, connectionEndpointFields } from "./connection-endpoint.js";

describe("database connection endpoint form boundary", () => {
  it("splits persisted TCP endpoints and applies Provider defaults for legacy host-only values", () => {
    expect(connectionEndpointFields("db.internal:6432", "postgresql")).toEqual({ host: "db.internal", port: 6432 });
    expect(connectionEndpointFields("mysql.internal", "mysql")).toEqual({ host: "mysql.internal", port: 3306 });
  });

  it("writes bracketed IPv6 endpoints and rejects a host field that already contains a TCP port", () => {
    expect(connectionEndpointFields("[2001:db8::10]:5432", "postgresql")).toEqual({ host: "2001:db8::10", port: 5432 });
    expect(connectionEndpoint("2001:db8::10", 6432)).toBe("[2001:db8::10]:6432");
    expect(connectionEndpoint("db.internal:5432", 5432)).toBeUndefined();
  });
});
