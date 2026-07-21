import { createHash } from "node:crypto";
import { chmod, writeFile } from "node:fs/promises";

export async function writeSessionFixture(path: string, token: string, expiresAt: Date, roles: readonly string[] = ["portal.compose"]): Promise<void> {
  const tokenSHA256 = createHash("sha256").update(token).digest("hex");
  await writeFile(path, JSON.stringify({ sessions: [{ tokenSHA256, id: "alice", tenantId: "tenant-a", roles, expiresAt: expiresAt.toISOString() }] }));
  await chmod(path, 0o600);
}
