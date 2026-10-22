import { measureDistance, rulerGeoJSON } from '../ruler-core.js';

const SOURCE = 'gw-ruler';
const LAYERS = ['gw-ruler-outline', 'gw-ruler-line', 'gw-ruler-points'];

export function mountRulerLayer(map, { onMeasure, onComplete }) {
  let active = false;
  let start = null;
  let end = null;
  let down = null;
  let doubleClickEnabled = false;
  const container = map.getContainer();
  map.addSource(SOURCE, { type: 'geojson', data: rulerGeoJSON(null, null) });
  for (const [id, color, width] of [
    [LAYERS[0], '#ffffff', 5], [LAYERS[1], '#226ddd', 2.5],
  ]) map.addLayer({ id, type: 'line', source: SOURCE,
    filter: ['==', ['geometry-type'], 'LineString'],
    paint: { 'line-color': color, 'line-width': width },
  });
  map.addLayer({ id: LAYERS[2], type: 'circle', source: SOURCE,
    filter: ['==', ['geometry-type'], 'Point'],
    paint: { 'circle-radius': 5, 'circle-color': '#226ddd',
      'circle-stroke-color': '#ffffff', 'circle-stroke-width': 2 },
  });

  function refresh() {
    map.getSource(SOURCE)?.setData(rulerGeoJSON(start, end));
    onMeasure?.(start && end ? measureDistance(start, end) : null, !!start);
  }
  function stop() {
    if (active && doubleClickEnabled) map.doubleClickZoom.enable();
    active = false;
    container.classList.remove('gw-ruler-active');
  }
  function pointerDown(event) { down = { x: event.clientX, y: event.clientY }; }
  function click(event) {
    if (!active || event.target.closest('.maplibregl-control-container, .maplibregl-popup')) return;
    // Capture before station markers and GL feature handlers so measuring
    // across a station or an estimate does not open an unrelated popup.
    event.stopImmediatePropagation();
    event.preventDefault();
    if (down && Math.hypot(event.clientX - down.x, event.clientY - down.y) > 5) return;
    const bounds = container.getBoundingClientRect();
    const point = map.unproject([event.clientX - bounds.left, event.clientY - bounds.top]);
    const fix = { lat: point.lat, lon: point.lng };
    if (!start) {
      start = fix;
      end = null;
      refresh();
    } else {
      end = fix;
      refresh();
      stop();
      onComplete?.();
    }
  }
  function move(event) {
    if (!active || !start) return;
    end = { lat: event.lngLat.lat, lon: event.lngLat.lng };
    refresh();
  }
  container.addEventListener('pointerdown', pointerDown, true);
  container.addEventListener('click', click, true);
  map.on('mousemove', move);
  return {
    start() {
      stop();
      start = null;
      end = null;
      down = null;
      doubleClickEnabled = map.doubleClickZoom.isEnabled();
      map.doubleClickZoom.disable();
      active = true;
      container.classList.add('gw-ruler-active');
      refresh();
    },
    clear() { stop(); start = null; end = null; refresh(); },
    destroy() {
      stop();
      container.removeEventListener('pointerdown', pointerDown, true);
      container.removeEventListener('click', click, true);
      map.off('mousemove', move);
      if (!map.style) return;
      for (const id of [...LAYERS].reverse()) if (map.getLayer(id)) map.removeLayer(id);
      if (map.getSource(SOURCE)) map.removeSource(SOURCE);
    },
  };
}
