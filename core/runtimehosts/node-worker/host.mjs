#!/usr/bin/env node

import { accessSync, constants } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { createInterface } from 'node:readline';
import { fileURLToPath } from 'node:url';
import { Worker } from 'node:worker_threads';

const here = dirname(fileURLToPath(import.meta.url));

export function parseArguments(argv) {
  let entry = '';
  let pool = false;
  const pluginArgs = [];
  for (let index = 0; index < argv.length; index += 1) {
    const argument = argv[index];
    if (argument === '--entry') {
      entry = argv[index + 1] ?? '';
      index += 1;
    } else if (argument === '--pool') {
      pool = true;
    } else if (argument === '--') {
      pluginArgs.push(...argv.slice(index + 1));
      break;
    } else {
      pluginArgs.push(argument);
    }
  }
  if (!entry && !pool) {
    throw new Error('缺少必需参数 --entry');
  }
  return { entry: entry ? resolve(entry) : '', pluginArgs, pool };
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

export function startWorker({ entry, pluginArgs }, environment = process.env, redirectOutput = false) {
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
    stdout: redirectOutput,
    stderr: redirectOutput,
  });
}

function controlReply(message) {
  process.stdout.write(`${JSON.stringify(message)}\n`);
}

function stopWorker(worker) {
  worker.postMessage({ type: 'shutdown' });
  // protocolbus has already completed the graceful lifecycle handshake before
  // issuing a pool stop. Keep only a short guard for a wedged Worker.
  const timer = setTimeout(() => void worker.terminate(), 500);
  timer.unref();
  worker.once('exit', () => clearTimeout(timer));
}

export async function runPool(input = process.stdin) {
  const units = new Map();
  const stoppingUnits = new Set();
  const reader = createInterface({ input, crlfDelay: Infinity });
  let stopping = false;

  const shutdown = () => {
    if (stopping) return;
    stopping = true;
    for (const [unitId, worker] of units) {
      stoppingUnits.add(unitId);
      stopWorker(worker);
    }
    reader.close();
  };
  process.once('SIGINT', shutdown);
  process.once('SIGTERM', shutdown);

  for await (const line of reader) {
    if (!line.trim()) continue;
    let request;
    try {
      request = JSON.parse(line);
      if (!request.requestId || !request.operation) throw new Error('控制请求缺少 requestId/operation');
      if (request.operation === 'start') {
        if (!request.unitId || !request.entry) throw new Error('start 缺少 unitId/entry');
        if (units.has(request.unitId)) throw new Error(`执行单元重复: ${request.unitId}`);
        const worker = startWorker({
          entry: resolve(request.entry),
          pluginArgs: Array.isArray(request.args) ? request.args.map(String) : [],
        }, request.environment ?? {}, true);
        units.set(request.unitId, worker);
        worker.stdout?.pipe(process.stderr, { end: false });
        worker.stderr?.pipe(process.stderr, { end: false });
        worker.on('message', (message) => {
          if (message?.type === 'fatal') {
            process.stderr.write(`Node Worker 插件启动失败 unit=${request.unitId}: ${message.error}\n`);
          }
        });
        worker.once('exit', (code) => {
          units.delete(request.unitId);
          const expected = stoppingUnits.delete(request.unitId);
          controlReply({ event: 'unit-exited', unitId: request.unitId, status: 'ok',
            ...(code === 0 || expected ? {} : { error: `Worker exit code ${code}` }) });
        });
        controlReply({ requestId: request.requestId, unitId: request.unitId, status: 'ok' });
      } else if (request.operation === 'stop') {
        const worker = units.get(request.unitId);
        if (worker) {
          stoppingUnits.add(request.unitId);
          stopWorker(worker);
        }
        controlReply({ requestId: request.requestId, unitId: request.unitId, status: 'ok' });
      } else if (request.operation === 'shutdown') {
        controlReply({ requestId: request.requestId, status: 'ok' });
        shutdown();
      } else {
        throw new Error(`未知控制操作: ${request.operation}`);
      }
    } catch (error) {
      controlReply({ requestId: request?.requestId ?? '', status: 'error', error: error.message });
    }
  }

  if (units.size > 0) {
    await Promise.all([...units.values()].map((worker) => new Promise((resolveExit) => worker.once('exit', resolveExit))));
  }
  return 0;
}

export async function run(argv = process.argv.slice(2), environment = process.env) {
  const parsed = parseArguments(argv);
  if (parsed.pool) return runPool();
  const worker = startWorker(parsed, environment);
  let stopping = false;
  const stop = () => {
    if (stopping) return;
    stopping = true;
    stopWorker(worker);
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
