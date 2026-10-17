import test from 'node:test';
import assert from 'node:assert/strict';

import {
  closeStationWindow,
  moveStationWindow,
  pinStationWindow,
  refreshStationWindows,
  toggleStationWindow,
} from './station-windows-core.js';

test('several station windows can be pinned and cascaded', () => {
  let windows = pinStationWindow([], { callsign: 'F4MLV-2' }, { width: 1200, height: 800 });
  windows = pinStationWindow(windows, { callsign: 'F4MLV-15' }, { width: 1200, height: 800 });
  assert.equal(windows.length, 2);
  assert.notEqual(windows[0].x, windows[1].x);
  assert.notEqual(windows[0].y, windows[1].y);
});

test('pinning an existing station refreshes and restores its window', () => {
  let windows = pinStationWindow([], { callsign: 'WX1', last_heard: 'old' });
  windows = toggleStationWindow(windows, 'WX1');
  windows = pinStationWindow(windows, { callsign: 'WX1', last_heard: 'new' });
  assert.equal(windows.length, 1);
  assert.equal(windows[0].snapshot.last_heard, 'new');
  assert.equal(windows[0].minimized, false);
});

test('dragging stays inside the visible map', () => {
  let windows = pinStationWindow([], { callsign: 'WX1' }, { width: 1000, height: 700 });
  windows = moveStationWindow(windows, 'WX1', -200, 900, { width: 1000, height: 700 });
  assert.equal(windows[0].x, 12);
  assert.equal(windows[0].y, 648);
});

test('closing removes only the selected station window', () => {
  let windows = pinStationWindow([], { callsign: 'WX1' });
  windows = pinStationWindow(windows, { callsign: 'WX2' });
  windows = closeStationWindow(windows, 'WX1');
  assert.deepEqual(windows.map((card) => card.callsign), ['WX2']);
});

test('live station updates replace the retained snapshot', () => {
  let windows = pinStationWindow([], { callsign: 'WX1', last_heard: 'old' });
  windows = refreshStationWindows(
    windows,
    new Map([['WX1', { callsign: 'WX1', last_heard: 'new' }]])
  );
  assert.equal(windows[0].snapshot.last_heard, 'new');
});

test('an equivalent proxied station does not trigger a refresh loop', () => {
  const snapshot = { callsign: 'WX1', last_heard: 'same' };
  const windows = pinStationWindow([], snapshot);
  const refreshed = refreshStationWindows(
    windows,
    new Map([['WX1', { callsign: 'WX1', last_heard: 'same' }]])
  );
  assert.equal(refreshed, windows);
});
