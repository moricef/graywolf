import test from 'node:test';
import assert from 'node:assert/strict';
import { beaconRFTransport, beaconChannelPresentation } from './beaconTransport.js';

test('beacon picker reports TNC2 status rather than unrelated KISS health', () => {
  const channel = { id: 2, name: 'LoRa APRS', backing: { health: 'down' } };
  const config = { tx_channel: 2, tx_transport: 'tcp', tcp_connected: true };
  assert.deepEqual(beaconRFTransport(2, config), {
    kind: 'tnc2', health: 'live', detail: 'Native TNC2 (TCP) · Connected',
    connectionDetail: 'TCP · Connected',
  });
  assert.match(beaconChannelPresentation(channel, config).ariaLabel, /Native TNC2.*Connected/);
  assert.equal(beaconChannelPresentation({ id: 3 }, config), null);
});

test('TNC2 transport uses its own connection state', () => {
  const config = { tx_channel: 2, tx_transport: 'serial', tcp_connected: true, serial_connected: false };
  assert.deepEqual(beaconRFTransport(2, config), {
    kind: 'tnc2', health: 'down', detail: 'Native TNC2 (Serial) · Disconnected',
    connectionDetail: 'Serial · Disconnected',
  });
  assert.deepEqual(beaconRFTransport(3, config), {
    kind: 'ax25', detail: 'AX.25 via channel backend',
  });
  assert.deepEqual(beaconRFTransport(2, null), {
    kind: 'unknown', detail: 'Transport unavailable',
  });
});
