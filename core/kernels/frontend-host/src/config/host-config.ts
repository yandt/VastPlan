import { isAbsolute, resolve } from "node:path";
import { addressingValueArguments, parseAddressingConfig, type PortalAddressingConfig } from "./addressing-config";
import { identityValueArguments, parseIdentityConfig, type PortalIdentityConfig } from "./identity-config";

export interface PortalHostConfig {
  listenHost: string;
  listenPort: number;
  portalAssets: string;
  accessProfileCatalog?: string;
  identity: PortalIdentityConfig;
  tls?: { certFile: string; keyFile: string };
  allowInsecureHTTP: boolean;
  addressing?: PortalAddressingConfig;
  delivery?: { cacheRoot: string; originRoot?: string };
}

export function parseHostArguments(args: readonly string[], cwd = process.cwd()): PortalHostConfig {
  const values = new Map<string, string>();
  let allowInsecureHTTP = false;
  let allowInsecureNATS = false;
  let allowInsecureOIDC = false;
  for (let index = 0; index < args.length; index += 1) {
    const name = args[index];
    if (name === "--allow-insecure-http") {
      allowInsecureHTTP = true;
      continue;
    }
    if (name === "--allow-insecure-nats") {
      allowInsecureNATS = true;
      continue;
    }
    if (name === "--oidc-allow-insecure") {
      allowInsecureOIDC = true;
      continue;
    }
    if (!name.startsWith("--") || index + 1 >= args.length || args[index + 1].startsWith("--")) {
      throw new Error(`无效启动参数: ${name}`);
    }
    if (values.has(name)) throw new Error(`重复启动参数: ${name}`);
    values.set(name, args[index + 1]);
    index += 1;
  }
  const allowed = new Set([
    "--listen", "--portal-assets", "--tls-cert", "--tls-key",
    "--access-profile-catalog",
    "--frontend-delivery-cache", "--frontend-delivery-origin", ...identityValueArguments, ...addressingValueArguments,
  ]);
  for (const name of values.keys()) if (!allowed.has(name)) throw new Error(`未知启动参数: ${name}`);

  const [listenHost, portText, extra] = (values.get("--listen") ?? "127.0.0.1:8443").split(":");
  const listenPort = Number(portText);
  if (!listenHost || extra !== undefined || !Number.isSafeInteger(listenPort) || listenPort < 1 || listenPort > 65_535) {
    throw new Error("--listen 必须为 host:port");
  }
  const portalAssets = values.get("--portal-assets");
  if (!portalAssets) throw new Error("必须配置 --portal-assets");
  const identity = parseIdentityConfig(values, allowInsecureOIDC, cwd);
  const certFile = values.get("--tls-cert");
  const keyFile = values.get("--tls-key");
  if ((certFile === undefined) !== (keyFile === undefined)) throw new Error("TLS 证书与私钥必须同时配置");
  if (!allowInsecureHTTP && (certFile === undefined || keyFile === undefined)) {
    throw new Error("生产 Portal Host 必须配置 TLS；本地开发需显式 --allow-insecure-http");
  }
  const addressing = parseAddressingConfig(values, allowInsecureNATS, cwd);
  const cacheRoot = values.get("--frontend-delivery-cache");
  const originRoot = values.get("--frontend-delivery-origin");
  const accessProfileCatalog = values.get("--access-profile-catalog");
  if (originRoot !== undefined && cacheRoot === undefined) throw new Error("配置 delivery origin 时必须同时配置本机 cache");
  return Object.freeze({
    listenHost,
    listenPort,
    portalAssets: absolutePath(portalAssets, cwd),
    ...(accessProfileCatalog === undefined ? {} : { accessProfileCatalog: absolutePath(accessProfileCatalog, cwd) }),
    identity,
    tls: certFile === undefined ? undefined : Object.freeze({ certFile: absolutePath(certFile, cwd), keyFile: absolutePath(keyFile!, cwd) }),
    allowInsecureHTTP,
    ...(addressing === undefined ? {} : { addressing }),
    ...(cacheRoot === undefined ? {} : {
      delivery: Object.freeze({
        cacheRoot: absolutePath(cacheRoot, cwd),
        ...(originRoot === undefined ? {} : { originRoot: absolutePath(originRoot, cwd) }),
      }),
    }),
  });
}

function absolutePath(path: string, cwd: string): string {
  return isAbsolute(path) ? path : resolve(cwd, path);
}
