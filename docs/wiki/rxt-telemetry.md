# LoRa APRS RXT telemetry

Graywolf can consume the versioned LoRa APRS JSON stream exposed by compatible
receivers and iGates. Each `rx` event supplies the clean APRS packet plus local
and RXT radio metadata. Graywolf feeds the clean packet into its APRS receive
pipeline and displays the measured RF links on the map.

## Configure an iGate

Open **Settings -> RXT**, enter the absolute URL of the iGate endpoint, and
select **Save**. For example:

```text
http://192.168.1.161/api/v1/aprs/stream
```

Graywolf requests `application/x-ndjson`, reads the connection continuously,
and reconnects automatically after a disconnect. The same page reports the
last attempt, last successful record, the latest error, the number of `rx`
records accepted, and the number of active links. An empty URL disables the
source. Changing the URL takes effect immediately without restarting Graywolf.

The authoritative `packet.raw_tnc2_base64` bytes are converted back to an
AX.25 UI frame before entering Graywolf's normal APRS parsing, messages,
station-cache, packet-log, and output pipeline. Stream packets are tagged as
`aprs-json`; they are not echoed to KISS clients or submitted to Graywolf's
digipeater, which prevents a remote reception copy from creating an RF loop.

The **RXT Telemetry** page lists decoded links and their RSSI, SNR, frequency
offset, time-to-hop, packet, age, and map-position state. The Live Map draws a
link only after APRS has supplied positions for both endpoint callsigns. The
newest observation replaces older data for the same directed link, and links
expire after 30 minutes.

## Versioned NDJSON stream

The endpoint emits one JSON object per line. Graywolf accepts `hello` control
records and processes `rx` records from protocol family `1`. For every
reception it keeps the declared measured RXT links and also derives the final
local link from the last repeated path station (or the packet source for a direct frame)
to `receiver.station`, using `reception.local` RSSI, SNR, and frequency error.
Legacy hops with `has_data:false` are accepted but are not drawn as measured
links, because the stream deliberately provides no radio metrics for them.

CRC-valid records marked `parse_status:"malformed"` are accepted and counted
without terminating the stream. Their authoritative bytes are offered to the
normal APRS pipeline, which rejects invalid TNC2 data without forwarding or
digipeating it; later stream records continue to be processed normally.

The source is intentionally receive-only: Graywolf does not use the optional
JSON TX API. It does handle heartbeat records and reliable history resume.
When the producer advertises `history_resume`, Graywolf persists the last
accepted `event_id` and reconnects with `after=<event_id>`. Replayed duplicates
from the same producer boot are ignored. A `gap` record clears the stale cursor
before subsequent live events establish a new one.

Graywolf deliberately does not consume the producer's
`GET /api/v1/aprs/events?limit=10` snapshot. That endpoint is intended for
dashboard polling, not lossless ingestion; mixing it with the continuous
stream would add a second source of duplicate and boundary handling. The
Graywolf RXT page instead polls its local `/api/rxt/links` projection, which is
populated from the continuous stream.

## Legacy endpoint compatibility

Existing `/rxt.json` URLs remain supported. Graywolf polls these endpoints
every five seconds and consumes `age_ms`, `packet`, and `rxt_hops` entries
whose `has_data` value is true:

```json
[
  {
    "rx_time": "09:25:13",
    "age_ms": 203355,
    "packet": "F5BYL-10>APLRG1,F6DEV-10*,F4MLV-10*:...",
    "local": {
      "rssi_dbm": -69,
      "snr_db": 9.5,
      "fo_hz": 2540
    },
    "rxt_hops": [
      {
        "from": "F5BYL-10",
        "to": "F6DEV-10",
        "has_data": true,
        "rssi_dbm": -120,
        "snr_db": 1.25,
        "fo_hz": -2177,
        "tth_ms": 6791
      }
    ]
  }
]
```

Legacy polling accepts at most 1 MiB per response. The iGate and Graywolf do
not need to share a timezone because `age_ms`, rather than the wall-clock-only
`rx_time`, determines the observation time.

## Portable Windows test build

The Windows ZIP contains both `graywolf.exe` and `graywolf-modem.exe`. Extract
the complete archive, keep both executables in the same directory, and start
`graywolf.exe`. The modem helper is discovered and launched automatically; it
does not need to be installed or started separately.

Graywolf stores its configuration and logs under `C:\ProgramData\Graywolf`, so
replacing an extracted test-build directory does not erase the saved RXT URL.
