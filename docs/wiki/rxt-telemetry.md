# LoRa APRS RXT telemetry

Graywolf can consume the versioned LoRa APRS JSON stream exposed by compatible
receivers and iGates. Each `rx` event supplies authoritative TNC2-compatible
bytes plus local and RXT radio metadata. Graywolf preserves every accepted
reception, decodes APRS semantics when applicable, and displays measured RF
links on the map.

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

The authoritative `packet.raw_tnc2_base64` bytes are first stored in a
lossless reception model. A separate textual TNC2 model is derived only for a
parsable envelope. Its source, destination and path identities remain exact,
including opaque or out-of-range suffixes such as `NN7LE-GS` and `F4JJE-16`.
Graywolf can therefore decode applicable APRS information, populate the packet
log and station cache, and update the map without first forcing the complete
envelope into classic AX.25 addressing.

Classic AX.25 conversion is a separate, checked adapter. It accepts only a
canonical uppercase 1--6 character AX.25 callsign and either no suffix (SSID
0) or a decimal suffix from 1 through 15 without leading zero. The converted
address must render back to exactly the authoritative TNC2 identity, including
the path `*` marker. Conversion failure does not invalidate or alter the
received JSON event. JSON ingress is receive-only by default: representability
alone never authorizes KISS, digipeater, RF or APRS-IS output, nor an automatic
action or message response.

The **RXT Telemetry** page lists decoded links and their RSSI, SNR, frequency
offset, time-to-hop, packet, age, and map-position state. The Live Map draws a
link only after APRS has supplied positions for both endpoint callsigns. The
newest observation replaces older data for the same directed link, and links
expire after 30 minutes.

## Versioned NDJSON stream

The endpoint emits one JSON object per line. Graywolf accepts `hello` control
records and processes `rx` records from protocol family `1`. For every
reception it keeps the declared measured RXT links. When the authoritative
bytes produce a `TNC2Packet`, Graywolf also derives the final local link from
its last repeated path identity (or its source for a direct frame) to
`receiver.station`, using `reception.local` RSSI, SNR, and frequency error.
Producer-supplied `packet.source` and `packet.path` hints never create a local
link, and no local link is invented when the authoritative envelope cannot be
parsed.
Legacy hops with `has_data:false` are accepted but are not drawn as measured
links, because the stream deliberately provides no radio metrics for them.

CRC-valid records marked `parse_status:"malformed"` are accepted, preserved
in the packet log and counted without pretending that they produced a parsed
TNC2 packet. Their reception and RXT metadata remain available, and later
stream records continue normally. Likewise, a valid TNC2-compatible but
non-APRS information field is retained even when Graywolf has no APRS
semantics to attach to it.

Mic-E remains binary end to end. The APRS semantic decoder recognizes all four
data identifiers defined by APRS 1.2c: printable current/old `` ` `` and `'`,
plus the non-printable Rev. 0 beta forms `0x1c` and `0x1d`. Neither the JSON
consumer nor the textual TNC2 envelope converts their information bytes to a
string before decoding or storage.

## Field validation

On 2026-09-21 the complete RF-to-map path was verified with a real current
Mic-E beacon from `F4MLV-7`. The tracker transmitted:

```text
F4MLV-7>4R5WV3,WIDE1-1,WIDE2-1:`w25l*o[/"=?}
```

The iGate RF log also observed its relayed form with an RXT trailer:

```text
F4MLV-7>4R5WV3,F4MLV-2*,WIDE2-1:`w25l*o[/"=?}{Yua>}
```

The firmware dashboard exposed the clean Mic-E frame without the RXT trailer.
Graywolf consumed the versioned JSON reception, classified the packet as
`mic-e`, retained `F4MLV-7` and destination `4R5WV3`, and created the drawable
local RF link `F4MLV-7 -> F4MLV-2` with `-74 dBm`, `13.00 dB` SNR and `459 Hz`
frequency error. The blank TTH value is intentional: TTH belongs to remote RXT
hops and is not invented for the final local reception.

The iGate display used UTC (`11:42`) while Graywolf rendered Europe/Paris local
time (`13:42`). This two-hour display difference did not change event ordering,
link age or packet identity. This OTA observation validates the printable
current Mic-E DTI; the non-printable `0x1c` and `0x1d` forms remain covered by
the byte-exact automated JSON tests.

The source is intentionally receive-only: Graywolf does not use the optional
JSON TX API. It does handle heartbeat records and reliable history resume.
When the producer advertises `history_resume`, Graywolf persists the last
preserved `event_id` and reconnects with `after=<event_id>`. A storage/delivery
failure does not advance the cursor, so the event can be replayed. Replayed
duplicates from the same producer boot are ignored. A `gap` record clears the
stale cursor before subsequent live events establish a new one.

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
