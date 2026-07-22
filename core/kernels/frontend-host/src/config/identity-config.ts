import { isAbsolute, resolve } from "node:path";

export interface FileIdentityConfig {
  readonly kind: "file";
  readonly sessionFile: string;
}

export interface BrokerIdentityConfig {
  readonly kind: "broker";
  readonly assertionTrustFile: string;
  readonly sessionKeyFile: string;
  readonly sessionMaxAgeSeconds: number;
  readonly brokerLogicalService?: string;
  readonly authorizationLogicalService?: string;
}

export type PortalIdentityConfig = FileIdentityConfig | BrokerIdentityConfig;

export const identityValueArguments = Object.freeze([
  "--identity-provider", "--session-file", "--authentication-assertion-trust-file", "--portal-session-key-file",
  "--portal-session-max-age", "--authentication-broker-logical-service", "--authorization-session-logical-service",
]);

export function parseIdentityConfig(values: ReadonlyMap<string, string>, cwd: string): PortalIdentityConfig {
  const kind = values.get("--identity-provider") ?? "file";
  if (kind === "file") {
    if (identityValueArguments.slice(2).some((name) => values.has(name))) throw new Error("文件身份模式不能配置 Broker 参数");
    const sessionFile = values.get("--session-file");
    if (sessionFile === undefined) throw new Error("文件身份模式必须配置 --session-file");
    return Object.freeze({ kind: "file", sessionFile: absolutePath(sessionFile, cwd) });
  }
  if (kind !== "broker") throw new Error("--identity-provider 只接受 file 或 broker；企业协议由 authentication Provider 插件实现");
  if (values.has("--session-file")) throw new Error("Broker 身份模式不能配置 --session-file");
  const assertionTrustFile = required(values, "--authentication-assertion-trust-file");
  const sessionKeyFile = required(values, "--portal-session-key-file");
  const sessionMaxAgeSeconds = integer(values.get("--portal-session-max-age") ?? "900", "Portal session max age", 60, 3600);
  const brokerLogicalService = optionalID(values.get("--authentication-broker-logical-service"), "Authentication Broker logical service");
  const authorizationLogicalService = optionalID(values.get("--authorization-session-logical-service"), "Authorization Session logical service");
  return Object.freeze({
    kind: "broker", assertionTrustFile: absolutePath(assertionTrustFile, cwd), sessionKeyFile: absolutePath(sessionKeyFile, cwd), sessionMaxAgeSeconds,
    ...(brokerLogicalService === undefined ? {} : { brokerLogicalService }),
    ...(authorizationLogicalService === undefined ? {} : { authorizationLogicalService }),
  });
}

function required(values: ReadonlyMap<string, string>, name: string): string { const value = values.get(name); if (value === undefined || value.trim() === "") throw new Error(`Broker 身份模式必须配置 ${name}`); return value; }
function optionalID(value: string | undefined, label: string): string | undefined { if (value !== undefined && !/^[A-Za-z0-9][A-Za-z0-9._-]{0,159}$/.test(value)) throw new Error(`${label} 无效`); return value; }
function integer(value: string, label: string, minimum: number, maximum: number): number { const parsed = Number(value); if (!Number.isSafeInteger(parsed) || parsed < minimum || parsed > maximum) throw new Error(`${label} 必须在 ${minimum}..${maximum} 范围`); return parsed; }
function absolutePath(path: string, cwd: string): string { return isAbsolute(path) ? path : resolve(cwd, path); }
