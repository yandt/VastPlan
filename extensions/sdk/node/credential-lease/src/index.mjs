import {
  createDecipheriv,
  createHmac,
  createPublicKey,
  diffieHellman,
  generateKeyPairSync,
} from "node:crypto";

const info = Buffer.from("vastplan/material-lease/v1");
const x25519Prefix = Buffer.from("302a300506032b656e032100", "hex");
const raw = (value) => Buffer.from(value, "base64url");
const validRef = (ref) =>
  ref &&
  String(ref.handle).startsWith("credential://managed/") &&
  ref.scope === "tenant" &&
  ref.owner &&
  ref.purpose &&
  Number.isSafeInteger(ref.version) &&
  ref.version > 0;

export class MaterialLeaseClient {
  constructor(plugin, { audience, now = () => Date.now() }) {
    if (!plugin || !audience) throw new Error("Material Lease client 身份无效");
    this.plugin = plugin;
    this.audience = audience;
    this.now = now;
  }

  async withMaterial(ref, tenantId, invocationContext, use) {
    if (!validRef(ref) || !tenantId || typeof use !== "function")
      throw new Error("Managed CredentialRef 无效");
    const { publicKey, privateKey } = generateKeyPairSync("x25519");
    const publicDer = publicKey.export({ format: "der", type: "spki" });
    const request = {
      ref,
      recipientPublicKey: publicDer
        .subarray(publicDer.length - 32)
        .toString("base64url"),
    };
    const response = await this.plugin.call(
      {
        extension_point: "kernel.service",
        capability: "kernel.credential.material-lease",
        operation: "issue",
      },
      { tenant_id: tenantId },
      Buffer.from(JSON.stringify(request)),
    );
    if (response?.result?.status !== "STATUS_OK")
      throw new Error(
        response?.result?.error?.message ?? "Material Lease 被拒绝",
      );
    const envelope = JSON.parse(
      Buffer.from(response.payload ?? []).toString("utf8"),
    );
    const expected = JSON.stringify(ref);
    if (
      envelope.version !== 1 ||
      envelope.tenantId !== tenantId ||
      envelope.audience !== this.audience ||
      JSON.stringify(envelope.ref) !== expected
    )
      throw new Error("Material Lease claims 不匹配");
    const now = this.now();
    if (
      envelope.expiresAtUnixMs <= envelope.issuedAtUnixMs ||
      envelope.expiresAtUnixMs - envelope.issuedAtUnixMs > 30_000 ||
      now < envelope.issuedAtUnixMs - 5_000 ||
      now >= envelope.expiresAtUnixMs
    )
      throw new Error("Material Lease 已过期");
    const senderRaw = raw(envelope.senderPublicKey);
    if (senderRaw.length !== 32)
      throw new Error("Material Lease sender key 无效");
    const sender = createPublicKey({
      key: Buffer.concat([x25519Prefix, senderRaw]),
      format: "der",
      type: "spki",
    });
    const shared = diffieHellman({ privateKey, publicKey: sender });
    const salt = raw(envelope.salt);
    const extract = createHmac("sha256", salt).update(shared).digest();
    const key = createHmac("sha256", extract)
      .update(info)
      .update(Buffer.from([1]))
      .digest();
    const nonce = raw(envelope.nonce);
    const ciphertext = raw(envelope.ciphertext);
    if (nonce.length !== 12 || ciphertext.length < 17)
      throw new Error("Material Lease ciphertext 无效");
    const aad = Buffer.from(
      JSON.stringify({
        version: envelope.version,
        leaseId: envelope.leaseId,
        tenantId: envelope.tenantId,
        audience: envelope.audience,
        ref: envelope.ref,
        issuedAtUnixMs: envelope.issuedAtUnixMs,
        expiresAtUnixMs: envelope.expiresAtUnixMs,
      }),
    );
    const tag = ciphertext.subarray(ciphertext.length - 16);
    const encrypted = ciphertext.subarray(0, ciphertext.length - 16);
    const decipher = createDecipheriv("aes-256-gcm", key, nonce);
    decipher.setAAD(aad);
    decipher.setAuthTag(tag);
    const material = Buffer.concat([
      decipher.update(encrypted),
      decipher.final(),
    ]);
    shared.fill(0);
    salt.fill(0);
    extract.fill(0);
    key.fill(0);
    ciphertext.fill(0);
    try {
      return await use(material, invocationContext);
    } finally {
      material.fill(0);
    }
  }
}
