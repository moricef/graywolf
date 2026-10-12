import { test } from 'node:test';
import assert from 'node:assert/strict';
import { canReadGps, fixedCoordsFromPosition } from './gps-fixed-core.js';

// fixedCoordsFromPosition checks fix freshness against an explicit
// `now` so the cases below are deterministic.
const NOW = Date.parse('2026-10-08T12:00:00Z');
const FRESH = '2026-10-08T11:59:45Z'; // 15s old
const STALE = '2026-10-08T11:57:00Z'; // 3 minutes old

test('canReadGps allows a saved serial or GPSD source', () => {
  assert.equal(canReadGps('serial'), true);
  assert.equal(canReadGps('gpsd'), true);
});

test('canReadGps refuses a saved fixed coordinate or no saved source', () => {
  assert.equal(canReadGps('fixed'), false);
  assert.equal(canReadGps('none'), false);
  assert.equal(canReadGps(''), false);
  assert.equal(canReadGps(null), false);
  assert.equal(canReadGps(undefined), false);
});

test('fixedCoordsFromPosition fills lat, lon and alt from a GPS fix', () => {
  const pos = { valid: true, source: 'gps', timestamp: FRESH, lat: 35.0844, lon: -106.6504, alt_m: 1500, has_alt: true };
  assert.deepEqual(fixedCoordsFromPosition(pos, NOW), { lat: '35.0844', lon: '-106.6504', alt: '1500' });
});

test('fixedCoordsFromPosition rounds coordinates to 6 decimal places', () => {
  const pos = { valid: true, source: 'gps', timestamp: FRESH, lat: 35.08441666666667, lon: -106.65040449999999, has_alt: false };
  assert.deepEqual(fixedCoordsFromPosition(pos, NOW), { lat: '35.084417', lon: '-106.650404', alt: null });
});

test('fixedCoordsFromPosition leaves altitude unset when the fix has none', () => {
  const pos = { valid: true, source: 'gps', timestamp: FRESH, lat: 35.0844, lon: -106.6504, has_alt: false };
  assert.deepEqual(fixedCoordsFromPosition(pos, NOW), { lat: '35.0844', lon: '-106.6504', alt: null });
});

test('fixedCoordsFromPosition rejects the beacon fallback reporting as fixed', () => {
  // source "fixed" from /api/position is the beacon's static lat/lon,
  // used when there is no GPS at all — it is NOT a saved Fixed
  // Coordinate config reporting itself. That case re-enters the cache
  // and reports as "gps"; canReadGps gates it on the saved source.
  const pos = { valid: true, source: 'fixed', timestamp: FRESH, lat: 35.0844, lon: -106.6504, alt_m: 1500, has_alt: true };
  assert.equal(fixedCoordsFromPosition(pos, NOW), null);
});

test('fixedCoordsFromPosition rejects a fix more than a minute old', () => {
  // The position cache never invalidates: once the receiver loses its
  // fix or is unplugged, the last fix keeps being reported as current.
  const pos = { valid: true, source: 'gps', timestamp: STALE, lat: 35.0844, lon: -106.6504, has_alt: false };
  assert.equal(fixedCoordsFromPosition(pos, NOW), null);
});

test('fixedCoordsFromPosition accepts a fix just inside the freshness limit', () => {
  const pos = { valid: true, source: 'gps', timestamp: '2026-10-08T11:59:00Z', lat: 35.0844, lon: -106.6504, has_alt: false };
  assert.notEqual(fixedCoordsFromPosition(pos, NOW), null);
});

test('fixedCoordsFromPosition rejects a missing or garbled timestamp', () => {
  // Freshness cannot be proven without a parseable timestamp.
  const base = { valid: true, source: 'gps', lat: 35.0844, lon: -106.6504, has_alt: false };
  assert.equal(fixedCoordsFromPosition(base, NOW), null);
  assert.equal(fixedCoordsFromPosition({ ...base, timestamp: 'not a date' }, NOW), null);
});

test('fixedCoordsFromPosition rejects no position and an invalid fix', () => {
  assert.equal(fixedCoordsFromPosition({ valid: false, source: 'none' }, NOW), null);
  assert.equal(fixedCoordsFromPosition({ valid: false, source: 'gps', timestamp: FRESH, lat: 35, lon: -106 }, NOW), null);
});

test('fixedCoordsFromPosition tolerates missing or garbled input', () => {
  assert.equal(fixedCoordsFromPosition(null), null);
  assert.equal(fixedCoordsFromPosition(undefined), null);
  assert.equal(fixedCoordsFromPosition('nope'), null);
  assert.equal(fixedCoordsFromPosition({ valid: true, source: 'gps' }, NOW), null);
  assert.equal(fixedCoordsFromPosition({ valid: true, source: 'gps', timestamp: FRESH, lat: '35', lon: -106 }, NOW), null);
});
