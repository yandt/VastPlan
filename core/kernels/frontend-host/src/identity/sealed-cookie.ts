import { createCipheriv, createDecipheriv, randomBytes } from "node:crypto";
import { lstat, readFile } from "node:fs/promises";

export class SealedCookieCodec {
  private constructor(private readonly key: Buffer, private readonly audience: string, private readonly now: () => number) {}

  public static async open(keyFile: string, audience: string, now: () => number = Date.now): Promise<SealedCookieCodec> {
    const info = await lstat(keyFile);
    if (!info.isFile() || info.isSymbolicLink() || (info.mode & 0o077) !== 0) throw new Error("Portal session key 必须是仅属主可读写的普通文件");
    const raw = await readFile(keyFile);
    const key = /^[a-f0-9]{64}\s*$/i.test(raw.toString("utf8")) ? Buffer.from(raw.toString("utf8").trim(), "hex") : raw;
    if (key.byteLength !== 32) throw new Error("Portal session key 必须是 32 字节或 64 位十六进制");
    return new SealedCookieCodec(Buffer.from(key), audience, now);
  }

  public seal(value: Readonly<Record<string, unknown>>): string {
    const nonce = randomBytes(12);
    const cipher = createCipheriv("aes-256-gcm", this.key, nonce);
    cipher.setAAD(Buffer.from(this.audience));
    const plaintext = Buffer.from(JSON.stringify(value));
    if (plaintext.byteLength > 4096) throw new Error("Portal session 内容超过上限");
    const ciphertext = Buffer.concat([cipher.update(plaintext), cipher.final()]);
    return `v1.${nonce.toString("base64url")}.${ciphertext.toString("base64url")}.${cipher.getAuthTag().toString("base64url")}`;
  }

  public unseal(value: string): Readonly<Record<string, unknown>> {
    if (value.length > 8192) throw new Error("Portal session cookie 超过上限");
    const [version, nonceText, ciphertextText, tagText, extra] = value.split(".");
    if (version !== "v1" || extra !== undefined) throw new Error("Portal session cookie 格式无效");
    const nonce = decodeCanonicalBase64URL(nonceText);
    const ciphertext = decodeCanonicalBase64URL(ciphertextText);
    const tag = decodeCanonicalBase64URL(tagText);
    if (nonce.byteLength !== 12 || tag.byteLength !== 16 || ciphertext.byteLength === 0 || ciphertext.byteLength > 4096) throw new Error("Portal session cookie 格式无效");
    const decipher = createDecipheriv("aes-256-gcm", this.key, nonce);
    decipher.setAAD(Buffer.from(this.audience));
    decipher.setAuthTag(tag);
    let parsed: unknown;
    try { parsed = JSON.parse(Buffer.concat([decipher.update(ciphertext), decipher.final()]).toString("utf8")); }
    catch { throw new Error("Portal session cookie 无效"); }
    if (typeof parsed !== "object" || parsed === null || Array.isArray(parsed)) throw new Error("Portal session cookie 内容无效");
    const record = parsed as Readonly<Record<string, unknown>>;
    if (!Number.isSafeInteger(record.exp) || (record.exp as number) <= Math.floor(this.now() / 1000)) throw new Error("Portal session 已过期");
    return record;
  }
}

function decodeCanonicalBase64URL(value: string | undefined): Buffer {
  if (value === undefined || value === "" || !/^[A-Za-z0-9_-]+$/.test(value)) throw new Error("Portal session cookie 格式无效");
  const decoded = Buffer.from(value, "base64url");
  if (decoded.toString("base64url") !== value) throw new Error("Portal session cookie 编码不规范");
  return decoded;
}
