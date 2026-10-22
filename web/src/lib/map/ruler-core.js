import { destinationPoint } from './direction-finding-core.js';

const rad = (degrees) => degrees * Math.PI / 180;

// Surface distance and initial true bearing from the first fix to the second.
export function measureDistance(start, end) {
  const a = rad(start.lat);
  const b = rad(end.lat);
  const deltaLat = b - a;
  const deltaLon = rad(end.lon - start.lon);
  const h = Math.sin(deltaLat / 2) ** 2 +
    Math.cos(a) * Math.cos(b) * Math.sin(deltaLon / 2) ** 2;
  const distanceKm = 6371.0088 * 2 * Math.asin(Math.sqrt(Math.min(1, h)));
  const bearingDeg = distanceKm < 1e-9 ? null :
    (Math.atan2(Math.sin(deltaLon) * Math.cos(b),
      Math.cos(a) * Math.sin(b) - Math.sin(a) * Math.cos(b) * Math.cos(deltaLon)) * 180 / Math.PI + 360) % 360;
  return { distanceKm, bearingDeg };
}

export function rulerGeoJSON(start, end) {
  const features = [];
  if (start) features.push({ type: 'Feature', properties: {},
    geometry: { type: 'Point', coordinates: [start.lon, start.lat] } });
  if (start && end) {
    const { distanceKm, bearingDeg } = measureDistance(start, end);
    const count = Math.max(1, Math.min(256, Math.ceil(distanceKm / 10)));
    const coordinates = [[start.lon, start.lat]];
    for (let i = 1; i <= count; i += 1) {
      const point = i === count ? end : destinationPoint(start.lat, start.lon, bearingDeg || 0, distanceKm * i / count);
      let lon = point.lon;
      const previous = coordinates.at(-1)[0];
      while (lon - previous > 180) lon -= 360;
      while (lon - previous < -180) lon += 360;
      coordinates.push([lon, point.lat]);
    }
    features.push({ type: 'Feature', properties: {}, geometry: { type: 'LineString', coordinates } });
    features.push({ type: 'Feature', properties: {}, geometry: { type: 'Point', coordinates: coordinates.at(-1) } });
  }
  return { type: 'FeatureCollection', features };
}
