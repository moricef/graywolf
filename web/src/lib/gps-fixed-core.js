// Pure logic for filling the fixed-coordinate GPS form from the current
// station position (GH #621). Kept free of Svelte runes and fetch so it
// runs under `node --test`; the fetch + form glue lives in routes/Gps.svelte.

// canReadGps reports whether Read GPS can do anything useful given the
// SAVED position source. Reading only makes sense while a receiver is
// the saved source: with Fixed Coordinate saved no receiver is running,
// and the fixed publisher keeps the saved coordinate in the position
// cache re-stamped every 30s, so /api/position hands the saved values
// straight back labelled source "gps". Copying those into the form
// would be a no-op dressed up as a GPS read, and no response-side check
// can tell them apart from a live fix — the gate has to be on the saved
// source, before the fetch happens.
export function canReadGps(savedSource) {
  return savedSource === 'serial' || savedSource === 'gpsd';
}

// The position cache never invalidates a fix: if the receiver loses its
// fix or is unplugged, /api/position keeps returning the last fix as
// current, however old it is. A fix older than this is not a position
// worth copying into the form.
const MAX_FIX_AGE_MS = 60_000;

// formatCoord renders a coordinate rounded to 6 decimal places
// (~0.1 m, well beyond APRS precision) instead of the raw float, which
// can surface in the field as e.g. 35.08441666666667.
function formatCoord(value) {
  return String(Number(value.toFixed(6)));
}

// fixedCoordsFromPosition reduces a GET /api/position response to the
// string values the fixed-coordinate fields should show, or null when
// the response holds no current GPS fix to copy. The checks:
//
//   - source must be "gps": a "fixed" response is only the beacon
//     fallback (a beacon's static lat/lon when there is no GPS at all),
//     never a receiver fix. A saved Fixed Coordinate does NOT come back
//     this way — it re-enters the cache and reports as "gps", which is
//     the case canReadGps above gates on the saved source.
//   - the fix must be fresh: pos.timestamp (UTC, seconds precision)
//     must parse and be no more than ~60s before `now`. A missing or
//     garbled timestamp proves nothing about freshness, so it rejects.
//   - lat/lon must be finite numbers.
//
// Altitude comes back as null when the fix has none, so the caller can
// leave the optional altitude field untouched.
export function fixedCoordsFromPosition(pos, now = Date.now()) {
  if (!pos || typeof pos !== 'object') return null;
  if (pos.valid !== true || pos.source !== 'gps') return null;
  if (!Number.isFinite(pos.lat) || !Number.isFinite(pos.lon)) return null;
  const fixTime = typeof pos.timestamp === 'string' ? Date.parse(pos.timestamp) : NaN;
  if (!Number.isFinite(fixTime) || now - fixTime > MAX_FIX_AGE_MS) return null;
  return {
    lat: formatCoord(pos.lat),
    lon: formatCoord(pos.lon),
    alt: pos.has_alt && Number.isFinite(pos.alt_m) ? String(pos.alt_m) : null,
  };
}
