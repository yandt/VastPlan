import { isAbsolute, resolve } from "node:path";

export interface FileIdentityConfig {
  readonly kind: "file";
  readonly sessionFile: string;
}

export interface OIDCIdentityConfig {
  readonly kind: "oidc";
  readonly issuer: string;
  readonly clientId: string;
  readonly clientSecretFile?: string;
  readonly clientAuthMethod: "client_secret_basic" | "client_secret_post" | "none";
  readonly redirectURI: string;
  readonly sessionKeyFile: string;
  readonly tenantClaim: string;
  readonly rolesClaim: string;
  readonly defaultTenant?: string;
  readonly scopes: string;
  readonly sessionMaxAgeSeconds: number;
  readonly allowInsecure: boolean;
}

export type PortalIdentityConfig = FileIdentityConfig | OIDCIdentityConfig;

export const identityValueArguments = Object.freeze([
  "--identity-provider", "--session-file", "--oidc-issuer", "--oidc-client-id", "--oidc-client-secret-file",
  "--oidc-client-auth-method", "--oidc-redirect-uri", "--oidc-session-key-file", "--oidc-tenant-claim",
  "--oidc-roles-claim", "--oidc-default-tenant", "--oidc-scopes", "--oidc-session-max-age",
]);

export function parseIdentityConfig(values: ReadonlyMap<string, string>, allowInsecureOIDC: boolean, cwd: string): PortalIdentityConfig {
  const kind = values.get("--identity-provider") ?? (values.has("--oidc-issuer") ? "oidc" : "file");
  if (kind === "file") {
    if (allowInsecureOIDC || identityValueArguments.slice(2).some((name) => values.has(name))) throw new Error("文件身份模式不能配置 OIDC 参数");
    const sessionFile = values.get("--session-file");
    if (sessionFile === undefined) throw new Error("文件身份模式必须配置 --session-file");
    return Object.freeze({ kind: "file", sessionFile: absolutePath(sessionFile, cwd) });
  }
  if (kind !== "oidc") throw new Error("--identity-provider 只接受 file 或 oidc");
  if (values.has("--session-file")) throw new Error("OIDC 身份模式不能配置 --session-file");
  const issuer = required(values, "--oidc-issuer");
  const clientId = required(values, "--oidc-client-id");
  const redirectURI = required(values, "--oidc-redirect-uri");
  const sessionKeyFile = required(values, "--oidc-session-key-file");
  const issuerURL = validURL(issuer, "OIDC issuer");
  const redirectURL = validURL(redirectURI, "OIDC redirect URI");
  if (issuerURL.search !== "" || issuerURL.hash !== "") throw new Error("OIDC issuer 不得包含 query 或 fragment");
  if (!allowInsecureOIDC && (issuerURL.protocol !== "https:" || redirectURL.protocol !== "https:")) throw new Error("生产 OIDC issuer 与 redirect URI 必须使用 HTTPS");
  if (redirectURL.pathname !== "/auth/callback" || redirectURL.search !== "" || redirectURL.hash !== "") throw new Error("OIDC redirect URI 必须精确指向 /auth/callback");
  const clientSecretFile = values.get("--oidc-client-secret-file");
  const clientAuthMethod = values.get("--oidc-client-auth-method") ?? (clientSecretFile === undefined ? "none" : "client_secret_basic");
  if (!new Set(["client_secret_basic", "client_secret_post", "none"]).has(clientAuthMethod)) throw new Error("OIDC client auth method 无效");
  if ((clientAuthMethod === "none") !== (clientSecretFile === undefined)) throw new Error("OIDC confidential client 必须配置 secret，public client 不得配置 secret");
  const sessionMaxAgeSeconds = integer(values.get("--oidc-session-max-age") ?? "900", "OIDC session max age", 60, 3600);
  const tenantClaim = claimName(values.get("--oidc-tenant-claim") ?? "tenant_id", "OIDC tenant claim");
  const rolesClaim = claimName(values.get("--oidc-roles-claim") ?? "roles", "OIDC roles claim");
  if (tenantClaim === rolesClaim) throw new Error("OIDC tenant claim 与 roles claim 不能相同");
  const defaultTenant = values.get("--oidc-default-tenant");
  if (defaultTenant !== undefined && !safeIdentifier(defaultTenant)) throw new Error("OIDC default tenant 无效");
  const scopes = validScopes(values.get("--oidc-scopes") ?? "openid profile email");
  return Object.freeze({
    kind: "oidc", issuer: issuerURL.href, clientId,
    ...(clientSecretFile === undefined ? {} : { clientSecretFile: absolutePath(clientSecretFile, cwd) }),
    clientAuthMethod: clientAuthMethod as OIDCIdentityConfig["clientAuthMethod"], redirectURI: redirectURL.href,
    sessionKeyFile: absolutePath(sessionKeyFile, cwd), tenantClaim, rolesClaim,
    ...(defaultTenant === undefined ? {} : { defaultTenant }),
    scopes, sessionMaxAgeSeconds, allowInsecure: allowInsecureOIDC,
  });
}

function required(values: ReadonlyMap<string, string>, name: string): string {
  const value = values.get(name);
  if (value === undefined || value.trim() === "") throw new Error(`OIDC 必须配置 ${name}`);
  return value;
}
function validURL(value: string, label: string): URL {
  let parsed: URL;
  try { parsed = new URL(value); } catch { throw new Error(`${label} 无效`); }
  if (parsed.username !== "" || parsed.password !== "") throw new Error(`${label} 不得包含凭据`);
  if (parsed.protocol !== "https:" && parsed.protocol !== "http:") throw new Error(`${label} 必须使用 HTTP(S)`);
  return parsed;
}
function claimName(value: string, label: string): string {
  if (!/^[A-Za-z0-9_.:-]{1,128}$/.test(value)) throw new Error(`${label} 无效`);
  return value;
}
function validScopes(value: string): string {
  if (value.length > 1024 || /[\u0000-\u001f\u007f]/.test(value)) throw new Error("OIDC scopes 无效");
  const scopes = value.trim().split(/ +/).filter(Boolean);
  if (!scopes.includes("openid") || new Set(scopes).size !== scopes.length || scopes.some((scope) => !/^[\x21-\x7e]+$/.test(scope))) throw new Error("OIDC scopes 必须包含唯一的 openid");
  return scopes.join(" ");
}
function safeIdentifier(value: string): boolean { return value.length > 0 && value.length <= 256 && /^[A-Za-z0-9._:@/-]+$/.test(value); }
function integer(value: string, label: string, minimum: number, maximum: number): number {
  const parsed = Number(value);
  if (!Number.isSafeInteger(parsed) || parsed < minimum || parsed > maximum) throw new Error(`${label} 必须在 ${minimum}..${maximum} 范围`);
  return parsed;
}
function absolutePath(path: string, cwd: string): string { return isAbsolute(path) ? path : resolve(cwd, path); }
