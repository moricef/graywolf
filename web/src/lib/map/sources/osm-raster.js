// graywolf/web/src/lib/map/sources/osm-raster.js
//
// Public OSM raster tiles wrapped as a MapLibre style. Used as the
// default basemap and as a fallback when the user hasn't registered
// for private maps. Tiles come straight from the OSM public servers,
// the same URLs Leaflet was hitting; no API key required.

export function osmRasterStyle() {
  return {
    version: 8,
    sources: {
      osm: {
        type: 'raster',
        // Use OSM's canonical HTTP/2 endpoint. The historical a/b/c
        // sharding forces three DNS lookups and three TLS connections on a
        // cold SPA remount, which makes the online basemap appear unavailable
        // briefly even though the Graywolf server and offline maps are fine.
        tiles: ['https://tile.openstreetmap.org/{z}/{x}/{y}.png'],
        tileSize: 256,
        maxzoom: 19,
        attribution:
          '&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a> contributors',
      },
    },
    layers: [{ id: 'osm', type: 'raster', source: 'osm' }],
  };
}
