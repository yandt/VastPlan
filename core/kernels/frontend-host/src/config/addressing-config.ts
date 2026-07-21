import { isAbsolute, resolve } from "node:path";
import type { NodeAddressingConfig } from "@vastplan/addressing-node";

export interface PortalAddressingConfig extends NodeAddressingConfig {
  composerLogicalService?: string;
}

export const addressingValueArguments = Object.freeze([
  "--nats-servers", "--addressing-contracts", "--transport-seed", "--transport-trust",
  "--nats-tls-ca", "--nats-tls-cert", "--nats-tls-key", "--composer-logical-service",
]);

export function parseAddressingConfig(values: ReadonlyMap<string, string>, allowInsecure: boolean, cwd: string): PortalAddressingConfig | undefined {
  const configured = addressingValueArguments.some((name) => values.has(name)) || allowInsecure;
  if (!configured) return undefined;
  const servers = (values.get("--nats-servers") ?? "").split(",").map((value) => value.trim()).filter(Boolean);
  const contractsDirectory = values.get("--addressing-contracts");
  const seedFile = values.get("--transport-seed");
  const trustFile = values.get("--transport-trust");
  if (servers.length === 0 || contractsDirectory === undefined || seedFile === undefined || trustFile === undefined) {
    throw new Error("Addressing 必须配置 NATS servers、contracts、transport seed 与 trust document");
  }
  const caFile = values.get("--nats-tls-ca");
  const certFile = values.get("--nats-tls-cert");
  const keyFile = values.get("--nats-tls-key");
  if ([caFile, certFile, keyFile].filter((value) => value !== undefined).length !== 0 && [caFile, certFile, keyFile].some((value) => value === undefined)) {
    throw new Error("NATS TLS CA、证书与私钥必须同时配置");
  }
  if (!allowInsecure && caFile === undefined) throw new Error("生产 Node Addressing 必须配置 NATS mTLS；本地开发需显式 --allow-insecure-nats");
  const composerLogicalService = values.get("--composer-logical-service");
  return Object.freeze({
    servers: Object.freeze(servers), clientName: "vastplan-portal-host",
    contractsDirectory: absolutePath(contractsDirectory, cwd), seedFile: absolutePath(seedFile, cwd), trustFile: absolutePath(trustFile, cwd),
    ...(caFile === undefined ? {} : { tls: Object.freeze({ caFile: absolutePath(caFile, cwd), certFile: absolutePath(certFile!, cwd), keyFile: absolutePath(keyFile!, cwd) }) }),
    ...(allowInsecure ? { allowInsecure: true } : {}),
    ...(composerLogicalService === undefined ? {} : { composerLogicalService }),
  });
}

function absolutePath(path: string, cwd: string): string {
  return isAbsolute(path) ? path : resolve(cwd, path);
}
