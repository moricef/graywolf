import {
  directionFindingsToGeoJSON,
  solveDirectionFindings,
  uncertaintyEllipse,
} from '../direction-finding-core.js';

const SOURCE = 'gw-direction-findings';
const GLOW = 'gw-direction-findings-glow';
const LINE = 'gw-direction-findings-line';
const HIT = 'gw-direction-findings-hit';
const ELLIPSE = 'gw-direction-findings-ellipse';
const ESTIMATE = 'gw-direction-findings-estimate';
const ESTIMATE_HIT = 'gw-direction-findings-estimate-hit';
const EMPTY = { type: 'FeatureCollection', features: [] };

function mapGeoJSON(observations, estimates) {
  const collection = directionFindingsToGeoJSON(observations);
  for (const estimate of estimates) {
    const color = estimate.coherent ? '#35c779' : '#ff5b57';
    const ring = uncertaintyEllipse(estimate);
    if (ring.length) collection.features.push({
      type: 'Feature',
      geometry: { type: 'Polygon', coordinates: [ring] },
      properties: { kind: 'ellipse', key: estimate.key, color },
    });
    collection.features.push({
      type: 'Feature',
      geometry: { type: 'Point', coordinates: [estimate.lon, estimate.lat] },
      properties: { kind: 'estimate', key: estimate.key, color },
    });
  }
  // Legacy ray features predate the kind property; stamp them here so filters
  // cannot accidentally render polygons/points through line layers.
  for (const feature of collection.features) {
    if (feature.geometry.type === 'LineString') feature.properties.kind = 'bearing';
  }
  return collection;
}

export function mountDirectionFindingsLayer(map, getObservations, {
  visible = true,
  onLineClick = null,
  onEstimateClick = null,
} = {}) {
  const visibility = visible ? 'visible' : 'none';
  map.addSource(SOURCE, { type: 'geojson', data: EMPTY });
  map.addLayer({
    id: ELLIPSE, type: 'fill', source: SOURCE,
    filter: ['==', ['get', 'kind'], 'ellipse'],
    layout: { visibility },
    paint: {
      'fill-color': ['get', 'color'],
      'fill-opacity': 0.13,
      'fill-outline-color': ['get', 'color'],
    },
  });
  map.addLayer({
    id: GLOW, type: 'line', source: SOURCE,
    filter: ['==', ['get', 'kind'], 'bearing'],
    layout: { visibility, 'line-cap': 'round' },
    paint: { 'line-color': '#ff9d2e', 'line-width': 8, 'line-opacity': 0.2 },
  });
  map.addLayer({
    id: LINE, type: 'line', source: SOURCE,
    filter: ['==', ['get', 'kind'], 'bearing'],
    layout: { visibility, 'line-cap': 'round' },
    paint: { 'line-color': '#ff9d2e', 'line-width': 2.5, 'line-opacity': 0.95 },
  });
  map.addLayer({
    id: HIT, type: 'line', source: SOURCE,
    filter: ['==', ['get', 'kind'], 'bearing'],
    layout: { visibility, 'line-cap': 'round' },
    paint: { 'line-color': '#000000', 'line-width': 18, 'line-opacity': 0 },
  });
  map.addLayer({
    id: ESTIMATE, type: 'circle', source: SOURCE,
    filter: ['==', ['get', 'kind'], 'estimate'],
    layout: { visibility },
    paint: {
      'circle-radius': 7,
      'circle-color': ['get', 'color'],
      'circle-stroke-color': '#ffffff',
      'circle-stroke-width': 2,
    },
  });
  map.addLayer({
    id: ESTIMATE_HIT, type: 'circle', source: SOURCE,
    filter: ['==', ['get', 'kind'], 'estimate'],
    layout: { visibility },
    paint: { 'circle-radius': 16, 'circle-opacity': 0 },
  });

  let lastEstimates = [];
  const enter = () => { map.getCanvas().style.cursor = 'pointer'; };
  const leave = () => { map.getCanvas().style.cursor = ''; };
  const clickLine = (event) => {
    const id = Number(event.features?.[0]?.properties?.id);
    const observation = getObservations()?.find((candidate) => candidate.id === id);
    if (observation) onLineClick?.(observation, event.lngLat);
  };
  const clickEstimate = (event) => {
    const key = String(event.features?.[0]?.properties?.key ?? '');
    const estimate = lastEstimates.find((candidate) => candidate.key === key);
    if (estimate) onEstimateClick?.(estimate, event.lngLat);
  };
  map.on('mouseenter', HIT, enter);
  map.on('mouseleave', HIT, leave);
  map.on('click', HIT, clickLine);
  map.on('mouseenter', ESTIMATE_HIT, enter);
  map.on('mouseleave', ESTIMATE_HIT, leave);
  map.on('click', ESTIMATE_HIT, clickEstimate);

  return {
    refresh() {
      const observations = getObservations();
      lastEstimates = solveDirectionFindings(observations);
      map.getSource(SOURCE)?.setData(mapGeoJSON(observations, lastEstimates));
    },
    setVisible(next) {
      for (const id of [ELLIPSE, GLOW, LINE, HIT, ESTIMATE, ESTIMATE_HIT]) {
        if (map.getLayer(id)) map.setLayoutProperty(id, 'visibility', next ? 'visible' : 'none');
      }
    },
    destroy() {
      try { map.off('mouseenter', HIT, enter); } catch { /* map removed */ }
      try { map.off('mouseleave', HIT, leave); } catch { /* map removed */ }
      try { map.off('click', HIT, clickLine); } catch { /* map removed */ }
      try { map.off('mouseenter', ESTIMATE_HIT, enter); } catch { /* map removed */ }
      try { map.off('mouseleave', ESTIMATE_HIT, leave); } catch { /* map removed */ }
      try { map.off('click', ESTIMATE_HIT, clickEstimate); } catch { /* map removed */ }
      if (!map.style) return;
      for (const id of [ESTIMATE_HIT, ESTIMATE, HIT, LINE, GLOW, ELLIPSE]) {
        if (map.getLayer(id)) map.removeLayer(id);
      }
      if (map.getSource(SOURCE)) map.removeSource(SOURCE);
    },
  };
}
