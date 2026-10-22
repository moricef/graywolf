// Port of toMaidenhead from LiveMap.svelte:40-51 -- Maidenhead grid
// locator (six characters by default, optionally eight for an extended square).

export function toMaidenhead(lat, lon, precision = 6) {
  lon += 180;
  lat += 90;
  const locator = (
    String.fromCharCode(65 + Math.floor(lon / 20)) +
    String.fromCharCode(65 + Math.floor(lat / 10)) +
    Math.floor((lon % 20) / 2) +
    Math.floor(lat % 10) +
    String.fromCharCode(97 + Math.floor((lon % 2) * 12)) +
    String.fromCharCode(97 + Math.floor((lat % 1) * 24))
  );
  if (precision !== 8) return locator;
  // Each subsquare is divided into ten longitude and ten latitude cells.
  // The tiny epsilon avoids losing an exact boundary to floating-point noise.
  return locator +
    (Math.floor(lon * 120 + 1e-9) % 10) +
    (Math.floor(lat * 240 + 1e-9) % 10);
}
