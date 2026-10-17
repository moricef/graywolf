const CARD_WIDTH = 360;
const EDGE_GAP = 12;

function nextZ(windows) {
  return windows.reduce((highest, card) => Math.max(highest, card.z || 0), 99) + 1;
}

export function pinStationWindow(windows, station, viewport = {}) {
  if (!station?.callsign) return windows;
  const existing = windows.find((card) => card.callsign === station.callsign);
  if (existing) {
    const z = nextZ(windows);
    return windows.map((card) =>
      card.callsign === station.callsign
        ? { ...card, snapshot: station, minimized: false, z }
        : card
    );
  }

  const index = windows.length;
  const width = Math.max(CARD_WIDTH + EDGE_GAP * 2, Number(viewport.width) || 1024);
  const height = Math.max(240, Number(viewport.height) || 768);
  const cascade = (index % 6) * 26;
  const x = Math.max(EDGE_GAP, width - CARD_WIDTH - 24 - cascade);
  const y = Math.min(Math.max(64 + cascade, EDGE_GAP), height - 120);
  return [
    ...windows,
    {
      callsign: station.callsign,
      snapshot: station,
      x,
      y,
      z: nextZ(windows),
      minimized: false,
    },
  ];
}

export function moveStationWindow(windows, callsign, x, y, viewport = {}) {
  const width = Math.max(CARD_WIDTH + EDGE_GAP * 2, Number(viewport.width) || 1024);
  const height = Math.max(240, Number(viewport.height) || 768);
  const clampedX = Math.min(Math.max(EDGE_GAP, x), width - CARD_WIDTH - EDGE_GAP);
  const clampedY = Math.min(Math.max(EDGE_GAP, y), height - 52);
  return windows.map((card) =>
    card.callsign === callsign ? { ...card, x: clampedX, y: clampedY } : card
  );
}

export function frontStationWindow(windows, callsign) {
  const card = windows.find((entry) => entry.callsign === callsign);
  if (!card) return windows;
  const z = nextZ(windows);
  return windows.map((entry) => (entry.callsign === callsign ? { ...entry, z } : entry));
}

export function toggleStationWindow(windows, callsign) {
  return windows.map((card) =>
    card.callsign === callsign ? { ...card, minimized: !card.minimized } : card
  );
}

export function closeStationWindow(windows, callsign) {
  return windows.filter((card) => card.callsign !== callsign);
}

export function refreshStationWindows(windows, stations) {
  let changed = false;
  const refreshed = windows.map((card) => {
    const station = stations?.get(card.callsign);
    // $state and SvelteMap can expose different proxy identities for the same
    // station object. Comparing object identity here made the parent effect
    // replace the windows array forever and locked the UI as soon as a card
    // was pinned. last_heard advances whenever the station DTO is refreshed,
    // so it is the stable change token we need.
    if (!station || station.last_heard === card.snapshot?.last_heard) return card;
    changed = true;
    return { ...card, snapshot: station };
  });
  return changed ? refreshed : windows;
}
