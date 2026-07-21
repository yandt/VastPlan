import { pathToFileURL } from "node:url";

const [runtimePath, contractsDirectory, seedFile, trustFile, serverURL, capability, caFile, certFile, keyFile] = process.argv.slice(2);
if ([runtimePath, contractsDirectory, seedFile, trustFile, serverURL, capability].some((value) => !value)) {
  throw new Error("Node Addressing E2E 参数不完整");
}

const { openNodeAddressing } = await import(pathToFileURL(runtimePath).href);
const runtime = await openNodeAddressing({
  servers: [serverURL],
  clientName: "node-addressing-e2e",
  contractsDirectory,
  seedFile,
  trustFile,
  ...(caFile && certFile && keyFile ? { tls: { caFile, certFile, keyFile } } : { allowInsecure: true }),
});

try {
  const response = await runtime.client.invoke({
    extension_point: "tool.package",
    capability,
  }, {
    caller: { kind: 4, id: "node-addressing-e2e" },
    scene: "addressing.e2e",
    tenant_id: "acme",
  }, new TextEncoder().encode("from-node"));
  process.stdout.write(JSON.stringify({
    status: response.result.status,
    payload: new TextDecoder().decode(response.payload),
  }));
} finally {
  await runtime.close();
}
