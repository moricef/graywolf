# LoRa APRS RXT telemetry

RXT (Remote Receiver Telemetry) reports measurements made by receivers along
the RF path. Graywolf consumes the versioned LoRa APRS JSON stream exposed by
compatible receivers and iGates. Each `rx` event supplies authoritative
TNC2-compatible bytes, measurements from the local iGate receiver, and any RXT
measurements made by previous receivers. Graywolf preserves accepted
receptions, decodes APRS semantics when applicable, and displays measured RF
links on the map.

## Configure iGates

Open **Settings -> RXT**, add each iGate's stream URL, and select **Save**. For
example:

```text
http://192.168.1.161/api/v1/aprs/stream
```

The URL must be reachable from the computer running Graywolf. If an iGate is
reachable only through a VPN or SSH tunnel on another computer, arrange that
network path first; enter the URL at the address and port visible to Graywolf.
For a tunnel terminating on the Graywolf computer, that can be
`http://127.0.0.1:18102/api/v1/aprs/stream`.

Graywolf runs every configured source independently. It requests
`application/x-ndjson`, reads each connection continuously, and reconnects
after a disconnect. The settings page reports the last attempt, last successful
event, latest error, accepted `rx` count, active links and history cursor
separately for every source. Adding or removing a URL takes effect without
restarting Graywolf.
Removing every URL disables RXT ingestion.

There is no fixed source-count limit in the application. Each source uses one
HTTP connection and one lightweight worker; the practical limit is the host's
network and memory capacity. Source URLs are configuration data: Graywolf does
not contain site-specific station names, VPN addresses or LAN routes.

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

The **RXT Telemetry** page lists measured links and their RSSI, SNR, frequency
offset (FO), time-to-hop (TTH), packet, age, and map-position state. Each
directed `TX -> RX` pair has one row: its newest observation wins, including
when multiple sources report the same pair. Links expire after 30 minutes.
`TTH` is absent for the final local reception because it was not measured as a
remote RXT hop.

On the **Live Map**, enable **RXT Links** to draw links whose two stations have
known positions. The **RXT station** selector shows only links involving one
chosen station, or all links. Green means observed within the last 10 minutes;
orange means older than 10 minutes. These colors represent age, not RSSI or
link quality. Hover over a line to see its measurements. When several links
overlap, the popup lists each directed link separately. **Station Labels**
controls callsign labels independently of station symbols; clicking overlapping
station symbols spreads them apart for selection.

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

## Historical field validation (2026-09-21)

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

For this observation the iGate displayed UTC (`11:42`) while Graywolf rendered
Europe/Paris local time (`13:42`). The iGate's GMT offset is configurable; the
display difference did not change event ordering, link age or packet identity.
This RF observation validates the printable
current Mic-E DTI; the non-printable `0x1c` and `0x1d` forms remain covered by
the byte-exact automated JSON tests.

The packet inspector applies the same APRS 1.2c Mic-E wire rules as the
semantic decoder. In particular, the six longitude and speed/course bytes use
an offset of 28 and may therefore range from `0x1c` through `0x7f`; they are
not restricted to printable ASCII. A live zero-speed beacon from `F4MLV-7`
contained the valid speed/course triplet `0x6c 0x20 0x60` (0 kt, 68 degrees),
which is retained and displayed without a false malformed-packet error.

## Extended identity field validation (2026-09-25)

The extended textual identity model was validated over RF with the tracker
identity `F4MLV-MC`. Graywolf received message `test etendu 2` with APRS
message ID `165`, kept `F4MLV-MC` as the peer and source, and classified it as
RF input. The same reception appeared in the versioned JSON stream as event
`f490c9bf:51`:

```text
F4MLV-MC>APLRT1,WIDE1-1,WIDE2-1::F4MLV-2  :test etendu 2{165
```

Its local receiver measurements were `-73 dBm` RSSI, `12 dB` SNR and `676 Hz`
frequency error. This observation validates extended-identity message
reception through both the native TNC2 path and the JSON reception model.

After adding every participating RXT digi to the receiver's exact-identity
whitelist, a second tracker beacon produced JSON events `5b689892:2` for the
direct reception and `5b689892:3` for the RXT relay. The clean relayed packet
was:

```text
F4MLV-MC>TRUWV3,F4MLV-10*,WIDE2-1:`w25l#X</"=C}
```

The relay carried the RXT tuple `Pu+G`, resolved without an unknown identity
as `F4MLV-MC -> F4MLV-10`. Its remote measurements were `-83 dBm` RSSI,
`12 dB` SNR, `-1512 Hz` frequency error and `5775 ms` TTH. Graywolf advanced
the source cursor through event `5b689892:3`, confirming consumption from the
continuous JSON stream.

Together with the automated extended-identity and AX.25-boundary tests, these
on-air observations validate 100% of the extended textual identity model in
Graywolf's supported LoRa APRS/RXT scope: message reception, beacon reception,
exact identity preservation, JSON ingestion, RXT decoding and named RF-hop
resolution.

Each source is intentionally receive-only: Graywolf does not use the optional
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
whose `has_data` value is true. This compatibility path does not ingest APRS
packets or create a final local link from the legacy `local` field. Use the
versioned JSON stream for those functions:

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
