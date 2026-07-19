#!/usr/bin/env node

import { accessSync, constants } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { Worker } from 'node:worker_threads';

const here = dirname(fileURLToPath(import.meta.url));

export function parseArguments(argv) {
  let entry = '';
  const pluginArgs = [];
  for (let index = 0; index < argv.length; index += 1) {
    const argument = argv[index];
    if (argument === '--entry') {
      entry = argv[index + 1] ?? '';
      index += 1;
    } else if (argument === '--') {
      pluginArgs.push(...argv.slice(index + 1));
      break;
    } else {
      pluginArgs.push(argument);
    }
  }
  if (!entry) {
    throw new Error('缺少必需参数 --entry');
  }
  return { entry: resolve(entry), pluginArgs };
}

export function resourceLimits(environment = process.env) {
  const readPositiveInteger = (name) => {
    const raw = environment[name];
    if (raw === undefined || raw === '') return undefined;
    const value = Number(raw);
    if (!Number.isSafeInteger(value) || value <= 0) {
      throw new Error(`${name} 必须是正整数`);
    }
    return value;
  };
  return {
    maxOldGenerationSizeMb: readPositiveInteger('VASTPLAN_NODE_MAX_OLD_MB') ?? 256,
    maxYoungGenerationSizeMb: readPositiveInteger('VASTPLAN_NODE_MAX_YOUNG_MB') ?? 64,
    stackSizeMb: readPositiveInteger('VASTPLAN_NODE_STACK_MB') ?? 8,
  };
}

export function contractsDirectory(environment = process.env) {
  const configured = environment.VASTPLAN_CONTRACTS_DIR;
  const candidate = configured ? resolve(configured) : resolve(here, '../../../contracts/proto');
  accessSync(resolve(candidate, 'pluginhost/v1/pluginhost.proto'), constants.R_OK);
  return candidate;
}

export function startWorker({ entry, pluginArgs }, environment = process.env) {
  accessSync(entry, constants.R_OK);
  const workerEnvironment = {
    ...environment,
    VASTPLAN_CONTRACTS_DIR: contractsDirectory(environment),
  };
  return new Worker(new URL('./runner.mjs', import.meta.url), {
    workerData: { entry, pluginArgs },
    env: workerEnvironment,
    resourceLimits: resourceLimits(environment),
    name: `vastplan:${entry}`,
  });
}

export async function run(argv = process.argv.slice(2), environment = process.env) {
  const worker = startWorker(parseArguments(argv), environment);
  let stopping = false;
  const stop = () => {
    if (stopping) return;
    stopping = true;
    worker.postMessage({ type: 'shutdown' });
    const timer = setTimeout(() => worker.terminate(), 5_000);
    timer.unref();
  };
  process.once('SIGINT', stop);
  process.once('SIGTERM', stop);
  worker.on('message', (message) => {
    if (message?.type === 'fatal') {
      process.stderr.write(`Node Worker 插件启动失败: ${message.error}\n`);
    }
  });
  worker.on('error', (error) => {
    process.stderr.write(`Node Worker 异常: ${error.stack ?? error.message}\n`);
  });
  return new Promise((resolveExit) => {
    worker.once('exit', (code) => resolveExit(stopping && code === 1 ? 0 : code));
  });
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  run().then((code) => {
    process.exitCode = code;
  }).catch((error) => {
    process.stderr.write(`Node Runtime Host 启动失败: ${error.stack ?? error.message}\n`);
    process.exitCode = 1;
  });
}
