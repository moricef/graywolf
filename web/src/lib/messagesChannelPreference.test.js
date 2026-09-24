import assert from 'node:assert/strict';
import { test } from 'node:test';
import { getThreadChannel, setThreadChannel } from './messagesChannelPreference.js';

function memoryStorage() {
  const values = new Map();
  return {
    getItem: (key) => values.get(key) ?? null,
    setItem: (key, value) => values.set(key, value),
    removeItem: (key) => values.delete(key),
  };
}

test('keeps the selected channel separately for each conversation', () => {
  const storage = memoryStorage();
  setThreadChannel('dm:F4MLV-7', 4, storage);
  setThreadChannel('dm:F4MLV-15', 1, storage);
  assert.equal(getThreadChannel('dm:F4MLV-7', storage), 4);
  assert.equal(getThreadChannel('dm:F4MLV-15', storage), 1);
  assert.equal(getThreadChannel('dm:OTHER', storage), 0);
  setThreadChannel('dm:F4MLV-7', 0, storage);
  assert.equal(getThreadChannel('dm:F4MLV-7', storage), 0);
});

test('ignores invalid stored channel identifiers', () => {
  const storage = memoryStorage();
  storage.setItem('graywolf.messages.tx-channel.dm:TEST', '-1');
  assert.equal(getThreadChannel('dm:TEST', storage), 0);
  storage.setItem('graywolf.messages.tx-channel.dm:TEST', '4.5');
  assert.equal(getThreadChannel('dm:TEST', storage), 0);
});
