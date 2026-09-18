import test from 'node:test';
import assert from 'node:assert/strict';
import { osmRasterStyle } from './osm-raster.js';

test('OSM raster style uses the canonical single HTTP/2 tile endpoint', () => {
  const style = osmRasterStyle();
  assert.deepEqual(style.sources.osm.tiles, [
    'https://tile.openstreetmap.org/{z}/{x}/{y}.png',
  ]);
});
