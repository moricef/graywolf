import test from 'node:test';
import assert from 'node:assert/strict';
import { keyLengthText, keyLengthWrong, fingerprintText } from './keyText.js';

test('key length text counts trimmed characters against 43', () => {
  assert.equal(keyLengthText(''), '');
  assert.equal(keyLengthText('a'.repeat(43)), '43/43 characters');
  assert.equal(keyLengthText(' ' + 'a'.repeat(40) + ' '), '40/43 characters — a key is exactly 43 characters');
  assert.equal(keyLengthWrong('a'.repeat(40)), true);
  assert.equal(keyLengthWrong('a'.repeat(43)), false);
  assert.equal(keyLengthWrong(''), false);
});

test('fingerprint text names the value to compare with the iGate', () => {
  assert.equal(fingerprintText(''), '');
  assert.match(fingerprintText('187cbf'), /187cbf/);
});
