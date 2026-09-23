import test from 'node:test';
import assert from 'node:assert/strict';
import {
  overlappingCluster,
  spiderOffsets,
  STATION_OVERLAP_PX,
} from './station-spider-core.js';

test('overlappingCluster includes transitive overlaps but excludes distant markers', () => {
  const points = [
    { id: 'A', x: 0, y: 0 },
    { id: 'B', x: STATION_OVERLAP_PX, y: 0 },
    { id: 'C', x: STATION_OVERLAP_PX * 2, y: 0 },
    { id: 'D', x: 200, y: 200 },
  ];
  assert.deepEqual(overlappingCluster(points, 'A').sort(), ['A', 'B', 'C']);
  assert.deepEqual(overlappingCluster(points, 'D'), ['D']);
  assert.deepEqual(overlappingCluster(points, 'missing'), []);
});

test('spiderOffsets expands markers evenly around their real position', () => {
  assert.deepEqual(spiderOffsets(1), []);
  const offsets = spiderOffsets(4);
  assert.equal(offsets.length, 4);
  assert.deepEqual(offsets, [
    { x: 0, y: -38 },
    { x: 38, y: 0 },
    { x: 0, y: 38 },
    { x: -38, y: 0 },
  ]);
});
