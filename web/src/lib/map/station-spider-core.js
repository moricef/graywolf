// Pure geometry helpers for expanding overlapping station markers. Kept free
// of DOM and MapLibre dependencies so the clustering/layout rules stay easy to
// test.

export const STATION_OVERLAP_PX = 24;

export function overlappingCluster(points, startId, threshold = STATION_OVERLAP_PX) {
  const byId = new Map((points ?? []).map((point) => [point.id, point]));
  if (!byId.has(startId)) return [];

  const thresholdSq = threshold * threshold;
  const found = new Set([startId]);
  const queue = [startId];
  while (queue.length) {
    const current = byId.get(queue.shift());
    for (const point of byId.values()) {
      if (found.has(point.id)) continue;
      const dx = point.x - current.x;
      const dy = point.y - current.y;
      if (dx * dx + dy * dy <= thresholdSq) {
        found.add(point.id);
        queue.push(point.id);
      }
    }
  }
  return [...found];
}

export function spiderOffsets(count, { minRadius = 38, separation = 26 } = {}) {
  if (count < 2) return [];
  const radius = Math.max(minRadius, (count * separation) / (2 * Math.PI));
  return Array.from({ length: count }, (_, index) => {
    const angle = -Math.PI / 2 + (index * 2 * Math.PI) / count;
    return {
      x: Math.round(Math.cos(angle) * radius),
      y: Math.round(Math.sin(angle) * radius),
    };
  });
}
