import grpc from '@grpc/grpc-js';
import protoLoader from '@grpc/proto-loader';
import { resolve } from 'node:path';

let cached;

export function loadProtocol(environment = process.env) {
  if (cached) return cached;
  const root = environment.VASTPLAN_CONTRACTS_DIR;
  if (!root) {
    throw new Error('Runtime Host 未注入 VASTPLAN_CONTRACTS_DIR');
  }
  const definition = protoLoader.loadSync(resolve(root, 'pluginhost/v1/pluginhost.proto'), {
    includeDirs: [root],
    keepCase: true,
    longs: String,
    enums: String,
    defaults: true,
    oneofs: true,
  });
  const tree = grpc.loadPackageDefinition(definition);
  cached = {
    grpc,
    PluginHost: tree.vastplan.pluginhost.v1.PluginHost,
  };
  return cached;
}
