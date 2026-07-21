import { isAbsolute, resolve } from "node:path";

export interface PortalHostConfig {
  listenHost: string;
  listenPort: number;
  portalAssets: string;
  sessionFile: string;
  tls?: { certFile: string; keyFile: string };
  allowInsecureHTTP: boolean;
}

export function parseHostArguments(args: readonly string[], cwd = process.cwd()): PortalHostConfig {
  const values = new Map<string, string>();
  let allowInsecureHTTP = false;
  for (let index = 0; index < args.length; index += 1) {
    const name = args[index];
    if (name === "--allow-insecure-http") {
      allowInsecureHTTP = true;
      continue;
    }
    if (!name.startsWith("--") || index + 1 >= args.length || args[index + 1].startsWith("--")) {
      throw new Error(`无效启动参数: ${name}`);
    }
    if (values.has(name)) throw new Error(`重复启动参数: ${name}`);
    values.set(name, args[index + 1]);
    index += 1;
  }
  const allowed = new Set(["--listen", "--portal-assets", "--session-file", "--tls-cert", "--tls-key"]);
  for (const name of values.keys()) if (!allowed.has(name)) throw new Error(`未知启动参数: ${name}`);

  const [listenHost, portText, extra] = (values.get("--listen") ?? "127.0.0.1:8443").split(":");
  const listenPort = Number(portText);
  if (!listenHost || extra !== undefined || !Number.isSafeInteger(listenPort) || listenPort < 1 || listenPort > 65_535) {
    throw new Error("--listen 必须为 host:port");
  }
  const portalAssets = values.get("--portal-assets");
  if (!portalAssets) throw new Error("必须配置 --portal-assets");
  const sessionFile = values.get("--session-file");
  if (!sessionFile) throw new Error("必须配置 --session-file");
  const certFile = values.get("--tls-cert");
  const keyFile = values.get("--tls-key");
  if ((certFile === undefined) !== (keyFile === undefined)) throw new Error("TLS 证书与私钥必须同时配置");
  if (!allowInsecureHTTP && (certFile === undefined || keyFile === undefined)) {
    throw new Error("生产 Portal Host 必须配置 TLS；本地开发需显式 --allow-insecure-http");
  }
  return Object.freeze({
    listenHost,
    listenPort,
    portalAssets: absolutePath(portalAssets, cwd),
    sessionFile: absolutePath(sessionFile, cwd),
    tls: certFile === undefined ? undefined : Object.freeze({ certFile: absolutePath(certFile, cwd), keyFile: absolutePath(keyFile!, cwd) }),
    allowInsecureHTTP,
  });
}

function absolutePath(path: string, cwd: string): string {
  return isAbsolute(path) ? path : resolve(cwd, path);
}
