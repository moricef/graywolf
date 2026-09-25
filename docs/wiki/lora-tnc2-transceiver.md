# Direct LoRa TNC2 transceiver (TCP or serial)

Graywolf can connect directly to a LoRa transceiver that sends and accepts
newline-delimited TNC2 packets. This connection is separate from the LoRa APRS
JSON/RXT source and from KISS. It keeps extended textual identities such as
`F4MLV-16` or `F4MLV-GS` and binary Mic-E information without AX.25 address
conversion.

Open **Settings → TNC2** to configure a TCP host/port, a serial device and
baud rate, and the explicitly authorized transmit transport, source, channel
and byte limit. **Save** persists the settings in `graywolf.db` and applies
them immediately; they survive a restart. Both receive connections may be
configured at once. Leave either connection blank to disable it. The status
panel reports whether each configured connection is currently connected.

The `-tnc2-tcp HOST:PORT` and `-tnc2-serial DEVICE -tnc2-baud 115200` flags
remain available for initial or headless configuration. Once settings have
been saved in the WebUI, the database settings take precedence over these
flags. Incoming TNC2 packets are decoded
for Graywolf's packet log and station map; they are not automatically forwarded
to KISS, the digipeater, APRS-IS, or RF. Non-packet console and RXT diagnostic
lines on the same port are ignored. RXT measurements still come from the
separate JSON/RXT source.

Transmission is disabled by default. To authorize it, configure
exactly one output transport, one textual source identity, and one Graywolf
channel. For example:

```text
-tnc2-tcp 192.168.1.161:8001
-tnc2-tx-transport tcp
-tnc2-tx-source F4MLV-GS
-tnc2-tx-channel 1
-tnc2-max-tx-bytes 255
```

For a serial-connected transceiver, select **Serial** as the transmit
transport and enter its device and baud rate in Settings → TNC2 (or use the
equivalent flags in a headless deployment). The configured source
must match the source in each submitted packet exactly. The byte limit is the
peer's input-record limit; 255 is the current CA2RXU TNC2 buffer limit.
Packets containing CR or LF are rejected because either byte would split the
wire record. TX requests use Graywolf's priority, deduplication and per-channel
rate governor, but never pass through the AX.25 encoder.

The first explicit TX entry point is the authenticated
`POST /api/tnc2/tx` endpoint. Its JSON body contains one complete TNC2 packet
as `raw_tnc2_base64`. A `202` response means the packet was accepted into the
TX queue, **not** that the transceiver transmitted it over RF. The source
check, output permission, connection state, and peer byte limit are enforced
before queueing. A normal receive connection does not authorize transmission.

The message composer uses this textual output for RF sends on the authorized
channel, including retries. Incoming TNC2 APRS messages reach the normal
Graywolf inbox. Beacons configured on the same authorized channel also use
the textual TNC2 output. This includes position, tracker, object, iGate and
custom beacons, with the existing schedule and **Send now** action. Textual
identities such as `F4MLV-16` and `F4MLV-GS` are preserved on that channel;
other channels retain AX.25/KISS validation and cannot encode them. A beacon
whose source differs from the configured TNC2 TX source is refused at send
time. A beacon set to **RF + APRS-IS** sends the RF copy through TNC2 and an
independent APRS-IS line with `TCPIP*`.

By default, the connected iGate/TNC owns message auto-ACKs;
Graywolf does not send a second ACK. Only use `-tnc2-autoack` when the
connected device does not generate ACKs itself. This option requires explicit
TNC2 TX authorization. The configured station callsign must exactly match
`-tnc2-tx-source`; a different source is refused rather than converted to
AX.25. Other channels retain their existing AX.25/KISS behavior. APRS message
addressees still have their standard nine-character limit.

The iGate must have its TNC port configured for TNC2 input and LoRa TX
enabled. If the submitted source
equals the iGate's own callsign, CA2RXU also requires its "accept own" TNC
setting; otherwise it deliberately ignores the frame. CA2RXU currently trims
the incoming text record, so Graywolf's exact bytes-to-port guarantee does not
imply byte-for-byte identity after the firmware's input processing for records
with leading or trailing whitespace.

## Extended identity model

The [LoRa APRS JSON protocol, section 3](https://github.com/moricef/LoRa_APRS_JSON_Protocol/blob/main/LORA_APRS_JSON_PROTOCOL.md#3-representation)
defines the source model for extended textual identities: received address
text is authoritative, a suffix after `-` is an opaque string, and an
optional numeric `ssid` is only a derived view. The authoritative packet
copy is `packet.raw_tnc2_base64`, including binary information bytes.
Graywolf preserves these properties in its TNC2 packet model and checks
classic AX.25 representability only when crossing an AX.25 output boundary.
Automated tests cover identities such as `F4JJE-16`, `NN7LE-GS` and
`F4MLV-GS` on the JSON and direct TNC2 paths.

## On-air validation (2026-09-24)

The direct TNC2 path was verified with the station `F4MLV-2` on Graywolf
channel 1. These observations pair Graywolf's service journal with packets
seen on aprs.fi after reception by other iGates (times are CEST):

| Graywolf RF send | aprs.fi reception | Evidence |
| --- | --- | --- |
| 20:51:51, message ID `010` to `F4MLV-7` on channel 1 | 20:52:03, `F4MLV-2>APGRWO,F4MLV-10*,WIDE2-1,qAR,F4BPJ-10::F4MLV-7  :test{010` | Same message ID and text; repeated RF hop `F4MLV-10*`; `qAR` identifies RF-to-APRS-IS gating by `F4BPJ-10`. |
| 23:32:10, position beacon ID 1 with `send_path=rf` on channel 1 | 23:32:20, `F4MLV-2>APGRWO,F4MLV-10*,WIDE2-1,qAO,F6DEV-10:!L8gjuNmQaa  C/A=002789Graywolf/0.14.13{bq&I}` | Same beacon information prefix; repeated RF hop `F4MLV-10*`; received and uploaded by `F6DEV-10` with an RXT trailer. |

`qAR` identifies RF-to-APRS-IS gating; `qAO` is used by receive-only iGates
for RF traffic (see the [APRS-IS q construct
specification](https://aprs-is.net/q.aspx)). These paired
observations validate RF transmission of a message and a beacon through the
native TNC2 path. Other `F4MLV-2` entries in the same aprs.fi history carry
`TCPIP*,qAC`; those are direct APRS-IS submissions and are not used as RF
evidence here. The observed source in these two RF examples is the canonical
`F4MLV-2`; they validate native TNC2 RF transmission, while the extended
identity examples above have automated coverage. On 2026-09-25, the tracker
identity `F4MLV-MC` was also validated over RF through native TNC2 message
reception and the complete JSON/RXT beacon path. The observations and measured
hop are recorded in [RXT telemetry](rxt-telemetry.md#extended-identity-field-validation-2026-09-25).
