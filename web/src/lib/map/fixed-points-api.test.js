import test from 'node:test';
import assert from 'node:assert/strict';
import { fixedPointFromApi, fixedPointToApi } from './fixed-points-api.js';

test('fixedPointFromApi maps server fields to the layer shape', () => {
  const p = fixedPointFromApi({
    id: 7, name: 'Aid 3', symbol_table: '/', symbol: 'a', overlay: '',
    latitude: 37.5, longitude: -122.0,
  });
  assert.deepEqual(p, {
    id: 7, name: 'Aid 3', table: '/', symbol: 'a', overlay: '', lat: 37.5, lon: -122.0, operatorStation: false,
  });
});

test('fixedPointFromApi drops malformed records (null)', () => {
  assert.equal(fixedPointFromApi(null), null);
  assert.equal(fixedPointFromApi({ id: 1, name: 'x', latitude: 'nope', longitude: 2 }), null);
});

test('fixedPointToApi maps the dialog result to the request body', () => {
  const body = fixedPointToApi({ name: 'Aid 3', table: '/', symbol: 'a', overlay: '', lat: 37.5, lon: -122.0 });
  assert.deepEqual(body, {
    name: 'Aid 3', symbol_table: '/', symbol: 'a', overlay: '', latitude: 37.5, longitude: -122.0, operator_station: false,
  });
});

test('manual station identity survives API conversion', () => {
  const body = fixedPointToApi({ name: 'F4MLV-16', lat: 43, lon: 1.3, operatorStation: true });
  assert.equal(body.operator_station, true);
  const station = fixedPointFromApi({ id: 12, ...body });
  assert.equal(station.operatorStation, true);
  assert.equal(station.name, 'F4MLV-16');
});
