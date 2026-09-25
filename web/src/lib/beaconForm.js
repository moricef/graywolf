export const BEACON_TYPE_OPTIONS = Object.freeze([
  {
    value: 'position',
    label: 'Position',
    description: 'a beacon for a station.',
  },
  {
    value: 'object',
    label: 'Object',
    description: 'a named item such as a repeater, event site, or hospital.',
  },
  {
    value: 'tracker',
    label: 'Tracker',
    description: 'a GPS-driven mobile beacon that uses SmartBeaconing to adapt its rate to speed and turns.',
  },
  {
    value: 'weather',
    label: 'Weather',
    description: 'a positioned APRS weather report read from a WxNow.txt file.',
  },
  {
    value: 'custom',
    label: 'Custom',
    description: 'a raw APRS information field, optionally followed by a static or command-generated comment.',
  },
]);

export function beaconTypeUsesPosition(type) {
  return type !== 'custom';
}

export function beaconExtraFields(row = {}) {
  return {
    custom_info: typeof row.custom_info === 'string' ? row.custom_info : '',
    comment_cmd: typeof row.comment_cmd === 'string' ? row.comment_cmd : '',
    weather_source: typeof row.weather_source === 'string' && row.weather_source
      ? row.weather_source
      : 'wxnow_file',
    weather_path: typeof row.weather_path === 'string' ? row.weather_path : '',
  };
}

export function validateWeatherBeacon(form) {
  if (form?.type !== 'weather') return '';
  if (form.weather_source !== 'wxnow_file') return 'Weather source must be WxNow.txt';
  if (typeof form.weather_path !== 'string' || form.weather_path.trim() === '') {
    return 'WxNow.txt path is required for a weather beacon';
  }
  return '';
}

export function validateCustomBeacon(form) {
  if (form?.type !== 'custom') return '';
  if (typeof form.custom_info !== 'string' || form.custom_info.trim() === '') {
    return 'Raw APRS information is required for a custom beacon';
  }
  return '';
}
