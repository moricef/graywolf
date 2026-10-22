import test from 'node:test';
import assert from 'node:assert/strict';
import { measureDistance, rulerGeoJSON } from './ruler-core.js';

test('ruler measures distance and initial true bearing', () => {
  const east = measureDistance({ lat: 0, lon: 0 }, { lat: 0, lon: 1 });
  assert.ok(Math.abs(east.distanceKm - 111.195) < 0.001);
  assert.equal(east.bearingDeg, 90);
  assert.equal(measureDistance({ lat: 43, lon: 1 }, { lat: 44, lon: 1 }).bearingDeg, 0);
  assert.deepEqual(measureDistance({ lat: 43, lon: 1 }, { lat: 43, lon: 1 }), { distanceKm: 0, bearingDeg: null });
});

test('ruler follows the short path across the antimeridian', () => {
  const start = { lat: 0, lon: 179.9 };
  const end = { lat: 0, lon: -179.9 };
  assert.ok(measureDistance(start, end).distanceKm < 23);
  const line = rulerGeoJSON(start, end).features.find((feature) => feature.geometry.type === 'LineString');
  assert.ok(Math.abs(line.geometry.coordinates.at(-1)[0] - 180.1) < 1e-9);
  assert.equal(rulerGeoJSON(null, null).features.length, 0);
});
