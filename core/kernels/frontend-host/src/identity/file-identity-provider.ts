import { createHash, timingSafeEqual } from "node:crypto";
import { lstat, readFile } from "node:fs/promises";
import type { IncomingMessage } from "node:http";
import { onlyCookie } from "../http/cookies";
import type { IdentityProvider, Principal } from "./identity-provider";
import { SessionRejectedError } from "./identity-provider";

const sessionCookieName = "vastplan_session";

interface SessionRecord {
  tokenSHA256: string;
  id: string;
  tenantId: string;
  roles: readonly string[];
  expiresAt: string;
}

export class FileIdentityProvider implements IdentityProvider {
  private constructor(private readonly path: string, private readonly now: () => Date) {}

  public static async open(path: string, now: () => Date = () => new Date()): Promise<FileIdentityProvider> {
    const provider = new FileIdentityProvider(path, now);
    await provider.readSessions();
    return provider;
  }

  public async authenticate(request: IncomingMessage): Promise<Principal> {
    const token = onlyCookie(request, sessionCookieName);
    if (token === undefined) throw new SessionRejectedError();
    const actual = createHash("sha256").update(token).digest();
    const sessions = await this.readSessions();
    for (const record of sessions) {
      const expected = Buffer.from(record.tokenSHA256, "hex");
      if (expected.byteLength !== actual.byteLength || !timingSafeEqual(actual, expected)) continue;
      const expiresAt = Date.parse(record.expiresAt);
      if (!Number.isFinite(expiresAt) || expiresAt <= this.now().getTime()) throw new SessionRejectedError();
      return Object.freeze({ id: record.id, tenantId: record.tenantId, roles: Object.freeze([...record.roles]) });
    }
    throw new SessionRejectedError();
  }

  private async readSessions(): Promise<readonly SessionRecord[]> {
    const file = await lstat(this.path);
    if (!file.isFile() || file.isSymbolicLink() || (file.mode & 0o077) !== 0) {
      throw new Error("Portal session 文件必须是仅属主可读写的普通文件");
    }
    let value: unknown;
    try {
      value = JSON.parse(await readFile(this.path, "utf8"));
    } catch {
      throw new Error("Portal session 文件格式无效");
    }
    if (!isRecord(value) || !hasOnlyKeys(value, ["sessions"]) || !Array.isArray(value.sessions)) throw new Error("Portal session 文件格式无效");
    return value.sessions.map(parseSessionRecord);
  }
}

function parseSessionRecord(value: unknown): SessionRecord {
  const keys = ["tokenSHA256", "id", "tenantId", "roles", "expiresAt"];
  if (!isRecord(value) || !hasOnlyKeys(value, keys) || typeof value.tokenSHA256 !== "string" || !/^[a-f0-9]{64}$/.test(value.tokenSHA256) ||
      typeof value.id !== "string" || value.id === "" || typeof value.tenantId !== "string" || value.tenantId === "" ||
      !Array.isArray(value.roles) || value.roles.some((role) => typeof role !== "string" || role === "") || new Set(value.roles).size !== value.roles.length ||
      typeof value.expiresAt !== "string") {
    throw new Error("Portal session 文件格式无效");
  }
  return { tokenSHA256: value.tokenSHA256, id: value.id, tenantId: value.tenantId, roles: Object.freeze([...value.roles]), expiresAt: value.expiresAt };
}

function hasOnlyKeys(value: Record<string, unknown>, keys: readonly string[]): boolean {
  return Object.keys(value).length === keys.length && keys.every((key) => Object.hasOwn(value, key));
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}
