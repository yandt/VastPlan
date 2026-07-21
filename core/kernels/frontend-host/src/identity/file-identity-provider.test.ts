import { chmod } from "node:fs/promises";
import { join } from "node:path";
import { describe, expect, it } from "vitest";
import { createPortalFixture } from "../testing/portal-fixture";
import { writeSessionFixture } from "../testing/session-fixture";
import { FileIdentityProvider } from "./file-identity-provider";

describe("FileIdentityProvider", () => {
  it("authenticates digest-only sessions and rereads revocation state", async () => {
    const path = join(await createPortalFixture(), "sessions.json");
    const now = new Date("2026-07-21T12:00:00Z");
    await writeSessionFixture(path, "opaque-token", new Date(now.getTime() + 60_000));
    const provider = await FileIdentityProvider.open(path, () => now);
    const principal = await provider.authenticate(requestWithCookie("vastplan_session=opaque-token"));
    expect(principal).toEqual({ id: "alice", tenantId: "tenant-a", roles: ["portal.compose"] });

    await writeSessionFixture(path, "opaque-token", new Date(now.getTime() - 1));
    await expect(provider.authenticate(requestWithCookie("vastplan_session=opaque-token"))).rejects.toThrow(/无效或已过期/);
  });

  it("rejects duplicate cookies and group-readable session files", async () => {
    const path = join(await createPortalFixture(), "sessions.json");
    await writeSessionFixture(path, "token", new Date(Date.now() + 60_000));
    const provider = await FileIdentityProvider.open(path);
    await expect(provider.authenticate(requestWithCookie("vastplan_session=token; vastplan_session=other"))).rejects.toThrow(/无效或已过期/);
    await chmod(path, 0o640);
    await expect(provider.authenticate(requestWithCookie("vastplan_session=token"))).rejects.toThrow(/仅属主/);
  });
});

function requestWithCookie(cookie: string): import("node:http").IncomingMessage {
  return { headers: { cookie } } as import("node:http").IncomingMessage;
}
