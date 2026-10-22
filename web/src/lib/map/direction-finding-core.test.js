import test from 'node:test';
import assert from 'node:assert/strict';
import {
  destinationPoint,
  directionFindingsToGeoJSON,
  solveDirectionFindings,
  uncertaintyEllipse,
} from './direction-finding-core.js';

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

test('solveDirectionFindings finds a perpendicular crossing', () => {
  const estimates = solveDirectionFindings([
    { id: 1, mission: 'SATER-01', lat: 43, lon: 1, bearingDeg: 90, uncertaintyDeg: 5 },
    { id: 2, mission: 'SATER-01', lat: 42.9, lon: 1.1, bearingDeg: 0, uncertaintyDeg: 5 },
  ]);
  assert.equal(estimates.length, 1);
  assert.ok(Math.abs(estimates[0].lat - 43) < 0.001);
  assert.ok(Math.abs(estimates[0].lon - 1.1) < 0.001);
  assert.equal(estimates[0].coherent, true);
  assert.ok(estimates[0].geometryScore > 0.99);
});

test('solveDirectionFindings keeps missions separate', () => {
  const estimates = solveDirectionFindings([
    { mission: 'A', lat: 43, lon: 1, bearingDeg: 90, uncertaintyDeg: 5 },
    { mission: 'B', lat: 43, lon: 1, bearingDeg: 90, uncertaintyDeg: 5 },
    { mission: 'A', lat: 42.9, lon: 1.1, bearingDeg: 0, uncertaintyDeg: 5 },
  ]);
  assert.equal(estimates.length, 1);
  assert.equal(estimates[0].mission, 'A');
});

test('solveDirectionFindings flags nearly parallel geometry', () => {
  const estimates = solveDirectionFindings([
    { lat: 43, lon: 1, bearingDeg: 45, uncertaintyDeg: 5 },
    { lat: 43.01, lon: 1, bearingDeg: 46, uncertaintyDeg: 5 },
  ]);
  assert.equal(estimates.length, 1);
  assert.equal(estimates[0].poorGeometry, true);
  assert.equal(estimates[0].coherent, false);
});

test('uncertaintyEllipse closes its polygon', () => {
  const ring = uncertaintyEllipse({
    lat: 43, lon: 1, majorAxisKm: 4, minorAxisKm: 2, majorAngleDeg: 30,
  }, 12);
  assert.equal(ring.length, 13);
  assert.deepEqual(ring[0], ring.at(-1));
});
