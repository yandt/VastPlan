import type { APIContractContribution, APIRouteContract } from "./api-exposure-contract";

export interface MatchedAPIRoute {
  readonly route: APIRouteContract;
  readonly pathParams: Readonly<Record<string, string>>;
}

export function matchAPIRoute(contract: APIContractContribution, method: string, path: string): MatchedAPIRoute | "method-not-allowed" | undefined {
  let pathMatched = false;
  for (const route of contract.routes) {
    const parameters = matchTemplate(route.path, path);
    if (parameters === undefined) continue;
    pathMatched = true;
    if (route.method === method) return { route, pathParams: parameters };
  }
  return pathMatched ? "method-not-allowed" : undefined;
}

function matchTemplate(template: string, path: string): Readonly<Record<string, string>> | undefined {
  const expected = template.split("/").slice(1);
  const actual = path.split("/").slice(1);
  if (expected.length !== actual.length) return undefined;
  const parameters: Record<string, string> = {};
  for (let index = 0; index < expected.length; index += 1) {
    const segment = expected[index];
    const raw = actual[index];
    if (!segment.startsWith("{")) {
      if (segment !== raw) return undefined;
      continue;
    }
    let decoded: string;
    try { decoded = decodeURIComponent(raw); } catch { return undefined; }
    if (decoded === "" || Buffer.byteLength(decoded) > 2_048 || decoded.includes("/") || decoded.includes("\\") || /[\u0000-\u001f\u007f]/.test(decoded)) return undefined;
    parameters[segment.slice(1, -1)] = decoded;
  }
  return Object.freeze(parameters);
}
