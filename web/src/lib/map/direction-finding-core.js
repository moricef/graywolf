const EARTH_RADIUS_KM = 6371.0088;

const rad = (degrees) => degrees * Math.PI / 180;
const deg = (radians) => radians * 180 / Math.PI;

// Destination on a spherical Earth. The displayed segment has a finite
// length, but semantically represents a ray from the reporting station.
export function destinationPoint(lat, lon, bearingDeg, distanceKm = 200) {
  const angularDistance = distanceKm / EARTH_RADIUS_KM;
  const latitude = rad(lat);
  const longitude = rad(lon);
  const bearing = rad(bearingDeg);
  const destinationLatitude = Math.asin(
    Math.sin(latitude) * Math.cos(angularDistance) +
    Math.cos(latitude) * Math.sin(angularDistance) * Math.cos(bearing),
  );
  const destinationLongitude = longitude + Math.atan2(
    Math.sin(bearing) * Math.sin(angularDistance) * Math.cos(latitude),
    Math.cos(angularDistance) - Math.sin(latitude) * Math.sin(destinationLatitude),
  );
  return {
    lat: deg(destinationLatitude),
    lon: ((deg(destinationLongitude) + 540) % 360) - 180,
  };
}

export function directionFindingsToGeoJSON(observations, distanceKm = 200) {
  const features = [];
  for (const observation of observations || []) {
    if (!Number.isFinite(observation.lat) || !Number.isFinite(observation.lon) ||
        !Number.isFinite(observation.bearingDeg)) continue;
    const end = destinationPoint(
      observation.lat, observation.lon, observation.bearingDeg, distanceKm,
    );
    features.push({
      type: 'Feature',
      geometry: {
        type: 'LineString',
        coordinates: [[observation.lon, observation.lat], [end.lon, end.lat]],
      },
      properties: { id: observation.id },
    });
  }
  return { type: 'FeatureCollection', features };
}
