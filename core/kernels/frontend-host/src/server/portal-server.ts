import { readFile } from "node:fs/promises";
import { createServer as createHTTPServer, type Server } from "node:http";
import { createServer as createHTTPSServer } from "node:https";
import type { PortalHostConfig } from "../config/host-config";
import { PortalAssets } from "../assets/portal-assets";
import { createPortalHandler } from "../http/portal-handler";
import { FileIdentityProvider } from "../identity/file-identity-provider";
import { openNodeAddressing, type NodeAddressingRuntime } from "@vastplan/addressing-node";
import { AddressingPortalComposerClient } from "../capabilities/portal-composer-client";

const addressingRuntimes = new WeakMap<Server, NodeAddressingRuntime>();

export async function createPortalServer(config: PortalHostConfig): Promise<Server> {
  const assets = await PortalAssets.load(config.portalAssets);
  const identity = await FileIdentityProvider.open(config.sessionFile);
  const addressing = config.addressing === undefined ? undefined : await openNodeAddressing(config.addressing);
  try {
    const composer = addressing === undefined ? undefined : new AddressingPortalComposerClient(addressing.client, config.addressing?.composerLogicalService);
    const handler = createPortalHandler({ assets, identity, secureCookies: config.tls !== undefined, ...(composer === undefined ? {} : { composer }) });
    let server: Server;
    if (config.tls === undefined) server = createHTTPServer(handler);
    else {
      const [cert, key] = await Promise.all([readFile(config.tls.certFile), readFile(config.tls.keyFile)]);
      server = createHTTPSServer({ cert, key, minVersion: "TLSv1.2" }, handler);
    }
    if (addressing !== undefined) addressingRuntimes.set(server, addressing);
    return server;
  } catch (error) {
    await addressing?.close();
    throw error;
  }
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
  const addressing = addressingRuntimes.get(server);
  addressingRuntimes.delete(server);
  try {
    if (server.listening) await new Promise<void>((resolve, reject) => server.close((error) => error === undefined ? resolve() : reject(error)));
  } finally {
    await addressing?.close();
  }
}
