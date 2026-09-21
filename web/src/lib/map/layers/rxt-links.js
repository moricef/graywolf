const SOURCE = 'gw-rxt-links';
const HIT = 'gw-rxt-links-hit';
const GLOW = 'gw-rxt-links-glow';
const LINES = 'gw-rxt-links-lines';
const EMPTY = { type: 'FeatureCollection', features: [] };
export const RXT_POPUP_CLASS = 'gw-station-popup gw-rxt-popup';

export function uniqueRXTLinkProperties(features = []) {
  const links = new Map();
  for (const feature of features) {
    const p = feature?.properties;
    if (!p?.from || !p?.to) continue;
    links.set(`${p.from}\u0000${p.to}`, p);
  }
  return [...links.values()].sort((a, b) =>
    `${a.from}>${a.to}`.localeCompare(`${b.from}>${b.to}`));
}

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
  if (!map.getLayer(HIT)) map.addLayer({
    id: HIT, type: 'line', source: SOURCE,
    layout: { visibility, 'line-cap': 'round' },
    paint: { 'line-color': '#000000', 'line-width': 18, 'line-opacity': 0 },
  });
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
  let popupSignature = '';
  const showPopup = (event) => {
    const links = uniqueRXTLinkProperties(event.features);
    if (links.length === 0) return;
    const signature = JSON.stringify(links);
    if (!popup) {
      popup = new maplibregl.Popup({
        offset: 8,
        maxWidth: '420px',
        className: RXT_POPUP_CLASS,
      }).setLngLat(event.lngLat).addTo(map);
    } else {
      popup.setLngLat(event.lngLat);
    }
    if (signature === popupSignature) return;
    popupSignature = signature;

    const body = document.createElement('div');
    body.className = 'rxt-popup-body';
    if (links.length > 1) {
      const heading = document.createElement('strong');
      heading.className = 'rxt-popup-heading';
      heading.textContent = `${links.length} liaisons superposées`;
      body.append(heading);
    }
    for (const p of links) {
      const row = document.createElement('div');
      row.className = 'rxt-popup-row';
      const title = document.createElement('strong');
      title.textContent = `${p.from} → ${p.to}`;
      const metrics = document.createElement('div');
      metrics.className = 'rxt-popup-metrics';
      const tth = p.tth_ms == null ? '—' : `${p.tth_ms} ms`;
      metrics.textContent = `RSSI ${p.rssi_dbm} dBm · SNR ${Number(p.snr_db).toFixed(2)} dB · FO ${p.fo_hz} Hz · TTH ${tth}`;
      row.append(title, metrics);
      body.append(row);
    }
    popup.setDOMContent(body);
  };
  const enter = (event) => {
    map.getCanvas().style.cursor = 'pointer';
    showPopup(event);
  };
  const move = (event) => { showPopup(event); };
  const leave = () => {
    map.getCanvas().style.cursor = '';
    popup?.remove();
    popup = null;
    popupSignature = '';
  };
  const click = (event) => { showPopup(event); };
  map.on('mouseenter', HIT, enter);
  map.on('mousemove', HIT, move);
  map.on('mouseleave', HIT, leave);
  map.on('click', HIT, click);
  return {
    refresh(links) { map.getSource(SOURCE)?.setData(linksToGeoJSON(links)); },
    setVisible(v) {
      for (const id of [HIT, GLOW, LINES]) if (map.getLayer(id)) map.setLayoutProperty(id, 'visibility', v ? 'visible' : 'none');
    },
    destroy() {
      popup?.remove();
      // Svelte destroys the child MaplibreMap before the parent route tears
      // down its overlay objects. At that point map.remove() has already
      // cleared map.style, and getLayer()/getSource() throw instead of simply
      // returning undefined. Cleanup must be idempotent and safe against that
      // lifecycle order or the exception aborts the SPA route transition.
      try { map.off('mouseenter', HIT, enter); } catch { /* map removed */ }
      try { map.off('mousemove', HIT, move); } catch { /* map removed */ }
      try { map.off('mouseleave', HIT, leave); } catch { /* map removed */ }
      try { map.off('click', HIT, click); } catch { /* map removed */ }
      if (!map.style) return;
      for (const id of [LINES, GLOW, HIT]) {
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
