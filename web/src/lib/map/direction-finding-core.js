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

function normalizeAngleDelta(degrees) {
  return ((degrees + 540) % 360) - 180;
}

function solve2x2(a11, a12, a22, b1, b2) {
  const determinant = a11 * a22 - a12 * a12;
  if (Math.abs(determinant) < 1e-12) return null;
  return {
    x: (a22 * b1 - a12 * b2) / determinant,
    y: (a11 * b2 - a12 * b1) / determinant,
    determinant,
  };
}

function missionGroups(observations) {
  const groups = new Map();
  for (const observation of observations || []) {
    if (!Number.isFinite(observation.lat) || !Number.isFinite(observation.lon) ||
        !Number.isFinite(observation.bearingDeg)) continue;
    const mission = String(observation.mission || '').trim();
    const key = mission.toLocaleLowerCase();
    if (!groups.has(key)) groups.set(key, { key, mission, observations: [] });
    groups.get(key).observations.push(observation);
  }
  return groups.values();
}

// Weighted least-squares estimate in a local tangent plane. Each observation
// contributes the line normal to its true bearing. Four iterations turn the
// angular uncertainty into a cross-track uncertainty at the current estimated
// range. This is deliberately a regional SAR solver, not a long-range geodesic
// navigator; equirectangular projection error is negligible at SATER scale.
export function solveDirectionFindings(observations) {
  const estimates = [];
  for (const group of missionGroups(observations)) {
    if (group.observations.length < 2) continue;
    const refLat = group.observations.reduce((sum, item) => sum + item.lat, 0) / group.observations.length;
    const refLon = group.observations.reduce((sum, item) => sum + item.lon, 0) / group.observations.length;
    const cosLat = Math.max(0.01, Math.cos(rad(refLat)));
    const rows = group.observations.map((item) => {
      const bearing = rad(((item.bearingDeg % 360) + 360) % 360);
      const x = rad(item.lon - refLon) * EARTH_RADIUS_KM * cosLat;
      const y = rad(item.lat - refLat) * EARTH_RADIUS_KM;
      const nx = Math.cos(bearing);
      const ny = -Math.sin(bearing);
      return {
        item, x, y, nx, ny,
        dx: Math.sin(bearing), dy: Math.cos(bearing),
        c: nx * x + ny * y,
        sigmaRad: rad(Math.max(0.5, Math.min(90, Number(item.uncertaintyDeg) || 10))),
        weight: 1,
      };
    });

    // Orientation-only geometry score: 1 is a balanced perpendicular
    // crossing; 0 is parallel. It is independent of operator uncertainty.
    let g11 = 0, g12 = 0, g22 = 0;
    for (const row of rows) {
      g11 += row.nx * row.nx;
      g12 += row.nx * row.ny;
      g22 += row.ny * row.ny;
    }
    const gTrace = g11 + g22;
    const geometryScore = gTrace > 0
      ? Math.max(0, Math.min(1, 4 * (g11 * g22 - g12 * g12) / (gTrace * gTrace)))
      : 0;

    let solution = null;
    let matrix = null;
    for (let iteration = 0; iteration < 4; iteration += 1) {
      let a11 = 0, a12 = 0, a22 = 0, b1 = 0, b2 = 0;
      for (const row of rows) {
        const w = row.weight;
        a11 += w * row.nx * row.nx;
        a12 += w * row.nx * row.ny;
        a22 += w * row.ny * row.ny;
        b1 += w * row.nx * row.c;
        b2 += w * row.ny * row.c;
      }
      solution = solve2x2(a11, a12, a22, b1, b2);
      if (!solution) break;
      matrix = { a11, a12, a22, determinant: solution.determinant };
      for (const row of rows) {
        const rangeKm = Math.max(0.25, Math.hypot(solution.x - row.x, solution.y - row.y));
        const sigmaCrossKm = Math.max(0.05, rangeKm * Math.tan(Math.min(rad(80), row.sigmaRad)));
        row.weight = 1 / (sigmaCrossKm * sigmaCrossKm);
      }
    }
    if (!solution || !matrix || matrix.determinant <= 1e-12) continue;

    let angularErrorSquared = 0;
    let forwardCount = 0;
    for (const row of rows) {
      const east = solution.x - row.x;
      const north = solution.y - row.y;
      const predicted = (deg(Math.atan2(east, north)) + 360) % 360;
      const delta = normalizeAngleDelta(predicted - row.item.bearingDeg);
      angularErrorSquared += delta * delta;
      if (east * row.dx + north * row.dy >= 0) forwardCount += 1;
    }
    const rmsAngularErrorDeg = Math.sqrt(angularErrorSquared / rows.length);
    const forwardFraction = forwardCount / rows.length;
    const poorGeometry = geometryScore < 0.08;
    const coherent = !poorGeometry && forwardFraction === 1 && rmsAngularErrorDeg <= 20;

    // The inverse weighted normal matrix is the local 1-sigma covariance.
    const covariance = {
      xx: matrix.a22 / matrix.determinant,
      xy: -matrix.a12 / matrix.determinant,
      yy: matrix.a11 / matrix.determinant,
    };
    const trace = covariance.xx + covariance.yy;
    const disc = Math.sqrt(Math.max(0,
      (covariance.xx - covariance.yy) ** 2 + 4 * covariance.xy ** 2,
    ));
    const lambdaMajor = Math.max(0, (trace + disc) / 2);
    const lambdaMinor = Math.max(0, (trace - disc) / 2);
    const majorAxisKm = Math.sqrt(5.991 * lambdaMajor); // 95%, two dimensions
    const minorAxisKm = Math.sqrt(5.991 * lambdaMinor);
    const majorAngleRad = 0.5 * Math.atan2(2 * covariance.xy, covariance.xx - covariance.yy);

    estimates.push({
      key: group.key,
      mission: group.mission,
      count: rows.length,
      lat: refLat + deg(solution.y / EARTH_RADIUS_KM),
      lon: refLon + deg(solution.x / (EARTH_RADIUS_KM * cosLat)),
      geometryScore,
      poorGeometry,
      coherent,
      forwardFraction,
      rmsAngularErrorDeg,
      majorAxisKm,
      minorAxisKm,
      majorAngleDeg: deg(majorAngleRad),
    });
  }
  return estimates;
}

export function uncertaintyEllipse(estimation, vertices = 72) {
  if (!estimation || !Number.isFinite(estimation.majorAxisKm) || !Number.isFinite(estimation.minorAxisKm)) return [];
  const angle = rad(estimation.majorAngleDeg);
  const cosLat = Math.max(0.01, Math.cos(rad(estimation.lat)));
  const coordinates = [];
  for (let i = 0; i <= vertices; i += 1) {
    const phase = 2 * Math.PI * i / vertices;
    const major = estimation.majorAxisKm * Math.cos(phase);
    const minor = estimation.minorAxisKm * Math.sin(phase);
    const east = major * Math.cos(angle) - minor * Math.sin(angle);
    const north = major * Math.sin(angle) + minor * Math.cos(angle);
    coordinates.push([
      estimation.lon + deg(east / (EARTH_RADIUS_KM * cosLat)),
      estimation.lat + deg(north / EARTH_RADIUS_KM),
    ]);
  }
  return coordinates;
}
