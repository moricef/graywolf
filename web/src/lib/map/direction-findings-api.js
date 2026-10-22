export function directionFindingFromApi(row) {
  if (
    !row ||
    typeof row.callsign !== 'string' ||
    !Number.isFinite(row.latitude) ||
    !Number.isFinite(row.longitude) ||
    !Number.isFinite(row.bearing_deg) ||
    !Number.isFinite(Date.parse(row.observed_at))
  ) return null;

  return {
    id: row.id,
    callsign: row.callsign,
    mission: row.mission || '',
    lat: row.latitude,
    lon: row.longitude,
    bearingDeg: row.bearing_deg,
    uncertaintyDeg: Number.isFinite(row.uncertainty_deg) ? row.uncertainty_deg : 10,
    frequencyMHz: Number.isFinite(row.frequency_mhz) ? row.frequency_mhz : 121.5,
    observedAt: row.observed_at,
    note: row.note || '',
  };
}

export function directionFindingToApi(observation) {
  return {
    callsign: (observation.callsign || '').trim().toUpperCase(),
    mission: (observation.mission || '').trim(),
    latitude: observation.lat,
    longitude: observation.lon,
    bearing_deg: observation.bearingDeg,
    uncertainty_deg: observation.uncertaintyDeg,
    frequency_mhz: observation.frequencyMHz,
    observed_at: observation.observedAt,
    note: (observation.note || '').trim(),
  };
}
