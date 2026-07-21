import { createHash, generateKeyPairSync, sign } from "node:crypto";
import { createServer, type Server } from "node:http";
import type { AddressInfo } from "node:net";

export interface OIDCTestProvider {
  readonly issuer: string;
  close(): Promise<void>;
}

export async function startOIDCTestProvider(clientId: string): Promise<OIDCTestProvider> {
  const { privateKey, publicKey } = generateKeyPairSync("rsa", { modulusLength: 2048 });
  const jwk = { ...(publicKey.export({ format: "jwk" })), kid: "test-key", alg: "RS256", use: "sig" };
  let issuer = "";
  const codes = new Map<string, { nonce: string; challenge: string; redirectURI: string }>();
  const server = createServer(async (request, response) => {
    const url = new URL(request.url ?? "/", issuer || "http://127.0.0.1");
    if (url.pathname === "/.well-known/openid-configuration") return json(response, {
      issuer, authorization_endpoint: `${issuer}authorize`, token_endpoint: `${issuer}token`, jwks_uri: `${issuer}jwks`,
      response_types_supported: ["code"], subject_types_supported: ["public"], id_token_signing_alg_values_supported: ["RS256"],
      token_endpoint_auth_methods_supported: ["none"], code_challenge_methods_supported: ["S256"],
    });
    if (url.pathname === "/jwks") return json(response, { keys: [jwk] });
    if (url.pathname === "/authorize") {
      const state = url.searchParams.get("state"), nonce = url.searchParams.get("nonce"), challenge = url.searchParams.get("code_challenge");
      const redirectURI = url.searchParams.get("redirect_uri");
      if (url.searchParams.get("client_id") !== clientId || url.searchParams.get("response_type") !== "code" || url.searchParams.get("code_challenge_method") !== "S256"
        || state === null || nonce === null || challenge === null || redirectURI === null) return json(response, { error: "invalid_request" }, 400);
      const code = "test-authorization-code";
      codes.set(code, { nonce, challenge, redirectURI });
      response.statusCode = 302;
      response.setHeader("Location", `${redirectURI}?code=${code}&state=${encodeURIComponent(state)}`);
      return response.end();
    }
    if (url.pathname === "/token" && request.method === "POST") {
      const body = new URLSearchParams(await readBody(request));
      const code = body.get("code") ?? "", verifier = body.get("code_verifier") ?? "";
      const transaction = codes.get(code);
      if (transaction === undefined || body.get("client_id") !== clientId || body.get("redirect_uri") !== transaction.redirectURI
        || createHash("sha256").update(verifier).digest("base64url") !== transaction.challenge) return json(response, { error: "invalid_grant" }, 400);
      codes.delete(code);
      const now = Math.floor(Date.now() / 1000);
      const idToken = jwt({ iss: issuer, aud: clientId, sub: "alice", iat: now, exp: now + 600, nonce: transaction.nonce, tenant_id: "tenant-a", roles: ["portal.read", "platform.admin"] }, privateKey);
      return json(response, { access_token: "opaque-access-token", token_type: "Bearer", expires_in: 300, id_token: idToken });
    }
    json(response, { error: "not_found" }, 404);
  });
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  const address = server.address() as AddressInfo;
  issuer = `http://127.0.0.1:${address.port}/`;
  return { issuer, close: () => close(server) };
}

function jwt(payload: Readonly<Record<string, unknown>>, privateKey: ReturnType<typeof generateKeyPairSync>["privateKey"]): string {
  const header = Buffer.from(JSON.stringify({ alg: "RS256", kid: "test-key", typ: "JWT" })).toString("base64url");
  const claims = Buffer.from(JSON.stringify(payload)).toString("base64url");
  const input = `${header}.${claims}`;
  return `${input}.${sign("RSA-SHA256", Buffer.from(input), privateKey).toString("base64url")}`;
}
async function readBody(request: import("node:http").IncomingMessage): Promise<string> {
  const chunks: Buffer[] = [];
  for await (const chunk of request) {
    chunks.push(Buffer.from(chunk));
    if (chunks.reduce((sum, value) => sum + value.byteLength, 0) > 64 * 1024) throw new Error("OIDC test request too large");
  }
  return Buffer.concat(chunks).toString("utf8");
}
function json(response: import("node:http").ServerResponse, value: unknown, status = 200): void {
  response.statusCode = status;
  response.setHeader("Content-Type", "application/json");
  response.end(JSON.stringify(value));
}
function close(server: Server): Promise<void> { return new Promise((resolve, reject) => server.close((error) => error === undefined ? resolve() : reject(error))); }
