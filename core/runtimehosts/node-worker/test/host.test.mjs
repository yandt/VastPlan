import assert from 'node:assert/strict';
import test from 'node:test';

import { parseArguments, resourceLimits } from '../host.mjs';

test('parseArguments separates the trusted entry from plugin arguments', () => {
  const parsed = parseArguments(['--entry', './plugin.mjs', '--', '--tenant', 'acme']);
  assert.match(parsed.entry, /plugin\.mjs$/);
  assert.deepEqual(parsed.pluginArgs, ['--tenant', 'acme']);
	assert.equal(parsed.pool, false);
});

test('parseArguments accepts pool control mode without a plugin entry', () => {
  assert.deepEqual(parseArguments(['--pool']), { entry: '', pluginArgs: [], pool: true });
});

test('parseArguments rejects a missing entry', () => {
  assert.throws(() => parseArguments([]), /--entry/);
});

test('resource limits are bounded positive integers', () => {
  assert.deepEqual(resourceLimits({}), {
    maxOldGenerationSizeMb: 256,
    maxYoungGenerationSizeMb: 64,
    stackSizeMb: 8,
  });
  assert.throws(() => resourceLimits({ VASTPLAN_NODE_MAX_OLD_MB: '0' }), /正整数/);
});
