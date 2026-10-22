import test from 'node:test';
import assert from 'node:assert/strict';
import { toMaidenhead } from './maidenhead.js';

test('extended locator subdivides a subsquare while keeping six-character default', () => {
  assert.equal(toMaidenhead(43.0125, 1.375), 'JN03qa');
  assert.equal(toMaidenhead(43.0125, 1.375, 8), 'JN03qa53');
  assert.equal(toMaidenhead(0, 0, 8), 'JJ00aa00');
  assert.equal(toMaidenhead(-90, -180, 8), 'AA00aa00');
});
