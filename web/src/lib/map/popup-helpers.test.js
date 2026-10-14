import test from 'node:test';
import assert from 'node:assert/strict';

import {
  cardinal,
  dewPointF,
  formatWeatherRows,
  formatWindDirection,
  windChillF,
} from './weather-presentation.js';

test('wind direction normalizes degrees and includes the cardinal point', () => {
  assert.equal(cardinal(360), 'N');
  assert.equal(cardinal(-90), 'W');
  assert.equal(formatWindDirection(270), '270° W');
});

test('derived weather values use standard surface formulas', () => {
  assert.ok(Math.abs(dewPointF(68, 60) - 53.6) < 0.2);
  assert.ok(Math.abs(windChillF(30, 10) - 21.2) < 0.2);
  assert.equal(windChillF(60, 10), null);
  assert.equal(windChillF(30, 2), null);
});

test('weather rows honor metric units and expose rain since midnight', () => {
  const rows = Object.fromEntries(
    formatWeatherRows(
      {
        temp_f: 68,
        humidity: 60,
        wind_mph: 10,
        wind_dir: 270,
        pressure_mb: 1013.2,
        rain_today_in: 0.3,
      },
      true
    )
  );

  assert.equal(rows.Temperature, '20.0°C');
  assert.equal(rows.Wind, '16.1 km/h · 270° W');
  assert.equal(rows.Pressure, '1013.2 hPa');
  assert.equal(rows['Rain today'], '7.6 mm');
  assert.equal(rows['Dew point'], '12.0°C');
});

test('weather rows honor imperial pressure and rainfall', () => {
  const rows = Object.fromEntries(
    formatWeatherRows({ pressure_mb: 1013.25, rain_today_in: 0.3 }, false)
  );
  assert.equal(rows.Pressure, '29.92 inHg');
  assert.equal(rows['Rain today'], '0.30″');
});
