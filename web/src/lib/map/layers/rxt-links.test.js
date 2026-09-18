import test from 'node:test';
import assert from 'node:assert/strict';
import { linksToGeoJSON, mountRXTLinksLayer } from './rxt-links.js';

test('linksToGeoJSON omits unresolved and expired links and ages colors', () => {
  const now = Date.parse('2026-09-18T12:30:00Z');
  const base = { from: 'A', to: 'B', from_position: { lat: 42, lon: 1 }, to_position: { lat: 43, lon: 2 } };
  const fc = linksToGeoJSON([
    { ...base, observed_at: '2026-09-18T12:25:00Z' },
    { ...base, from: 'C', observed_at: '2026-09-18T12:15:00Z' },
    { ...base, from: 'D', observed_at: '2026-09-18T11:59:00Z' },
    { ...base, from: 'E', from_position: null, observed_at: '2026-09-18T12:29:00Z' },
  ], now);
  assert.equal(fc.features.length, 2);
  assert.equal(fc.features[0].properties.color, '#2fbf71');
  assert.equal(fc.features[1].properties.color, '#e58b2a');
});

test('RXT layer teardown is safe after MapLibre has removed its style', () => {
  const sources = new Map();
  const layers = new Map();
  const map = {
    style: {},
    addSource(id, source) { sources.set(id, source); },
    getSource(id) { return sources.get(id); },
    removeSource(id) { sources.delete(id); },
    addLayer(layer) { layers.set(layer.id, layer); },
    getLayer(id) { return layers.get(id); },
    removeLayer(id) { layers.delete(id); },
    on() {},
    off() {},
    getCanvas() { return { style: {} }; },
  };
  const mounted = mountRXTLinksLayer(map);
  map.style = undefined;
  map.getLayer = () => { throw new TypeError('style already removed'); };
  assert.doesNotThrow(() => mounted.destroy());
});
