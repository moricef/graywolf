import test from 'node:test';
import assert from 'node:assert/strict';
import { directionFindingFromApi, directionFindingToApi } from './direction-findings-api.js';

test('directionFindingFromApi maps persisted fields', () => {
  assert.deepEqual(directionFindingFromApi({
    id: 3, callsign: 'F4AAA-9', mission: 'SATER-01', latitude: 42.9,
    longitude: 1.6, bearing_deg: 73.5, uncertainty_deg: 8,
    frequency_mhz: 121.5, observed_at: '2026-10-22T14:30:00Z', note: 'clear',
  }), {
    id: 3, callsign: 'F4AAA-9', mission: 'SATER-01', lat: 42.9,
    lon: 1.6, bearingDeg: 73.5, uncertaintyDeg: 8,
    frequencyMHz: 121.5, observedAt: '2026-10-22T14:30:00Z', note: 'clear',
  });
});

test('directionFindingFromApi rejects malformed observations', () => {
  assert.equal(directionFindingFromApi(null), null);
  assert.equal(directionFindingFromApi({ callsign: 'X', latitude: 1, longitude: 2, bearing_deg: 3 }), null);
});

test('directionFindingToApi normalizes callsign and text', () => {
  assert.deepEqual(directionFindingToApi({
    callsign: ' f4aaa-9 ', mission: ' SATER-01 ', lat: 42.9, lon: 1.6,
    bearingDeg: 73.5, uncertaintyDeg: 8, frequencyMHz: 121.5,
    observedAt: '2026-10-22T14:30:00Z', note: ' clear ',
  }), {
    callsign: 'F4AAA-9', mission: 'SATER-01', latitude: 42.9, longitude: 1.6,
    bearing_deg: 73.5, uncertainty_deg: 8, frequency_mhz: 121.5,
    observed_at: '2026-10-22T14:30:00Z', note: 'clear',
  });
});
