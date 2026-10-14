export const KMH_PER_MPH = 1.60934;
const MM_PER_IN = 25.4;
const INHG_PER_MB = 0.0295299830714;

export function cardinal(deg) {
  const dirs = ['N', 'NE', 'E', 'SE', 'S', 'SW', 'W', 'NW'];
  const normalized = ((Number(deg) % 360) + 360) % 360;
  return dirs[Math.round(normalized / 45) % 8];
}

export function formatWindDirection(deg) {
  if (!Number.isFinite(Number(deg))) return '';
  const normalized = ((Number(deg) % 360) + 360) % 360;
  return `${Math.round(normalized)}° ${cardinal(normalized)}`;
}

// Magnus approximation, accurate enough for surface weather observations.
// APRS carries temperature in Fahrenheit, so this helper returns Fahrenheit
// to keep all derived values in the wire unit until presentation time.
export function dewPointF(tempF, humidityPct) {
  const tF = Number(tempF);
  const rh = Number(humidityPct);
  if (!Number.isFinite(tF) || !Number.isFinite(rh) || rh <= 0 || rh > 100) return null;
  const tC = ((tF - 32) * 5) / 9;
  const alpha = Math.log(rh / 100) + (17.625 * tC) / (243.04 + tC);
  const dewC = (243.04 * alpha) / (17.625 - alpha);
  return (dewC * 9) / 5 + 32;
}

// US National Weather Service wind-chill formula. Outside its documented
// domain (<= 50 F and wind > 3 mph), there is no meaningful wind-chill value.
export function windChillF(tempF, windMph) {
  const t = Number(tempF);
  const v = Number(windMph);
  if (!Number.isFinite(t) || !Number.isFinite(v) || t > 50 || v <= 3) return null;
  const v016 = Math.pow(v, 0.16);
  return 35.74 + 0.6215 * t - 35.75 * v016 + 0.4275 * t * v016;
}

function formatTemperature(tempF, isMetric) {
  const value = isMetric ? ((tempF - 32) * 5) / 9 : tempF;
  return `${value.toFixed(1)}°${isMetric ? 'C' : 'F'}`;
}

function formatRain(inches, isMetric) {
  return isMetric ? `${(inches * MM_PER_IN).toFixed(1)} mm` : `${inches.toFixed(2)}″`;
}

// Turns a WeatherDTO into [label, value] pairs for the station popup.
export function formatWeatherRows(wx, isMetric) {
  if (!wx) return [];
  const rows = [];
  if (wx.temp_f != null) {
    rows.push(['Temperature', formatTemperature(wx.temp_f, isMetric)]);
  }
  if (wx.wind_mph != null) {
    const s = isMetric ? wx.wind_mph * KMH_PER_MPH : wx.wind_mph;
    let v = `${s.toFixed(1)} ${isMetric ? 'km/h' : 'mph'}`;
    if (wx.wind_dir != null) v += ` · ${formatWindDirection(wx.wind_dir)}`;
    rows.push(['Wind', v]);
  }
  if (wx.gust_mph != null) {
    const g = isMetric ? wx.gust_mph * KMH_PER_MPH : wx.gust_mph;
    rows.push(['Gust', `${g.toFixed(1)} ${isMetric ? 'km/h' : 'mph'}`]);
  }
  if (wx.humidity != null) {
    rows.push(['Humidity', `${wx.humidity}%`]);
  }
  if (wx.pressure_mb != null) {
    rows.push([
      'Pressure',
      isMetric
        ? `${wx.pressure_mb.toFixed(1)} hPa`
        : `${(wx.pressure_mb * INHG_PER_MB).toFixed(2)} inHg`,
    ]);
  }
  const dew = dewPointF(wx.temp_f, wx.humidity);
  if (dew != null) rows.push(['Dew point', formatTemperature(dew, isMetric)]);
  const chill = windChillF(wx.temp_f, wx.wind_mph);
  if (chill != null) rows.push(['Wind chill', formatTemperature(chill, isMetric)]);
  if (wx.rain_1h_in != null) rows.push(['Rain 1h', formatRain(wx.rain_1h_in, isMetric)]);
  if (wx.rain_24h_in != null) rows.push(['Rain 24h', formatRain(wx.rain_24h_in, isMetric)]);
  if (wx.rain_today_in != null) rows.push(['Rain today', formatRain(wx.rain_today_in, isMetric)]);
  if (wx.snow_24h_in != null) rows.push(['Snow 24h', formatRain(wx.snow_24h_in, isMetric)]);
  if (wx.luminosity_wm2 != null) rows.push(['Solar', `${wx.luminosity_wm2} W/m²`]);
  return rows;
}
