import { readFile } from "node:fs/promises";
import { createServer as createHTTPServer, type Server } from "node:http";
import { createServer as createHTTPSServer } from "node:https";
import type { PortalHostConfig } from "../config/host-config";
import { PortalAssets } from "../assets/portal-assets";
import { createPortalHandler } from "../http/portal-handler";
import { FileIdentityProvider } from "../identity/file-identity-provider";

export async function createPortalServer(config: PortalHostConfig): Promise<Server> {
  const assets = await PortalAssets.load(config.portalAssets);
  const identity = await FileIdentityProvider.open(config.sessionFile);
  const handler = createPortalHandler({ assets, identity, secureCookies: config.tls !== undefined });
  if (config.tls === undefined) return createHTTPServer(handler);
  const [cert, key] = await Promise.all([readFile(config.tls.certFile), readFile(config.tls.keyFile)]);
  return createHTTPSServer({ cert, key, minVersion: "TLSv1.2" }, handler);
}

export async function listenPortalServer(server: Server, config: PortalHostConfig): Promise<void> {
  await new Promise<void>((resolve, reject) => {
    server.once("error", reject);
    server.listen(config.listenPort, config.listenHost, () => {
      server.off("error", reject);
      resolve();
    });
  });
}

export async function closePortalServer(server: Server): Promise<void> {
  if (!server.listening) return;
  await new Promise<void>((resolve, reject) => server.close((error) => error === undefined ? resolve() : reject(error)));
}
