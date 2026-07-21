import { connect, type NatsConnection, type NodeConnectionOptions } from "@nats-io/transport-node";
import { AddressingProtocolCodec } from "./protocol-codec.js";
import { NatsCapabilityDirectory } from "./capability-directory.js";
import { NodeAddressingClient } from "./addressing-client.js";
import { NodeTransportSecurity } from "./transport-security.js";

export interface NodeAddressingConfig {
  servers: readonly string[];
  clientName: string;
  contractsDirectory: string;
  seedFile: string;
  trustFile: string;
  tls?: { caFile: string; certFile: string; keyFile: string };
  allowInsecure?: boolean;
}

export interface NodeAddressingRuntime {
  client: NodeAddressingClient;
  close(): Promise<void>;
}

export async function openNodeAddressing(config: NodeAddressingConfig): Promise<NodeAddressingRuntime> {
  if (config.servers.length === 0 || !config.clientName || !config.contractsDirectory || !config.seedFile || !config.trustFile) throw new Error("Node Addressing 启动配置不完整");
  if (config.tls === undefined && config.allowInsecure !== true) throw new Error("生产 Node Addressing 必须配置 NATS TLS；本地开发需显式 allowInsecure");
  const security = await NodeTransportSecurity.open(config.seedFile, config.trustFile);
  let connection: NatsConnection | undefined;
  let directory: NatsCapabilityDirectory | undefined;
  try {
    const options: NodeConnectionOptions = {
      servers: [...config.servers], name: config.clientName,
      maxReconnectAttempts: -1,
      ...(config.tls === undefined ? {} : { tls: { ...config.tls, rejectUnauthorized: true } }),
    };
    connection = await connect(options);
    directory = await NatsCapabilityDirectory.open(connection, security);
    const client = new NodeAddressingClient({ connection, directory, security, codec: new AddressingProtocolCodec(config.contractsDirectory) });
    let closed = false;
    return {
      client,
      async close() {
        if (closed) return;
        closed = true;
        await directory?.close();
        await connection?.drain();
        security.close();
      },
    };
  } catch (error) {
    await directory?.close().catch(() => undefined);
    await connection?.close().catch(() => undefined);
    security.close();
    throw error;
  }
}
