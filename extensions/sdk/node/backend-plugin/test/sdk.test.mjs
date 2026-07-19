import assert from 'node:assert/strict';
import test from 'node:test';

import { Contribution, InvocationContext, callResult } from '../src/index.mjs';

test('Contribution maps the stable wire fields', () => {
  const contribution = new Contribution({
    extensionPoint: 'tool.package',
    id: 'example.node',
    descriptor: { title: 'Node' },
    handlers: { echo: () => callResult.ok() },
  });
  assert.equal(contribution.wire().extension_point, 'tool.package');
  assert.equal(contribution.wire().id, 'example.node');
  assert.ok(Buffer.isBuffer(contribution.wire().descriptor_json));
});

test('InvocationContext exposes cancellation without a mutable trusted context', () => {
  const invocation = new InvocationContext({ request_id: 'r1', context: {}, delegation_token: 'opaque' });
  assert.equal(invocation.delegationToken, 'opaque');
  assert.equal(invocation.cancelled, false);
  invocation.signal.abort();
  assert.throws(() => invocation.throwIfCancelled(), /cancelled/);
});
