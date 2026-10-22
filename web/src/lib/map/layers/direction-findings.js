import { directionFindingsToGeoJSON } from '../direction-finding-core.js';

const SOURCE = 'gw-direction-findings';
const GLOW = 'gw-direction-findings-glow';
const LINE = 'gw-direction-findings-line';
const HIT = 'gw-direction-findings-hit';
const EMPTY = { type: 'FeatureCollection', features: [] };

export function mountDirectionFindingsLayer(map, getObservations, { visible = true, onLineClick = null } = {}) {
  const visibility = visible ? 'visible' : 'none';
  map.addSource(SOURCE, { type: 'geojson', data: EMPTY });
  map.addLayer({
    id: GLOW, type: 'line', source: SOURCE,
    layout: { visibility, 'line-cap': 'round' },
    paint: { 'line-color': '#ff9d2e', 'line-width': 8, 'line-opacity': 0.2 },
  });
  map.addLayer({
    id: LINE, type: 'line', source: SOURCE,
    layout: { visibility, 'line-cap': 'round' },
    paint: { 'line-color': '#ff9d2e', 'line-width': 2.5, 'line-opacity': 0.95 },
  });
  map.addLayer({
    id: HIT, type: 'line', source: SOURCE,
    layout: { visibility, 'line-cap': 'round' },
    paint: { 'line-color': '#000000', 'line-width': 18, 'line-opacity': 0 },
  });

  const enter = () => { map.getCanvas().style.cursor = 'pointer'; };
  const leave = () => { map.getCanvas().style.cursor = ''; };
  const click = (event) => {
    const id = Number(event.features?.[0]?.properties?.id);
    const observation = getObservations()?.find((candidate) => candidate.id === id);
    if (observation) onLineClick?.(observation, event.lngLat);
  };
  map.on('mouseenter', HIT, enter);
  map.on('mouseleave', HIT, leave);
  map.on('click', HIT, click);

  return {
    refresh() {
      map.getSource(SOURCE)?.setData(directionFindingsToGeoJSON(getObservations()));
    },
    setVisible(next) {
      for (const id of [GLOW, LINE, HIT]) {
        if (map.getLayer(id)) map.setLayoutProperty(id, 'visibility', next ? 'visible' : 'none');
      }
    },
    destroy() {
      try { map.off('mouseenter', HIT, enter); } catch { /* map removed */ }
      try { map.off('mouseleave', HIT, leave); } catch { /* map removed */ }
      try { map.off('click', HIT, click); } catch { /* map removed */ }
      if (!map.style) return;
      for (const id of [HIT, LINE, GLOW]) if (map.getLayer(id)) map.removeLayer(id);
      if (map.getSource(SOURCE)) map.removeSource(SOURCE);
    },
  };
}
