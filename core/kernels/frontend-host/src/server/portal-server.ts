import { readFile } from "node:fs/promises";
import { createServer as createHTTPServer, type Server } from "node:http";
import { createServer as createHTTPSServer } from "node:https";
import { join } from "node:path";
import type { PortalHostConfig } from "../config/host-config";
import { PortalAssets } from "../assets/portal-assets";
import { createPortalHandler } from "../http/portal-handler";
import { openIdentityProvider } from "../identity/identity-provider-factory";
import { openNodeAddressing, type NodeAddressingRuntime } from "@vastplan/addressing-node";
import { AddressingPortalComposerClient } from "../capabilities/portal-composer-client";
import { AddressingCapabilityInvoker } from "../capabilities/capability-invoker";
import { AddressingInteractionClient } from "../capabilities/interaction-client";
import { AddressingPlatformManagementClient } from "../capabilities/platform-management-client";
import { PlatformManagementResolver } from "../capabilities/platform-management-resolver";
import { PortalDeliveryStore } from "../runtime/portal-delivery-store";
import { PortalSSRCoordinator } from "../runtime/portal-ssr-coordinator";
import { ServerGenerationManager } from "../workers/server-generation-manager";
import { FileAccessProfileCatalog } from "../access/file-access-profile-catalog";

interface PortalServerResources {
	readonly addressing?: NodeAddressingRuntime;
	readonly generations?: ServerGenerationManager;
}

const serverResources = new WeakMap<Server, PortalServerResources>();

export async function createPortalServer(config: PortalHostConfig): Promise<Server> {
  const assets = await PortalAssets.load(config.portalAssets);
  const identity = await openIdentityProvider(config.identity);
  const access = config.accessProfileCatalog === undefined ? undefined : await FileAccessProfileCatalog.open(config.accessProfileCatalog);
  const addressing = config.addressing === undefined ? undefined : await openNodeAddressing(config.addressing);
  let generations: ServerGenerationManager | undefined;
  try {
    const invoker = addressing === undefined ? undefined : new AddressingCapabilityInvoker(addressing.client);
    const composer = invoker === undefined ? undefined : new AddressingPortalComposerClient(invoker, config.addressing?.composerLogicalService);
    const interaction = invoker === undefined ? undefined : new AddressingInteractionClient(invoker, config.addressing?.interactionLogicalService);
    const platform = invoker === undefined || composer === undefined ? undefined : {
      resolver: new PlatformManagementResolver(composer), client: new AddressingPlatformManagementClient(invoker),
    };
    const delivery = config.delivery === undefined ? undefined : await PortalDeliveryStore.open(config.delivery.cacheRoot, config.delivery.originRoot);
    generations = composer === undefined || delivery === undefined ? undefined : new ServerGenerationManager(
      delivery, join(config.delivery!.cacheRoot, "server-generations"), join(__dirname, "server-generation-worker.cjs"),
    );
    const ssr = composer === undefined || generations === undefined ? undefined : new PortalSSRCoordinator(composer, identity, generations);
    const handler = createPortalHandler({
      assets, identity, secureCookies: config.tls !== undefined,
      ...(access === undefined ? {} : { access }),
      ...(composer === undefined ? {} : { composer }),
      ...(interaction === undefined ? {} : { interaction }),
      ...(platform === undefined ? {} : { platform }),
      ...(delivery === undefined ? {} : { delivery }),
      ...(ssr === undefined ? {} : { ssr }),
    });
    let server: Server;
    if (config.tls === undefined) server = createHTTPServer(handler);
    else {
      const [cert, key] = await Promise.all([readFile(config.tls.certFile), readFile(config.tls.keyFile)]);
      server = createHTTPSServer({ cert, key, minVersion: "TLSv1.2" }, handler);
    }
    serverResources.set(server, { ...(addressing === undefined ? {} : { addressing }), ...(generations === undefined ? {} : { generations }) });
    return server;
  } catch (error) {
    await generations?.shutdown();
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
  const resources = serverResources.get(server);
  serverResources.delete(server);
  try {
    if (server.listening) await new Promise<void>((resolve, reject) => server.close((error) => error === undefined ? resolve() : reject(error)));
  } finally {
    await resources?.generations?.shutdown();
    await resources?.addressing?.close();
  }
}
