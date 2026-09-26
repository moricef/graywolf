export const CUSTOM_IGATE_SERVER = '__custom__';

export const IGATE_SERVER_OPTIONS = [
  { value: 'rotate.aprs2.net', label: 'Worldwide — rotate.aprs2.net' },
  { value: 'noam.aprs2.net', label: 'North America — noam.aprs2.net' },
  { value: 'soam.aprs2.net', label: 'South America — soam.aprs2.net' },
  { value: 'euro.aprs2.net', label: 'Europe & Africa — euro.aprs2.net' },
  { value: 'asia.aprs2.net', label: 'Asia — asia.aprs2.net' },
  { value: 'aunz.aprs2.net', label: 'Oceania — aunz.aprs2.net' },
  { value: CUSTOM_IGATE_SERVER, label: 'Custom…' },
];

const regionalServers = new Set(
  IGATE_SERVER_OPTIONS
    .map((option) => option.value)
    .filter((value) => value !== CUSTOM_IGATE_SERVER)
);

export function igateServerSelection(server) {
  return regionalServers.has(server) ? server : CUSTOM_IGATE_SERVER;
}

export function nextIgateServerState({ selection, server, customServer }, nextSelection) {
  const rememberedCustomServer = selection === CUSTOM_IGATE_SERVER
    ? server
    : customServer;

  return {
    selection: nextSelection,
    server: nextSelection === CUSTOM_IGATE_SERVER
      ? (rememberedCustomServer || server)
      : nextSelection,
    customServer: rememberedCustomServer,
  };
}
