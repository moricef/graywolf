import test from 'node:test';
import assert from 'node:assert/strict';
import { destinationPoint, directionFindingsToGeoJSON } from './direction-finding-core.js';

test('destinationPoint follows true bearings', () => {
  const north = destinationPoint(0, 0, 0, 111.195);
  assert.ok(Math.abs(north.lat - 1) < 0.001);
  assert.ok(Math.abs(north.lon) < 0.001);
  const east = destinationPoint(0, 0, 90, 111.195);
  assert.ok(Math.abs(east.lat) < 0.001);
  assert.ok(Math.abs(east.lon - 1) < 0.001);
});

test('directionFindingsToGeoJSON keeps observation id and origin', () => {
  const result = directionFindingsToGeoJSON([{ id: 7, lat: 42.9, lon: 1.6, bearingDeg: 73.5 }]);
  assert.equal(result.features.length, 1);
  assert.equal(result.features[0].properties.id, 7);
  assert.deepEqual(result.features[0].geometry.coordinates[0], [1.6, 42.9]);
});
