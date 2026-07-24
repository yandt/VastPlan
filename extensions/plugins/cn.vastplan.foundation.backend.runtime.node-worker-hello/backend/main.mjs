import { Contribution, Plugin, callResult } from '@vastplan/backend-plugin';

const plugin = new Plugin({
  id: 'cn.vastplan.foundation.backend.runtime.node-worker-hello',
  version: '0.1.1',
  engines: { backend: '^0.1 || ^1.0' },
});

plugin.contribute(new Contribution({
  extensionPoint: 'tool.package',
  id: 'vastplan.node-worker-hello',
  descriptor: {
    title: 'Node Worker Hello',
    subcommands: [{
      name: 'echo',
      description: '由 Node Worker 回显输入',
      paramsSchema: {
        type: 'object',
        properties: { text: { type: 'string' } },
        required: ['text'],
      },
    }],
  },
  handlers: {
    echo: (invocation, _host, context, payload) => {
      invocation.throwIfCancelled();
      const input = JSON.parse(payload.toString() || '{}');
      return callResult.ok(Buffer.from(JSON.stringify({
        echo: input.text ?? '',
        runtime: 'node-worker',
        tenant: context.tenant_id ?? '',
      })));
    },
  },
}));

export const start = () => plugin.serve();
export const shutdown = () => plugin.shutdown();
