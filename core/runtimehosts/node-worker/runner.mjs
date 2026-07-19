import { parentPort, workerData } from 'node:worker_threads';
import { pathToFileURL } from 'node:url';

if (!parentPort) {
  throw new Error('Node Worker runner 只能由 Runtime Host 创建');
}

let moduleNamespace;
parentPort.once('message', async (message) => {
  if (message?.type !== 'shutdown') return;
  try {
    await moduleNamespace?.shutdown?.();
  } finally {
    process.exitCode = 0;
  }
});

try {
  globalThis.__VASTPLAN_PLUGIN_ARGS__ = Object.freeze([...workerData.pluginArgs]);
  moduleNamespace = await import(pathToFileURL(workerData.entry).href);
  if (typeof moduleNamespace.start === 'function') {
    await moduleNamespace.start({ args: globalThis.__VASTPLAN_PLUGIN_ARGS__ });
  }
} catch (error) {
  parentPort.postMessage({ type: 'fatal', error: error.stack ?? error.message });
  throw error;
}
