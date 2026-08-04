const defaultPorts: Readonly<Record<string, number>> = Object.freeze({ postgresql: 5432, mysql: 3306 });

export interface ConnectionEndpointFields {
  host: string;
  port: number;
}

export function defaultConnectionPort(providerID: string): number {
  return defaultPorts[providerID] ?? 0;
}

/** Converts the persisted provider-neutral endpoint into separate form fields. */
export function connectionEndpointFields(endpoint: string, providerID: string): ConnectionEndpointFields {
  const value = endpoint.trim();
  const fallbackPort = defaultConnectionPort(providerID);
  const bracketed = /^\[([^\]]+)\](?::(\d+))?$/.exec(value);
  if (bracketed !== null) return { host: bracketed[1] ?? "", port: parsePort(bracketed[2]) ?? fallbackPort };
  const hostPort = /^([^:]+):(\d+)$/.exec(value);
  if (hostPort !== null) return { host: hostPort[1] ?? "", port: parsePort(hostPort[2]) ?? fallbackPort };
  return { host: value, port: fallbackPort };
}

/** Produces the sole endpoint value accepted by the database Runtime. */
export function connectionEndpoint(hostValue: unknown, portValue: unknown): string | undefined {
  const host = normalizedHost(hostValue);
  const port = validPort(portValue);
  if (host === undefined || port === undefined) return undefined;
  return host.includes(":") ? `[${host}]:${port}` : `${host}:${port}`;
}

function normalizedHost(value: unknown): string | undefined {
  if (typeof value !== "string") return undefined;
  const host = value.trim().replace(/^\[|\]$/g, "");
  if (host === "" || hasSingleColon(host)) return undefined;
  return host;
}

function hasSingleColon(value: string): boolean {
  const first = value.indexOf(":");
  return first >= 0 && first === value.lastIndexOf(":");
}

function validPort(value: unknown): number | undefined {
  return typeof value === "number" && Number.isSafeInteger(value) && value >= 1 && value <= 65535 ? value : undefined;
}

function parsePort(value: string | undefined): number | undefined {
  return value === undefined ? undefined : validPort(Number(value));
}
