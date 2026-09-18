const SOURCE = 'gw-rxt-links';
const GLOW = 'gw-rxt-links-glow';
const LINES = 'gw-rxt-links-lines';
const EMPTY = { type: 'FeatureCollection', features: [] };

export function linksToGeoJSON(links, now = Date.now()) {
  const features = [];
  for (const link of links ?? []) {
    const a = link.from_position;
    const b = link.to_position;
    if (!a || !b) continue;
    const observed = Date.parse(link.observed_at);
    if (!Number.isFinite(observed)) continue;
    const ageMs = Math.max(0, now - observed);
    if (ageMs > 30 * 60_000) continue;
    features.push({
      type: 'Feature',
      geometry: { type: 'LineString', coordinates: [[a.lon, a.lat], [b.lon, b.lat]] },
      properties: {
        from: link.from,
        to: link.to,
        rssi_dbm: link.rssi_dbm,
        snr_db: link.snr_db,
        fo_hz: link.fo_hz,
        tth_ms: link.tth_ms,
        age_ms: ageMs,
        color: ageMs <= 10 * 60_000 ? '#2fbf71' : '#e58b2a',
      },
    });
  }
  return { type: 'FeatureCollection', features };
}

export function mountRXTLinksLayer(map, { visible = true } = {}) {
  if (!map.getSource(SOURCE)) map.addSource(SOURCE, { type: 'geojson', data: EMPTY });
  const visibility = visible ? 'visible' : 'none';
  if (!map.getLayer(GLOW)) map.addLayer({
    id: GLOW, type: 'line', source: SOURCE,
    layout: { visibility, 'line-cap': 'round' },
    paint: { 'line-color': ['get', 'color'], 'line-width': 9, 'line-opacity': 0.16 },
  });
  if (!map.getLayer(LINES)) map.addLayer({
    id: LINES, type: 'line', source: SOURCE,
    layout: { visibility, 'line-cap': 'round' },
    paint: { 'line-color': ['get', 'color'], 'line-width': 3, 'line-opacity': 0.9 },
  });
  let popup = null;
  const enter = () => { map.getCanvas().style.cursor = 'pointer'; };
  const leave = () => { map.getCanvas().style.cursor = ''; };
  const click = (event) => {
    const p = event.features?.[0]?.properties;
    if (!p) return;
    popup?.remove();
    const body = document.createElement('div');
    const title = document.createElement('strong');
    title.textContent = `${p.from} → ${p.to}`;
    const metrics = document.createElement('div');
    metrics.textContent = `RSSI ${p.rssi_dbm} dBm · SNR ${Number(p.snr_db).toFixed(2)} dB · FO ${p.fo_hz} Hz · TTH ${p.tth_ms} ms`;
    body.append(title, metrics);
    popup = new maplibregl.Popup({ offset: 8, maxWidth: '360px' })
      .setLngLat(event.lngLat).setDOMContent(body).addTo(map);
  };
  map.on('mouseenter', LINES, enter);
  map.on('mouseleave', LINES, leave);
  map.on('click', LINES, click);
  return {
    refresh(links) { map.getSource(SOURCE)?.setData(linksToGeoJSON(links)); },
    setVisible(v) {
      for (const id of [GLOW, LINES]) if (map.getLayer(id)) map.setLayoutProperty(id, 'visibility', v ? 'visible' : 'none');
    },
    destroy() {
      popup?.remove();
      // Svelte destroys the child MaplibreMap before the parent route tears
      // down its overlay objects. At that point map.remove() has already
      // cleared map.style, and getLayer()/getSource() throw instead of simply
      // returning undefined. Cleanup must be idempotent and safe against that
      // lifecycle order or the exception aborts the SPA route transition.
      try { map.off('mouseenter', LINES, enter); } catch { /* map removed */ }
      try { map.off('mouseleave', LINES, leave); } catch { /* map removed */ }
      try { map.off('click', LINES, click); } catch { /* map removed */ }
      if (!map.style) return;
      for (const id of [LINES, GLOW]) {
        if (map.getLayer(id)) map.removeLayer(id);
      }
      if (map.getSource(SOURCE)) map.removeSource(SOURCE);
    },
  };
}

export async function loadRXTLinks() {
  const response = await fetch('/api/rxt/links', { credentials: 'same-origin' });
  if (!response.ok) throw new Error(`RXT links HTTP ${response.status}`);
  return response.json();
}
import maplibregl from 'maplibre-gl';
