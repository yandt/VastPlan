import { parseHostArguments } from "./config/host-config";
import { closePortalServer, createPortalServer, listenPortalServer } from "./server/portal-server";

async function main(): Promise<void> {
  const config = parseHostArguments(process.argv.slice(2));
  const server = await createPortalServer(config);
  await listenPortalServer(server, config);
  process.stdout.write(`Node Portal Kernel listening on ${config.listenHost}:${config.listenPort}\n`);
  const stop = async () => {
    await closePortalServer(server);
    process.exitCode = 0;
  };
  process.once("SIGINT", stop);
  process.once("SIGTERM", stop);
}

main().catch((error: unknown) => {
  process.stderr.write(`${error instanceof Error ? error.message : String(error)}\n`);
  process.exitCode = 1;
});
