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
    description: 'a positioned APRS weather report from a Davis console or WxNow.txt file.',
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
    weather_device: typeof row.weather_device === 'string' ? row.weather_device : '',
    weather_baud: Number.isFinite(Number(row.weather_baud)) && Number(row.weather_baud) > 0
      ? String(row.weather_baud)
      : '19200',
    weather_bucket: ['0.2mm', '0.1mm', '0.01in'].includes(row.weather_bucket)
      ? row.weather_bucket
      : '0.2mm',
  };
}

export function validateWeatherBeacon(form) {
  if (form?.type !== 'weather') return '';
  if (form.weather_source === 'wxnow_file') {
    if (typeof form.weather_path !== 'string' || form.weather_path.trim() === '') {
      return 'WxNow.txt path is required for a weather beacon';
    }
    return '';
  }
  if (form.weather_source === 'davis_serial') {
    if (typeof form.weather_device !== 'string' || form.weather_device.trim() === '') {
      return 'Serial device is required for a Davis weather beacon';
    }
    if (!Number.isInteger(Number(form.weather_baud)) || Number(form.weather_baud) <= 0) {
      return 'Serial baud must be a positive integer';
    }
    if (form.weather_bucket !== '0.2mm' && form.weather_bucket !== '0.01in') {
      return 'Select the Davis rain collector size';
    }
    return '';
  }
  if (form.weather_source === 'peet_serial') {
    if (typeof form.weather_device !== 'string' || form.weather_device.trim() === '') {
      return 'Serial device is required for a Peet Bros weather beacon';
    }
    if (!Number.isInteger(Number(form.weather_baud)) || Number(form.weather_baud) <= 0) {
      return 'Serial baud must be a positive integer';
    }
    if (form.weather_bucket !== '0.1mm' && form.weather_bucket !== '0.01in') {
      return 'Select the Peet Bros rain counter unit';
    }
    return '';
  }
  return 'Select a supported weather source';
}

export function validateCustomBeacon(form) {
  if (form?.type !== 'custom') return '';
  if (typeof form.custom_info !== 'string' || form.custom_info.trim() === '') {
    return 'Raw APRS information is required for a custom beacon';
  }
  return '';
}
