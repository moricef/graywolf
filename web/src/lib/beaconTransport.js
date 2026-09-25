// The TNC2 transmitter is configured independently of channel backing.
// Its status must take precedence when describing a beacon's RF route.
export function beaconRFTransport(channelId, tnc2Config) {
  if (!tnc2Config) return { kind: 'unknown', detail: 'Transport unavailable' };
  if (tnc2Config?.tx_transport && Number(tnc2Config.tx_channel) === Number(channelId)) {
    const transport = tnc2Config.tx_transport;
    const connected = transport === 'tcp'
      ? tnc2Config.tcp_connected
      : tnc2Config.serial_connected;
    const name = transport === 'tcp' ? 'TCP' : 'Serial';
    return {
      kind: 'tnc2',
      health: connected ? 'live' : 'down',
      detail: `Native TNC2 (${name}) · ${connected ? 'Connected' : 'Disconnected'}`,
      connectionDetail: `${name} · ${connected ? 'Connected' : 'Disconnected'}`,
    };
  }
  return { kind: 'ax25', detail: 'AX.25 via channel backend' };
}

export function beaconChannelPresentation(channel, tnc2Config) {
  if (!channel) return null;
  const route = beaconRFTransport(channel.id, tnc2Config);
  if (route.kind !== 'tnc2') return null;
  return {
    detail: route.detail,
    health: route.health,
    ariaLabel: `Channel ${channel.id}, ${channel.name}, ${route.detail}`,
    tooltip: route.detail,
  };
}
