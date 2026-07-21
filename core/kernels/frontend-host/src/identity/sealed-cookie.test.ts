import { randomBytes } from "node:crypto";
import { mkdtemp, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { describe, expect, it } from "vitest";
import { SealedCookieCodec } from "./sealed-cookie";

describe("SealedCookieCodec", () => {
  it("encrypts authenticated, expiring cluster-portable session state", async () => {
    const root = await mkdtemp(join(tmpdir(), "vastplan-oidc-cookie-"));
    const key = join(root, "session.key");
    await writeFile(key, randomBytes(32), { mode: 0o600 });
    const codec = await SealedCookieCodec.open(key, "issuer\0client", () => 1_000_000);
    const token = codec.seal({ kind: "session", exp: 1001, sub: "alice" });
    expect(token).not.toContain("alice");
    expect(codec.unseal(token)).toMatchObject({ sub: "alice" });
    expect(() => codec.unseal(`${token.slice(0, -1)}x`)).toThrow(/无效/);
    const expired = codec.seal({ kind: "session", exp: 999, sub: "alice" });
    expect(() => codec.unseal(expired)).toThrow(/过期/);
  });
});
