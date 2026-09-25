import test from 'node:test';
import assert from 'node:assert/strict';
import {
  BEACON_TYPE_OPTIONS,
  beaconExtraFields,
  beaconTypeUsesPosition,
  validateCustomBeacon,
  validateWeatherBeacon,
} from './beaconForm.js';

test('beacon type choices expose Custom in the UI', () => {
  assert.deepEqual(BEACON_TYPE_OPTIONS.map(({ value }) => value), [
    'position', 'object', 'tracker', 'weather', 'custom',
  ]);
});

test('custom beacons do not expose position controls', () => {
  assert.equal(beaconTypeUsesPosition('custom'), false);
  assert.equal(beaconTypeUsesPosition('position'), true);
  assert.equal(beaconTypeUsesPosition('object'), true);
  assert.equal(beaconTypeUsesPosition('tracker'), true);
  assert.equal(beaconTypeUsesPosition('weather'), true);
});

test('custom info and comment command survive edit-form hydration', () => {
  assert.deepEqual(beaconExtraFields({
    custom_info: '>WX:test',
    comment_cmd: '/usr/local/bin/weather-comment --short',
  }), {
    custom_info: '>WX:test',
    comment_cmd: '/usr/local/bin/weather-comment --short',
    weather_source: 'wxnow_file',
    weather_path: '',
  });
  assert.deepEqual(beaconExtraFields({}), {
    custom_info: '', comment_cmd: '', weather_source: 'wxnow_file', weather_path: '',
  });
});

test('weather beacons require the WxNow.txt file source', () => {
  assert.match(validateWeatherBeacon({
    type: 'weather', weather_source: 'wxnow_file', weather_path: '   ',
  }), /path is required/);
  assert.equal(validateWeatherBeacon({
    type: 'weather', weather_source: 'wxnow_file', weather_path: '/var/lib/weather/WxNow.txt',
  }), '');
  assert.equal(validateWeatherBeacon({ type: 'position' }), '');
});

test('custom beacons require a raw APRS information field', () => {
  assert.match(validateCustomBeacon({ type: 'custom', custom_info: '   ' }), /required/);
  assert.equal(validateCustomBeacon({ type: 'custom', custom_info: '>status' }), '');
  assert.equal(validateCustomBeacon({ type: 'position', custom_info: '' }), '');
});
