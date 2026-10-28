// Remote values advertised in a station's capability bulletin, not local counters.
export function igateRows(report) {
  const e = report?.entries;
  if (!e || !Object.hasOwn(e, 'IGATE')) return [];
  const rows = [];
  for (const [key, label] of [
    ['MSG_CNT', 'Messages gated'], ['LOC_CNT', 'Local stations'],
    ['DIR_CNT', 'Direct stations'], ['RF_CNT', 'RF stations'],
  ]) {
    if (/^\d+$/.test(e[key] ?? '')) rows.push([label, e[key], key]);
  }
  if (e.DX) rows.push(['DX', formatDX(e.DX), 'DX']);
  return rows;
}

export function formatDX(value) {
  // Accept Graywolf ASCII "deg" and other software's degree sign, with
  // optional count prefix (e.g. DX=1*CALL). Keep unknown formats readable.
  const match = /^(?:\d+\*)?([A-Za-z0-9-]+)\((\d+(?:\.\d+)?)km@(\d+(?:\.\d+)?)(?:deg|°|\uFFFD)\)$/.exec(value);
  if (!match) return value;
  return `${match[1]} · ${match[2]} km · ${match[3]}°`;
}

export function receiverDXRows(report) {
  if (!report) return [];
  const rows = [['Direct stations (30 min)', String(report.direct_stations), 'RXT']];
  const dx = report.dx;
  if (dx && Number.isFinite(dx.distance_km) && Number.isFinite(dx.bearing_deg)) {
    rows.push(['DX', `${dx.callsign} · ${dx.distance_km.toFixed(1)} km · ${Math.round(dx.bearing_deg) % 360}°`, 'RXT']);
    if (Number.isFinite(dx.rssi_dbm)) rows.push(['RSSI', `${dx.rssi_dbm} dBm`, 'RXT']);
    if (Number.isFinite(dx.snr_db)) rows.push(['SNR', `${dx.snr_db} dB`, 'RXT']);
    if (Number.isFinite(dx.frequency_error_hz)) rows.push(['Frequency offset', `${dx.frequency_error_hz} Hz`, 'RXT']);
  } else {
    rows.push(['DX', 'No qualifying positioned link', 'RXT']);
  }
  return rows;
}
